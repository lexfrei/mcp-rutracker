package rutracker

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

const (
	testSession   = "bb_session=server-issued-session"
	testUsername  = "lexfrei"
	testPassword  = "secret"
	testTopicID   = 6543210
	testTitle     = "Матрица / The Matrix (1999) BDRemux"
	testForum     = "Зарубежное кино"
	testForumID   = 2484
	testSizeBytes = int64(32212254720)
	testInfoHash  = "ABCDEF0123456789ABCDEF0123456789ABCDEF01"
)

const searchHTML = `<html><body><table id="tor-tbl"><tbody>
<tr class="tCenter hl-tr">
  <td class="f-name-col"><div class="f-name"><a class="gen f" href="tracker.php?f=2484">Зарубежное кино</a></div></td>
  <td class="t-title-col"><div class="t-title"><a data-topic_id="6543210" class="tLink" href="viewtopic.php?t=6543210">Матрица / The Matrix (1999) BDRemux</a></div></td>
  <td class="u-name-col"><div class="u-name"><a href="tracker.php?pid=1">uploader_x</a></div></td>
  <td class="tor-size" data-ts_text="32212254720"><a class="tr-dl" href="dl.php?t=6543210">30&nbsp;GB</a></td>
  <td class="nowrap"><b class="seedmed">123</b></td>
  <td class="leechmed">7</td>
  <td class="number-format" data-ts_text="4567">4567</td>
  <td class="nowrap" data-ts_text="1700000000"><p>2023-11-14</p></td>
</tr>
<tr class="tCenter hl-tr">
  <td class="f-name-col"><div class="f-name"><a class="gen f" href="tracker.php?f=2484">Зарубежное кино</a></div></td>
  <td class="t-title-col"><div class="t-title"><a data-topic_id="6543211" class="tLink" href="viewtopic.php?t=6543211">Матрица: Перезагрузка (2003)</a></div></td>
  <td class="u-name-col"><div class="u-name"><a href="tracker.php?pid=2">uploader_y</a></div></td>
  <td class="tor-size" data-ts_text="16106127360"><a class="tr-dl" href="dl.php?t=6543211">15&nbsp;GB</a></td>
  <td class="nowrap"><b class="seedmed">45</b></td>
  <td class="leechmed">3</td>
  <td class="number-format" data-ts_text="999">999</td>
  <td class="nowrap" data-ts_text="1699900000"><p>2023-11-13</p></td>
</tr>
</tbody></table></body></html>`

const topicHTML = `<html><body>
<h1 class="maintitle"><a id="topic-title" href="viewtopic.php?t=6543210">Матрица / The Matrix (1999) BDRemux</a></h1>
<a class="magnet-link" href="magnet:?xt=urn:btih:abcdef0123456789abcdef0123456789abcdef01&amp;dn=Matrix&amp;xl=32212254720">Скачать по magnet-ссылке</a>
<span id="tor-size-humn" class="size-humn" data-ts_text="32212254720">30&nbsp;GB</span>
<span class="seed"><b>123</b></span>
<span class="leech"><b>7</b></span>
<div class="post_body">Описание раздачи: отличное качество видео и звука.</div>
</body></html>`

const loginPageHTML = `<html><body><form id="login-form" action="login.php" method="post">
<input name="login_username"><input name="login_password"><input type="submit" name="login" value="Вход"></form></body></html>`

const captchaPageHTML = `<html><body><form id="login-form" action="login.php" method="post">
<img src="/captcha/12345.jpg"><input name="cap_sid" value="abc"><input name="cap_code_xyz">
<input name="login_username"><input type="submit" name="login" value="Вход"></form></body></html>`

// torrentBytes is a minimal bencoded payload that begins with a dict marker.
var torrentBytes = []byte("d8:announce0:4:infod6:lengthi100e4:name4:teste e")

// mockServer is a configurable rutracker stand-in for tests.
type mockServer struct {
	server      *httptest.Server
	loginCount  int
	trackerHits int

	// loginIssuesCookie controls whether a POST to login.php sets bb_session.
	loginIssuesCookie bool
	// loginBody is the page returned on a failed login (captcha vs plain).
	loginBody string
	// firstTrackerRedirects makes the first tracker.php hit redirect to login.
	firstTrackerRedirects bool
	// downloadReturnsHTML makes dl.php return an HTML page instead of a torrent,
	// simulating a stale session that yields a login page with status 200.
	downloadReturnsHTML bool
}

func newMockServer(t *testing.T) *mockServer {
	t.Helper()

	mock := &mockServer{loginIssuesCookie: true, loginBody: loginPageHTML}
	mux := http.NewServeMux()

	mux.HandleFunc("/forum/login.php", mock.handleLogin)
	mux.HandleFunc("/forum/tracker.php", mock.handleTracker)
	mux.HandleFunc("/forum/viewtopic.php", mock.handleTopic)
	mux.HandleFunc("/forum/dl.php", mock.handleDownload)

	mock.server = httptest.NewTLSServer(mux)
	t.Cleanup(mock.server.Close)

	return mock
}

func (m *mockServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeCP1251(w, http.StatusOK, loginPageHTML)

		return
	}

	m.loginCount++

	if m.loginIssuesCookie {
		http.SetCookie(w, &http.Cookie{Name: "bb_session", Value: "server-issued-session", Path: "/"})
		writeCP1251(w, http.StatusOK, "ok")

		return
	}

	writeCP1251(w, http.StatusOK, m.loginBody)
}

func (m *mockServer) handleTracker(w http.ResponseWriter, r *http.Request) {
	m.trackerHits++

	if m.firstTrackerRedirects && m.trackerHits == 1 {
		http.Redirect(w, r, "/forum/login.php", http.StatusFound)

		return
	}

	writeCP1251(w, http.StatusOK, searchHTML)
}

func (m *mockServer) handleTopic(w http.ResponseWriter, _ *http.Request) {
	writeCP1251(w, http.StatusOK, topicHTML)
}

func (m *mockServer) handleDownload(w http.ResponseWriter, _ *http.Request) {
	if m.downloadReturnsHTML {
		writeCP1251(w, http.StatusOK, loginPageHTML)

		return
	}

	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", `attachment; filename="matrix.torrent"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(torrentBytes)
}

// writeCP1251 writes an HTML body encoded as windows-1251.
func writeCP1251(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=windows-1251")
	w.WriteHeader(status)

	out, _, err := transform.Bytes(charmap.Windows1251.NewEncoder(), []byte(body))
	if err != nil {
		out = []byte(body)
	}

	_, _ = w.Write(out)
}

func (m *mockServer) newScraper(t *testing.T, opts *Options) *Scraper {
	t.Helper()

	opts.BaseURL = m.server.URL + "/forum/"
	opts.Transport = m.server.Client().Transport

	scraper, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return scraper
}

func TestNew_InvalidBaseURL(t *testing.T) {
	t.Parallel()

	_, err := New(&Options{BaseURL: "not-a-url"})
	if !errors.Is(err, ErrInvalidBaseURL) {
		t.Fatalf("expected ErrInvalidBaseURL, got %v", err)
	}
}

func TestSearch_ParsesRowsAndDecodesCyrillic(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	scraper := mock.newScraper(t, &Options{Cookie: testSession})

	results, err := scraper.Search(t.Context(), "матрица", SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	first := results[0]
	if first.TopicID != testTopicID {
		t.Errorf("TopicID = %d, want %d", first.TopicID, testTopicID)
	}

	if first.Title != testTitle {
		t.Errorf("Title = %q, want %q", first.Title, testTitle)
	}

	if first.Forum != testForum {
		t.Errorf("Forum = %q, want %q (cyrillic decode failed?)", first.Forum, testForum)
	}

	if first.ForumID != testForumID {
		t.Errorf("ForumID = %d, want %d", first.ForumID, testForumID)
	}

	if first.SizeBytes != testSizeBytes {
		t.Errorf("SizeBytes = %d, want %d", first.SizeBytes, testSizeBytes)
	}

	if first.Seeders != 123 || first.Leechers != 7 || first.Downloads != 4567 {
		t.Errorf("seeders/leechers/downloads = %d/%d/%d, want 123/7/4567",
			first.Seeders, first.Leechers, first.Downloads)
	}

	if first.Author != "uploader_x" {
		t.Errorf("Author = %q, want uploader_x", first.Author)
	}

	if first.Added.IsZero() {
		t.Error("Added is zero, want parsed timestamp")
	}

	if !strings.Contains(first.URL, "viewtopic.php?t=6543210") {
		t.Errorf("URL = %q, want a viewtopic link", first.URL)
	}
}

func TestSearch_RespectsLimit(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	scraper := mock.newScraper(t, &Options{Cookie: testSession})

	results, err := scraper.Search(t.Context(), "матрица", SearchOptions{Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result with limit, got %d", len(results))
	}
}

func TestCookieOverride_SkipsLogin(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	scraper := mock.newScraper(t, &Options{Cookie: testSession})

	_, err := scraper.Search(t.Context(), "матрица", SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if mock.loginCount != 0 {
		t.Errorf("expected no login with cookie override, got %d logins", mock.loginCount)
	}
}

func TestLogin_Success(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	scraper := mock.newScraper(t, &Options{Username: testUsername, Password: testPassword})

	_, err := scraper.Search(t.Context(), "матрица", SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if mock.loginCount != 1 {
		t.Errorf("expected exactly 1 login, got %d", mock.loginCount)
	}
}

func TestLogin_Captcha(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	mock.loginIssuesCookie = false
	mock.loginBody = captchaPageHTML
	scraper := mock.newScraper(t, &Options{Username: testUsername, Password: testPassword})

	_, err := scraper.Search(t.Context(), "матрица", SearchOptions{})
	if !errors.Is(err, ErrCaptcha) {
		t.Fatalf("expected ErrCaptcha, got %v", err)
	}
}

func TestLogin_BadCredentials(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	mock.loginIssuesCookie = false
	mock.loginBody = loginPageHTML
	scraper := mock.newScraper(t, &Options{Username: testUsername, Password: "wrong"})

	_, err := scraper.Search(t.Context(), "матрица", SearchOptions{})
	if !errors.Is(err, ErrLoginFailed) {
		t.Fatalf("expected ErrLoginFailed, got %v", err)
	}
}

func TestSearch_NoCredentials(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	scraper := mock.newScraper(t, &Options{})

	_, err := scraper.Search(t.Context(), "матрица", SearchOptions{})
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("expected ErrNoCredentials, got %v", err)
	}
}

func TestReauth_OnSessionExpiry(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	mock.firstTrackerRedirects = true
	scraper := mock.newScraper(t, &Options{Username: testUsername, Password: testPassword})

	results, err := scraper.Search(t.Context(), "матрица", SearchOptions{})
	if err != nil {
		t.Fatalf("Search after reauth: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected results after reauth")
	}

	if mock.loginCount != 2 {
		t.Errorf("expected 2 logins (initial + reauth), got %d", mock.loginCount)
	}
}

func TestTopicInfo_ParsesMagnetSizeAndHash(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	scraper := mock.newScraper(t, &Options{Cookie: testSession})

	info, err := scraper.TopicInfo(t.Context(), testTopicID)
	if err != nil {
		t.Fatalf("TopicInfo: %v", err)
	}

	if info.Title != testTitle {
		t.Errorf("Title = %q, want %q", info.Title, testTitle)
	}

	if !strings.HasPrefix(info.Magnet, "magnet:?xt=urn:btih:") {
		t.Errorf("Magnet = %q, want a magnet URI", info.Magnet)
	}

	if info.InfoHash != testInfoHash {
		t.Errorf("InfoHash = %q, want %q", info.InfoHash, testInfoHash)
	}

	if info.SizeBytes != testSizeBytes {
		t.Errorf("SizeBytes = %d, want %d (from magnet xl)", info.SizeBytes, testSizeBytes)
	}

	if info.Seeders != 123 || info.Leechers != 7 {
		t.Errorf("seeders/leechers = %d/%d, want 123/7", info.Seeders, info.Leechers)
	}

	if !strings.Contains(info.Description, "отличное качество") {
		t.Errorf("Description = %q, want decoded cyrillic text", info.Description)
	}
}

func TestMagnet_ReturnsLink(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	scraper := mock.newScraper(t, &Options{Cookie: testSession})

	magnet, err := scraper.Magnet(t.Context(), testTopicID)
	if err != nil {
		t.Fatalf("Magnet: %v", err)
	}

	if magnetInfoHash(magnet) != testInfoHash {
		t.Errorf("magnet hash = %q, want %q", magnetInfoHash(magnet), testInfoHash)
	}
}

func TestDownloadTorrent_ReturnsBencodeAndFilename(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	scraper := mock.newScraper(t, &Options{Cookie: testSession})

	file, err := scraper.DownloadTorrent(t.Context(), testTopicID)
	if err != nil {
		t.Fatalf("DownloadTorrent: %v", err)
	}

	if file.Filename != "matrix.torrent" {
		t.Errorf("Filename = %q, want matrix.torrent", file.Filename)
	}

	if len(file.Content) == 0 || file.Content[0] != 'd' {
		t.Errorf("Content is not bencode (first byte %v)", file.Content[:1])
	}

	if file.SizeBytes != len(file.Content) {
		t.Errorf("SizeBytes = %d, want %d", file.SizeBytes, len(file.Content))
	}
}

func TestDownloadTorrent_NonBencodeIsAuthError(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	mock.downloadReturnsHTML = true
	scraper := mock.newScraper(t, &Options{Cookie: testSession})

	_, err := scraper.DownloadTorrent(t.Context(), testTopicID)
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("expected ErrNotAuthenticated for non-bencode, got %v", err)
	}
}

func TestSessionPersistence_RoundTrip(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	cookiePath := filepath.Join(t.TempDir(), "cookies.json")

	first := mock.newScraper(t, &Options{Username: testUsername, Password: testPassword, CookiePath: cookiePath})

	_, err := first.Search(t.Context(), "матрица", SearchOptions{})
	if err != nil {
		t.Fatalf("first Search: %v", err)
	}

	// A second scraper with no credentials must reuse the persisted session.
	second := mock.newScraper(t, &Options{CookiePath: cookiePath})

	_, err = second.Search(t.Context(), "матрица", SearchOptions{})
	if err != nil {
		t.Fatalf("second Search (persisted session): %v", err)
	}

	if mock.loginCount != 1 {
		t.Errorf("expected exactly 1 login across both scrapers, got %d", mock.loginCount)
	}
}
