package rutracker

import "github.com/cockroachdb/errors"

// ErrNotAuthenticated indicates the current session is missing or expired and
// the request landed on the login page instead of the requested content.
var ErrNotAuthenticated = errors.New("not authenticated: session missing or expired")

// ErrCaptcha indicates rutracker demanded a captcha during login. The caller
// must obtain a session cookie manually (e.g. from a browser) and provide it
// via RUTRACKER_COOKIE.
var ErrCaptcha = errors.New("login requires a captcha: provide a session cookie via RUTRACKER_COOKIE")

// ErrLoginFailed indicates the credentials were rejected by rutracker.
var ErrLoginFailed = errors.New("login failed: invalid credentials")

// ErrNoCredentials indicates neither a session cookie nor username/password
// were configured, so the client cannot authenticate.
var ErrNoCredentials = errors.New("no credentials: set RUTRACKER_USERNAME/RUTRACKER_PASSWORD or RUTRACKER_COOKIE")

// ErrNotFound indicates the requested topic does not exist or is not visible.
var ErrNotFound = errors.New("topic not found")

// ErrParse indicates the page structure did not match the expected layout,
// usually because rutracker changed its markup.
var ErrParse = errors.New("failed to parse rutracker response")

// ErrInvalidBaseURL indicates the configured base URL lacks a scheme or host.
var ErrInvalidBaseURL = errors.New("invalid base URL: scheme and host are required")

// ErrInsecureBaseURL indicates the configured base URL is not HTTPS; rutracker
// is HTTPS-only and an http base would silently drop the secure session cookie.
var ErrInsecureBaseURL = errors.New("base URL must use https")

// ErrMirrorUnavailable indicates a transport error or 5xx response from the
// current mirror, signalling the client to fail over to the next base URL.
var ErrMirrorUnavailable = errors.New("rutracker mirror unavailable")
