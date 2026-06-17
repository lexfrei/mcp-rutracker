package main

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"

	"github.com/lexfrei/mcp-rutracker/internal/artifact"
	"github.com/lexfrei/mcp-rutracker/internal/config"
	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
)

const matrixTorrent = "Matrix.torrent"

func TestArtifactHandler_ServesOnce(t *testing.T) {
	t.Parallel()

	store := artifact.NewStore(time.Minute)

	art, err := store.Put(matrixTorrent, []byte("dabc"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /artifacts/{id}", artifactHandler(testLogger(), store))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/artifacts/"+art.Token, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("first GET status = %d, want 200", rec.Code)
	}

	if rec.Body.String() != "dabc" {
		t.Errorf("body = %q, want dabc", rec.Body.String())
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/x-bittorrent" {
		t.Errorf("Content-Type = %q", ct)
	}

	if cl := rec.Header().Get("Content-Length"); cl != "4" {
		t.Errorf("Content-Length = %q, want 4", cl)
	}

	// The digest header lets a fetcher verify the body end to end; it must equal
	// the sha256 of exactly the bytes served.
	bodySum := sha256.Sum256([]byte("dabc"))
	if got := rec.Header().Get("X-Content-Sha256"); got != hex.EncodeToString(bodySum[:]) {
		t.Errorf("X-Content-Sha256 = %q, want sha256 of the served body", got)
	}

	wantCD := `attachment; filename="` + matrixTorrent + `"; filename*=UTF-8''` + matrixTorrent
	if cd := rec.Header().Get("Content-Disposition"); cd != wantCD {
		t.Errorf("Content-Disposition = %q, want %q", cd, wantCD)
	}

	// One-time: a second fetch of the same token must 404.
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/artifacts/"+art.Token, nil))

	if rec2.Code != http.StatusNotFound {
		t.Errorf("second GET status = %d, want 404 (one-time)", rec2.Code)
	}
}

func TestSafeFilename(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		matrixTorrent:        matrixTorrent,
		"../../etc/passwd":   "passwd",
		`evil".torrent`:      "evil_.torrent",
		"a\r\nSet-Cookie: x": "a__Set-Cookie_ x",
		"café.torrent":       "caf_.torrent",
		// Degenerate names that reduce to no alphanumeric must fall back to a
		// fixed safe name, never filename="." or filename="_".
		"":    fallbackFilename,
		"/":   fallbackFilename,
		"...": fallbackFilename,
	}

	for input, want := range cases {
		if got := safeFilename(input); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestStartGC_StopsWhenGroupSiblingFails(t *testing.T) {
	t.Parallel()

	// serve() runs the janitor as an errgroup member bound to groupCtx, so its
	// lifetime matches the transports: when any sibling exits (here, a failing
	// transport), groupCtx is cancelled and GC must stop rather than outlive the
	// HTTP server it exists for.
	store := artifact.NewStore(time.Hour)

	group, groupCtx := errgroup.WithContext(t.Context())
	gcStopped := make(chan struct{})

	group.Go(func() error {
		store.StartGC(groupCtx)
		close(gcStopped)

		return nil
	})

	group.Go(func() error {
		return errors.New("transport failed")
	})

	_ = group.Wait()

	select {
	case <-gcStopped:
	case <-time.After(time.Second):
		t.Fatal("GC did not stop after a group sibling errored")
	}
}

func TestTokenRef_RedactsBearerToken(t *testing.T) {
	t.Parallel()

	const token = "kZJ8s-RawURLEncoded-capability-token"

	ref := tokenRef(token)

	if ref == token {
		t.Fatal("tokenRef must not return the raw token")
	}

	if strings.Contains(token, ref) {
		t.Errorf("ref %q is a substring of the token — not a safe reference", ref)
	}

	if tokenRef(token) != ref {
		t.Error("tokenRef must be deterministic for the same token")
	}
}

func TestRFC5987Value_PreservesCyrillic(t *testing.T) {
	t.Parallel()

	// Pure attr-chars pass through unchanged.
	if got := rfc5987Value(matrixTorrent); got != matrixTorrent {
		t.Errorf("ASCII = %q, want unchanged", got)
	}

	// A Cyrillic name is percent-encoded, carries no raw special bytes, and
	// round-trips back to the original (so a modern client recovers the name).
	const cyrillic = "Матрица.torrent"

	encoded := rfc5987Value(cyrillic)
	if strings.ContainsAny(encoded, "\r\n\"") {
		t.Errorf("encoded value carries unsafe bytes: %q", encoded)
	}

	decoded, err := url.PathUnescape(encoded)
	if err != nil {
		t.Fatalf("PathUnescape: %v", err)
	}

	if decoded != cyrillic {
		t.Errorf("round-trip = %q, want %q", decoded, cyrillic)
	}
}

func TestArtifactHandler_PreservesCyrillicFilename(t *testing.T) {
	t.Parallel()

	store := artifact.NewStore(time.Minute)

	art, err := store.Put("Матрица.torrent", []byte("d"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /artifacts/{id}", artifactHandler(testLogger(), store))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/artifacts/"+art.Token, nil))

	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "filename*=UTF-8''"+rfc5987Value("Матрица.torrent")) {
		t.Errorf("Content-Disposition lacks the RFC 5987 Cyrillic name: %q", cd)
	}
}

// failingWriter is an http.ResponseWriter whose body write always fails.
type failingWriter struct{ header http.Header }

func (f *failingWriter) Header() http.Header {
	if f.header == nil {
		f.header = http.Header{}
	}

	return f.header
}

func (f *failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (f *failingWriter) WriteHeader(int) {}

func TestArtifactHandler_WriteFailureStillConsumes(t *testing.T) {
	t.Parallel()

	store := artifact.NewStore(time.Minute)

	art, err := store.Put("x.torrent", []byte("d"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/artifacts/"+art.Token, nil)
	req.SetPathValue("id", art.Token)

	// A failed transfer must still consume the token (deliberate one-time).
	artifactHandler(testLogger(), store)(&failingWriter{}, req)

	if _, ok := store.Take(art.Token); ok {
		t.Error("token must be consumed even when the transfer fails")
	}
}

func TestArtifactHandler_SanitizesHostileFilename(t *testing.T) {
	t.Parallel()

	store := artifact.NewStore(time.Minute)
	// A filename crafted to inject a header or escape the directory.
	art, err := store.Put("evil\".torrent\r\nSet-Cookie: pwned=1", []byte("d"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /artifacts/{id}", artifactHandler(testLogger(), store))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/artifacts/"+art.Token, nil))

	cd := rec.Header().Get("Content-Disposition")
	if strings.ContainsAny(cd, "\r\n") {
		t.Errorf("Content-Disposition carries CR/LF: %q", cd)
	}

	if rec.Header().Get("Set-Cookie") != "" {
		t.Error("header injection succeeded: Set-Cookie was set")
	}

	// The quote inside the filename must be neutralised so it cannot break out.
	if strings.Count(cd, `"`) != 2 {
		t.Errorf("Content-Disposition has unbalanced quotes: %q", cd)
	}
}

// testLogger discards log output to keep test runs quiet.
func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestRequireCredentials(t *testing.T) {
	t.Parallel()

	if err := requireCredentials(&config.Config{}); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("no credentials: expected ErrNoCredentials, got %v", err)
	}

	if err := requireCredentials(&config.Config{Cookie: "bb_session=x"}); err != nil {
		t.Errorf("cookie override: expected nil, got %v", err)
	}

	if err := requireCredentials(&config.Config{Username: "u", Password: "p"}); err != nil {
		t.Errorf("username+password: expected nil, got %v", err)
	}
}

func TestRegisterTools_ListsAllTools(t *testing.T) {
	t.Parallel()

	client, err := rutracker.New(&rutracker.Options{})
	if err != nil {
		t.Fatalf("rutracker.New: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: "test"}, newServerOptions(testLogger()))
	registerTools(server, client, artifact.NewStore(time.Minute), &config.Config{})

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer func() { _ = serverSession.Close() }()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)

	clientSession, err := mcpClient.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = clientSession.Close() }()

	result, err := clientSession.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	got := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		got[tool.Name] = true
	}

	want := []string{
		"rutracker_server_version",
		"rutracker_search",
		"rutracker_topic_info",
		"rutracker_files",
		"rutracker_magnet",
		"rutracker_download",
	}

	if len(got) != len(want) {
		t.Errorf("tool count = %d, want %d (%v)", len(got), len(want), result.Tools)
	}

	for _, name := range want {
		if !got[name] {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestNewServerOptions_HasInstructions(t *testing.T) {
	t.Parallel()

	opts := newServerOptions(testLogger())
	if opts.Instructions == "" {
		t.Error("server instructions must not be empty")
	}

	if opts.Logger == nil {
		t.Error("server logger must be set")
	}

	// The instructions must describe the current download delivery, not the old
	// always-base64 behaviour the agent reads to decide how to call the tool.
	if strings.Contains(opts.Instructions, "returned as base64") {
		t.Error("instructions still describe the removed always-base64 download")
	}

	if !strings.Contains(opts.Instructions, "mode") {
		t.Error("instructions should mention the download 'mode' parameter")
	}
}
