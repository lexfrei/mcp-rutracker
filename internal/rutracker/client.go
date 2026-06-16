package rutracker

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/lexfrei/mcp-rutracker/internal/torrentmeta"
)

// DefaultBaseURL is the canonical rutracker forum root. Mirrors such as
// rutracker.net or rutracker.nl can be supplied via Options.BaseURL.
const DefaultBaseURL = "https://rutracker.org/forum/"

// defaultTimeout bounds a single HTTP request/response cycle.
const defaultTimeout = 30 * time.Second

// sessionCookieName is the cookie rutracker sets on a successful login.
const sessionCookieName = "bb_session"

// Client is the behaviour the MCP tools depend on. It is satisfied by *Scraper
// and mocked in tests.
type Client interface {
	// Search returns torrents matching query, tuned by opts.
	Search(ctx context.Context, query string, opts SearchOptions) ([]Torrent, error)
	// TopicInfo returns the detailed view of a single topic.
	TopicInfo(ctx context.Context, topicID int) (*TopicInfo, error)
	// DownloadTorrent fetches the raw .torrent file for a topic.
	DownloadTorrent(ctx context.Context, topicID int) (*TorrentFile, error)
	// Magnet resolves the magnet link for a topic.
	Magnet(ctx context.Context, topicID int) (string, error)
	// TorrentFiles downloads and decodes the topic's .torrent into its file list.
	TorrentFiles(ctx context.Context, topicID int) (*torrentmeta.Meta, error)
}

// Options configures a Scraper. Either Cookie or Username+Password must be set
// for the client to authenticate.
type Options struct {
	// BaseURL overrides DefaultBaseURL (e.g. a mirror).
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
	base       *url.URL
	http       Doer
	jar        *cookiejar.Jar
	username   string
	password   string
	cookie     string
	cookiePath string
	userAgent  string

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

	rawBase := opts.BaseURL
	if rawBase == "" {
		rawBase = DefaultBaseURL
	}

	base, err := url.Parse(rawBase)
	if err != nil {
		return nil, errors.Wrap(err, "parse base URL")
	}

	if base.Scheme == "" || base.Host == "" {
		return nil, ErrInvalidBaseURL
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, errors.Wrap(err, "create cookie jar")
	}

	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	httpClient := &http.Client{
		Jar:       jar,
		Timeout:   defaultTimeout,
		Transport: opts.Transport,
	}

	scraper := &Scraper{
		base:       base,
		http:       httpClient,
		jar:        jar,
		username:   opts.Username,
		password:   opts.Password,
		cookie:     opts.Cookie,
		cookiePath: opts.CookiePath,
		userAgent:  userAgent,
	}

	scraper.seedCookies()

	return scraper, nil
}

// authed runs op after ensuring a session exists. If op reports the session
// expired mid-flight, it re-authenticates once and retries.
func runAuthed[T any](ctx context.Context, scraper *Scraper, operation func() (T, error)) (T, error) {
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

// hasSessionCookie reports whether the jar currently holds a rutracker session.
func (s *Scraper) hasSessionCookie() bool {
	if s.jar == nil {
		return false
	}

	for _, cookie := range s.jar.Cookies(s.base) {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return true
		}
	}

	return false
}
