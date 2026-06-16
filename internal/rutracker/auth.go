package rutracker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/cockroachdb/errors"
)

// loginPath is the rutracker login endpoint, relative to the forum base.
const loginPath = "/forum/login.php"

// loginSubmitValue is the value of the form's submit button. rutracker checks
// for the presence of the "login" field; the Cyrillic value matches a browser.
const loginSubmitValue = "вход"

// File permissions for the persisted session file and its parent directory.
const (
	cookieDirPerm  = 0o700
	cookieFilePerm = 0o600
)

// persistedCookie is the on-disk representation of a single session cookie.
type persistedCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// seedCookies populates the jar from the raw cookie override and, failing that,
// the persisted session file. It never logs in; that is deferred to ensureAuth.
func (s *Scraper) seedCookies() {
	if s.cookie != "" {
		s.jar.SetCookies(s.base, parseCookieHeader(s.cookie))

		return
	}

	s.loadCookies()
}

// ensureAuth guarantees a usable session before the first protected request.
func (s *Scraper) ensureAuth(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.authed || s.hasSessionCookie() {
		s.authed = true

		return nil
	}

	return s.login(ctx)
}

// reauth forces a fresh login after a session expired mid-flight. A client
// configured with only a raw cookie (no password) cannot recover and surfaces
// ErrNotAuthenticated.
func (s *Scraper) reauth(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.authed = false

	if s.username == "" || s.password == "" {
		return ErrNotAuthenticated
	}

	return s.login(ctx)
}

// login submits the credentials form and verifies a session cookie was issued.
// It must be called with s.mu held.
func (s *Scraper) login(ctx context.Context) error {
	if s.username == "" || s.password == "" {
		return ErrNoCredentials
	}

	form := url.Values{
		"login_username": {s.username},
		"login_password": {s.password},
		"login":          {loginSubmitValue},
	}

	body, err := encodeValues(form)
	if err != nil {
		return err
	}

	req, err := s.newRequest(ctx, http.MethodPost, s.resolve(loginPath, ""), strings.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.http.Do(req)
	if err != nil {
		return errors.Wrap(err, "POST login.php")
	}

	defer func() { _ = resp.Body.Close() }()

	if s.hasSessionCookie() {
		s.authed = true
		s.saveCookies()

		return nil
	}

	return classifyLoginFailure(resp.Body)
}

// classifyLoginFailure inspects a failed login response body to distinguish a
// captcha challenge from rejected credentials.
func classifyLoginFailure(body io.Reader) error {
	doc, err := goquery.NewDocumentFromReader(decodeWindows1251(body))
	if err != nil {
		return errors.Wrap(err, "parse login response")
	}

	if doc.Find("img[src*='captcha'], input[name*='cap_code'], input[name='cap_sid']").Length() > 0 {
		return ErrCaptcha
	}

	return ErrLoginFailed
}

// sessionCookie builds a rutracker cookie with conservative attributes so the
// jar stores and replays it like a browser would.
func sessionCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

// parseCookieHeader turns a "name=value; name2=value2" header into cookies.
func parseCookieHeader(header string) []*http.Cookie {
	var cookies []*http.Cookie

	for part := range strings.SplitSeq(header, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		name, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}

		cookies = append(cookies, sessionCookie(strings.TrimSpace(name), strings.TrimSpace(value)))
	}

	return cookies
}

// loadCookies restores persisted cookies into the jar, if a session file exists.
func (s *Scraper) loadCookies() {
	if s.cookiePath == "" {
		return
	}

	data, err := os.ReadFile(s.cookiePath)
	if err != nil {
		return
	}

	var stored []persistedCookie

	jsonErr := json.Unmarshal(data, &stored)
	if jsonErr != nil {
		return
	}

	cookies := make([]*http.Cookie, 0, len(stored))
	for _, item := range stored {
		cookies = append(cookies, sessionCookie(item.Name, item.Value))
	}

	s.jar.SetCookies(s.base, cookies)
}

// saveCookies persists the current jar cookies for the base domain.
func (s *Scraper) saveCookies() {
	if s.cookiePath == "" {
		return
	}

	jarCookies := s.jar.Cookies(s.base)
	stored := make([]persistedCookie, 0, len(jarCookies))

	for _, cookie := range jarCookies {
		stored = append(stored, persistedCookie{Name: cookie.Name, Value: cookie.Value})
	}

	data, err := json.Marshal(stored)
	if err != nil {
		return
	}

	mkErr := os.MkdirAll(filepath.Dir(s.cookiePath), cookieDirPerm)
	if mkErr != nil {
		return
	}

	_ = os.WriteFile(s.cookiePath, data, cookieFilePerm)
}
