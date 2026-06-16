package rutracker

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/cockroachdb/errors"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// defaultUserAgent mimics a recent desktop Chrome to reduce anti-bot friction.
// rutracker rejects requests from obviously scripted clients, so a realistic
// User-Agent is the cheapest mitigation short of full TLS impersonation.
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// Doer is the subset of *http.Client the scraper relies on. Hiding the
// transport behind an interface lets callers swap in a TLS-impersonating
// client (e.g. one built on utls) without touching the scraping code.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// decodeWindows1251 wraps a reader so windows-1251 bytes are transformed into
// UTF-8 on the fly. rutracker serves every HTML page in windows-1251.
func decodeWindows1251(reader io.Reader) io.Reader {
	return transform.NewReader(reader, charmap.Windows1251.NewDecoder())
}

// encodeWindows1251 converts a UTF-8 string to windows-1251 bytes returned as a
// Go string (one byte per code unit). Outgoing query/form values containing
// Cyrillic must be encoded this way before percent-escaping.
func encodeWindows1251(value string) (string, error) {
	encoded, _, err := transform.String(charmap.Windows1251.NewEncoder(), value)
	if err != nil {
		return "", errors.Wrap(err, "windows-1251 encode")
	}

	return encoded, nil
}

// encodeValues serialises form/query values as windows-1251 percent-encoded
// pairs. It mirrors url.Values.Encode but encodes Cyrillic in windows-1251
// instead of UTF-8, which is what rutracker expects.
func encodeValues(values url.Values) (string, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	var buf strings.Builder

	for _, key := range keys {
		for _, value := range values[key] {
			encoded, err := encodeWindows1251(value)
			if err != nil {
				return "", err
			}

			if buf.Len() > 0 {
				buf.WriteByte('&')
			}

			buf.WriteString(url.QueryEscape(key))
			buf.WriteByte('=')
			buf.WriteString(url.QueryEscape(encoded))
		}
	}

	return buf.String(), nil
}

// resolve builds an absolute URL for a path under the configured base, with the
// given windows-1251 encoded query already attached.
func (s *Scraper) resolve(path, rawQuery string) string {
	ref := &url.URL{Path: path, RawQuery: rawQuery}

	return s.currentBase().ResolveReference(ref).String()
}

// newRequest creates a request with the configured User-Agent applied.
func (s *Scraper) newRequest(
	ctx context.Context,
	method, target string,
	body io.Reader,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, errors.Wrap(err, "build request")
	}

	req.Header.Set("User-Agent", s.userAgent)

	return req, nil
}

// getDoc fetches an HTML page, decodes it from windows-1251, and parses it.
// It returns ErrNotAuthenticated when the request was redirected to the login
// page, signalling the caller to re-authenticate and retry.
func (s *Scraper) getDoc(ctx context.Context, path string, query url.Values) (*goquery.Document, error) {
	rawQuery, err := encodeValues(query)
	if err != nil {
		return nil, err
	}

	req, err := s.newRequest(ctx, http.MethodGet, s.resolve(path, rawQuery), nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.doRequest(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if isLoginRedirect(resp) {
		return nil, ErrNotAuthenticated
	}

	doc, err := goquery.NewDocumentFromReader(decodeWindows1251(resp.Body))
	if err != nil {
		return nil, errors.Wrap(err, "parse HTML")
	}

	return doc, nil
}

// doRequest performs a request, classifying transport failures and 5xx
// responses as ErrMirrorUnavailable so the caller can fail over to another
// mirror. The caller owns the returned body.
func (s *Scraper) doRequest(req *http.Request) (*http.Response, error) {
	resp, err := s.http.Do(req)
	if err != nil {
		//nolint:wrapcheck // Mark adds the mirror-failover sentinel on top of Wrap.
		return nil, errors.Mark(errors.Wrap(err, req.Method+" "+req.URL.Path), ErrMirrorUnavailable)
	}

	if resp.StatusCode >= http.StatusInternalServerError {
		_ = resp.Body.Close()

		return nil, errors.Wrapf(ErrMirrorUnavailable, "status %d from %s", resp.StatusCode, req.URL.Host)
	}

	return resp, nil
}

// getRaw fetches a binary resource (e.g. a .torrent file). The caller owns the
// returned response body and must close it. It returns ErrNotAuthenticated when
// the request was redirected to the login page.
func (s *Scraper) getRaw(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	rawQuery, err := encodeValues(query)
	if err != nil {
		return nil, err
	}

	req, err := s.newRequest(ctx, http.MethodGet, s.resolve(path, rawQuery), nil)
	if err != nil {
		return nil, err
	}

	// Reference the specific topic page, matching what a browser sends from the
	// download button and keeping parity with the file-list request's Referer.
	refererQuery := ""
	if topic := query.Get("t"); topic != "" {
		refererQuery = "t=" + topic
	}

	req.Header.Set("Referer", s.resolve("/forum/viewtopic.php", refererQuery))

	resp, err := s.doRequest(req)
	if err != nil {
		return nil, err
	}

	if isLoginRedirect(resp) {
		_ = resp.Body.Close()

		return nil, ErrNotAuthenticated
	}

	return resp, nil
}

// isLoginRedirect reports whether a response landed on the login page, which is
// how rutracker signals an expired or missing session for protected resources.
func isLoginRedirect(resp *http.Response) bool {
	if resp.Request == nil || resp.Request.URL == nil {
		return false
	}

	return strings.Contains(resp.Request.URL.Path, "login.php")
}
