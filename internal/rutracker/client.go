package rutracker

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
)

// defaultTimeout bounds a single HTTP request/response cycle.
const defaultTimeout = 30 * time.Second

// sessionCookieName is the cookie rutracker sets on a successful login.
const sessionCookieName = "bb_session"

// defaultMirrors lists the rutracker base URLs tried, in order, when no explicit
// BaseURL is configured. The client fails over to the next on a network or 5xx
// error (rutracker.org frequently answers 52x from behind Cloudflare).
func defaultMirrors() []string {
	return []string{
		"https://rutracker.org/forum/",
		"https://rutracker.net/forum/",
	}
}

// Client is the behaviour the MCP tools depend on. It is satisfied by *Scraper
// and mocked in tests.
type Client interface {
	// Search returns torrents matching query, tuned by opts.
	Search(ctx context.Context, query string, opts SearchOptions) ([]Torrent, error)
	// TopicInfo returns the detailed view of a single topic.
	TopicInfo(ctx context.Context, topicID int) (*TopicInfo, error)
	// Files returns the topic's file list from the topic page (no download).
	Files(ctx context.Context, topicID int) (*FileList, error)
	// DownloadTorrent fetches the raw .torrent file for a topic.
	DownloadTorrent(ctx context.Context, topicID int) (*TorrentFile, error)
	// Magnet resolves the magnet link for a topic.
	Magnet(ctx context.Context, topicID int) (string, error)
}

// Options configures a Scraper. Either Cookie or Username+Password must be set
// for the client to authenticate.
type Options struct {
	// BaseURL pins a single rutracker base (e.g. a mirror). When empty, the
	// client round-robins over defaultMirrors on failure.
	BaseURL string
	// Username and Password authenticate via the login form.
	Username string
	Password string
	// Cookie is a raw Cookie header (e.g. "bb_session=...") used instead of, or
	// before, a password login. Lets the user bypass captcha by pasting a
	// browser session.
	Cookie string
	// CookiePath persists the session between runs (empty disables persistence).
	CookiePath string
	// UserAgent overrides defaultUserAgent.
	UserAgent string
	// Transport overrides the HTTP round-tripper while leaving cookie-jar
	// ownership with the Scraper. Use it for a proxy, custom TLS, or a
	// TLS-impersonating round-tripper (e.g. utls).
	Transport http.RoundTripper
}

// Scraper is the concrete rutracker.Client backed by net/http.
type Scraper struct {
	bases      []*url.URL
	active     atomic.Int32
	http       Doer
	jar        *cookiejar.Jar
	username   string
	password   string
	cookie     string
	cookiePath string
	userAgent  string

	// maxTorrentBytes caps a download body; overridable in tests.
	maxTorrentBytes int64

	mu     sync.Mutex
	authed bool
}

// New builds a Scraper from opts. It seeds the cookie jar from the raw cookie
// override and the on-disk session file, but defers the actual login until the
// first request.
func New(opts *Options) (*Scraper, error) {
	if opts == nil {
		opts = &Options{}
	}

	bases, err := parseBases(opts.BaseURL)
	if err != nil {
		return nil, err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, errors.Wrap(err, "create cookie jar")
	}

	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	scraper := &Scraper{
		bases: bases,
		http: &http.Client{
			Jar:       jar,
			Timeout:   defaultTimeout,
			Transport: opts.Transport,
		},
		jar:             jar,
		username:        opts.Username,
		password:        opts.Password,
		cookie:          opts.Cookie,
		cookiePath:      opts.CookiePath,
		userAgent:       userAgent,
		maxTorrentBytes: maxTorrentSize,
	}

	scraper.seedCookies()

	return scraper, nil
}

// parseBases resolves the configured base into one or more validated URLs.
func parseBases(rawBase string) ([]*url.URL, error) {
	raw := []string{rawBase}
	if rawBase == "" {
		raw = defaultMirrors()
	}

	bases := make([]*url.URL, 0, len(raw))

	for _, candidate := range raw {
		parsed, err := url.Parse(candidate)
		if err != nil {
			return nil, errors.Wrapf(err, "parse base URL %q", candidate)
		}

		if parsed.Scheme == "" || parsed.Host == "" {
			return nil, ErrInvalidBaseURL
		}

		// rutracker and its mirrors are HTTPS-only. Rejecting http bases avoids
		// silently dropping the Secure session cookie (which the jar will not
		// send over http), which would otherwise surface as a confusing
		// ErrNotAuthenticated on every request.
		if parsed.Scheme != "https" {
			return nil, errors.Wrapf(ErrInsecureBaseURL, "%q", candidate)
		}

		bases = append(bases, parsed)
	}

	return bases, nil
}

// currentBase returns the active mirror. It is lock-free so it can be called
// from request paths that already hold the auth mutex.
func (s *Scraper) currentBase() *url.URL {
	return s.bases[s.active.Load()]
}

// advanceMirror moves off the mirror at index failed, but only if it is still
// the active one. The compare-and-swap makes concurrent failovers converge:
// when several goroutines fail on the same mirror at once, exactly one advances
// and the rest become no-ops instead of racing the index forward and back.
func (s *Scraper) advanceMirror(failed int32) {
	//nolint:gosec // G115: bases holds at most a couple of mirrors; the length fits int32.
	count := int32(len(s.bases))
	// With a single pinned base this is a no-op on the index ((failed+1)%1 == 0);
	// it only resets authed, and runAuthed's loop runs exactly once.
	s.active.CompareAndSwap(failed, (failed+1)%count)

	s.mu.Lock()
	s.authed = false
	s.mu.Unlock()
}

// runAuthed ensures a session, runs operation, retries once on session expiry,
// and fails over to the next mirror on network or 5xx errors.
func runAuthed[T any](ctx context.Context, scraper *Scraper, operation func() (T, error)) (T, error) {
	var (
		zero    T
		lastErr error
	)

	for range scraper.bases {
		active := scraper.active.Load()

		result, err := attemptAuthed(ctx, scraper, operation)
		if isMirrorError(err) {
			lastErr = err

			scraper.advanceMirror(active)

			continue
		}

		return result, err
	}

	return zero, lastErr
}

// attemptAuthed runs one mirror's worth of ensure-auth + operation, retrying
// once if the session expired mid-flight.
func attemptAuthed[T any](ctx context.Context, scraper *Scraper, operation func() (T, error)) (T, error) {
	var zero T

	err := scraper.ensureAuth(ctx)
	if err != nil {
		return zero, err
	}

	result, err := operation()
	if errors.Is(err, ErrNotAuthenticated) {
		reErr := scraper.reauth(ctx)
		if reErr != nil {
			return zero, reErr
		}

		return operation()
	}

	return result, err
}

// hasSessionCookie reports whether the jar holds a session for the active mirror.
func (s *Scraper) hasSessionCookie() bool {
	if s.jar == nil {
		return false
	}

	for _, cookie := range s.jar.Cookies(s.currentBase()) {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return true
		}
	}

	return false
}

// isMirrorError reports whether err indicates the current mirror is unreachable
// and the client should fail over to the next one.
func isMirrorError(err error) bool {
	return errors.Is(err, ErrMirrorUnavailable)
}
