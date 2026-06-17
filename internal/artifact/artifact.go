// Package artifact provides a small in-memory, one-time, TTL-bounded store for
// downloaded .torrent files served over a capability URL.
//
// Each stored artifact gets an unguessable token (a bearer capability) and an
// expiry. A token is consumed on the first successful retrieval, so a download
// URL works exactly once and only until it expires.
package artifact

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
)

// tokenBytes is the entropy of an artifact token before base64 encoding.
const tokenBytes = 32

// maxGCInterval caps how often the background janitor runs.
const maxGCInterval = time.Minute

// maxItems bounds the number of stored artifacts so a burst of unfetched
// downloads cannot grow the heap without limit; once this many live artifacts
// exist, Put fails rather than evicting a still-valid download URL.
const maxItems = 256

// ErrStoreFull is returned by Put when the store already holds maxItems live
// (unexpired) artifacts, so accepting another would force evicting a download
// URL the caller already holds.
var ErrStoreFull = errors.New("artifact store full")

// Artifact is a stored .torrent payload with its retrieval metadata.
type Artifact struct {
	Token     string
	Filename  string
	Content   []byte
	Size      int
	ExpiresAt time.Time
}

// Store is a concurrency-safe, one-time, TTL-bounded artifact store.
type Store struct {
	mu    sync.Mutex
	items map[string]*Artifact
	ttl   time.Duration
	now   func() time.Time
}

// NewStore creates a store whose artifacts expire after ttl.
func NewStore(ttl time.Duration) *Store {
	return &Store{
		items: make(map[string]*Artifact),
		ttl:   ttl,
		now:   time.Now,
	}
}

// Put stores content under a fresh token and returns the resulting artifact.
// It retains the caller's slice without copying, so the bytes must not be
// mutated after the call.
func (s *Store) Put(filename string, content []byte) (*Artifact, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}

	moment := s.now()

	art := &Artifact{
		Token:     token,
		Filename:  filename,
		Content:   content,
		Size:      len(content),
		ExpiresAt: moment.Add(s.ttl),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sweepLocked(moment)

	// After sweeping expired entries, a full store means maxItems live artifacts.
	// Evicting one would silently poison a download URL the caller already holds,
	// so refuse the new artifact loudly instead.
	if len(s.items) >= maxItems {
		return nil, errors.Wrapf(ErrStoreFull, "%d live artifacts", maxItems)
	}

	s.items[token] = art

	return art, nil
}

// Take returns the artifact for token and removes it, so a token is valid for a
// single retrieval. It returns ok=false for unknown, expired, or already-used
// tokens. The leading sweep drops every expired entry first, so any token still
// present is necessarily live.
func (s *Store) Take(token string) (*Artifact, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sweepLocked(s.now())

	art, ok := s.items[token]
	if !ok {
		return nil, false
	}

	delete(s.items, token)

	return art, true
}

// StartGC periodically reclaims expired artifacts until ctx is cancelled, so an
// artifact whose download URL is never fetched does not linger past its TTL.
func (s *Store) StartGC(ctx context.Context) {
	interval := min(s.ttl, maxGCInterval)
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep()
		}
	}
}

// sweep reclaims expired artifacts.
func (s *Store) sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sweepLocked(s.now())
}

// sweepLocked drops expired artifacts. The caller must hold s.mu.
func (s *Store) sweepLocked(moment time.Time) {
	for token, art := range s.items {
		if moment.After(art.ExpiresAt) {
			delete(s.items, token)
		}
	}
}

// newToken returns an unguessable URL-safe token.
func newToken() (string, error) {
	buf := make([]byte, tokenBytes)

	_, err := rand.Read(buf)
	if err != nil {
		return "", errors.Wrap(err, "generate artifact token")
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
