package artifact

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
)

func TestStartGC_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	store := NewStore(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		store.StartGC(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartGC did not return after context cancel")
	}
}

func TestPut_Take_OneTime(t *testing.T) {
	t.Parallel()

	store := NewStore(time.Hour)

	art, err := store.Put("file.torrent", []byte("dabc"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok := store.Take(art.Token)
	if !ok {
		t.Fatal("first Take should succeed")
	}

	if string(got.Content) != "dabc" || got.Filename != "file.torrent" {
		t.Errorf("artifact = %+v", got)
	}

	if _, ok := store.Take(art.Token); ok {
		t.Error("second Take must fail (one-time)")
	}
}

func TestTake_Unknown(t *testing.T) {
	t.Parallel()

	store := NewStore(time.Hour)

	if _, ok := store.Take("nope"); ok {
		t.Error("unknown token must not resolve")
	}
}

func TestPut_RecordsSize(t *testing.T) {
	t.Parallel()

	store := NewStore(time.Hour)
	content := []byte("d4:infod6:lengthi1e4:name1:xee")

	art, err := store.Put("x.torrent", content)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if art.Size != len(content) {
		t.Errorf("Size = %d, want %d", art.Size, len(content))
	}
}

func TestPut_FullStoreRejectsRatherThanEvictingLive(t *testing.T) {
	t.Parallel()

	store := NewStore(time.Hour)

	// Fill the store to capacity with live (unexpired) artifacts.
	first, err := store.Put("first", []byte("d"))
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}

	for i := 1; i < maxItems; i++ {
		_, putErr := store.Put(strconv.Itoa(i), []byte("d"))
		if putErr != nil {
			t.Fatalf("Put %d: %v", i, putErr)
		}
	}

	// One more must fail loudly rather than evict a live artifact, so the caller
	// learns instead of receiving a download URL that is already poisoned.
	_, overflowErr := store.Put("overflow", []byte("d"))
	if !errors.Is(overflowErr, ErrStoreFull) {
		t.Fatalf("Put on full store = %v, want ErrStoreFull", overflowErr)
	}

	store.mu.Lock()
	size := len(store.items)
	store.mu.Unlock()

	if size != maxItems {
		t.Errorf("store holds %d items, want exactly %d", size, maxItems)
	}

	// The earliest artifact must still be retrievable — it was not silently evicted.
	if _, ok := store.Take(first.Token); !ok {
		t.Error("earliest live artifact was evicted; its download URL is poisoned")
	}
}

func TestTokens_UniqueAndNonEmpty(t *testing.T) {
	t.Parallel()

	store := NewStore(time.Hour)

	first, _ := store.Put("a", []byte("d"))
	second, _ := store.Put("b", []byte("d"))

	if first.Token == "" || second.Token == "" {
		t.Fatal("tokens must be non-empty")
	}

	if first.Token == second.Token {
		t.Error("tokens must be unique")
	}
}

func TestSweep_ReclaimsUnfetchedExpired(t *testing.T) {
	t.Parallel()

	// An artifact whose URL is never fetched must still be reclaimed once it
	// expires — the background janitor calls sweep on each tick.
	store := NewStore(time.Minute)

	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }

	_, err := store.Put("x", []byte("d"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	clock = clock.Add(2 * time.Minute)
	store.sweep()

	if len(store.items) != 0 {
		t.Errorf("expired-but-unfetched artifact not reclaimed: %d left", len(store.items))
	}
}

func TestTake_Expired(t *testing.T) {
	t.Parallel()

	store := NewStore(time.Minute)

	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }

	art, err := store.Put("x", []byte("d"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Advance past the TTL.
	clock = clock.Add(2 * time.Minute)

	if _, ok := store.Take(art.Token); ok {
		t.Error("expired artifact must not resolve")
	}
}
