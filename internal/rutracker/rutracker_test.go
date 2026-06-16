package rutracker

import (
	"crypto/tls"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cockroachdb/errors"
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
<div class="nav pad_8"><a href="index.php">RuTracker.org</a> » <a href="viewforum.php?f=1457">Зарубежное кино (UHD Video)</a></div>
<h1 class="maintitle"><a id="topic-title" class="topic-title-6543210" href="https://rutracker.org/forum/viewtopic.php?t=6543210">Матрица / The Matrix (1999) BDRemux</a></h1>
<a href="magnet:?xt=urn:btih:abcdef0123456789abcdef0123456789abcdef01&amp;tr=http%3A%2F%2Fbt.t-ru.org%2Fann" class="med magnet-link">Скачать по magnet-ссылке</a>
<span id="tor-size-humn" title="32212254720">30&nbsp;GB</span>
<span class="seed">Сиды:&nbsp; <b>123</b></span>
<span class="leech">Личи:&nbsp; <b>7</b></span>
<div class="post_body">Описание раздачи: отличное качество видео и звука.</div>
</body></html>`

const loginPageHTML = `<html><body><form id="login-form" action="login.php" method="post">
<input name="login_username"><input name="login_password"><input type="submit" name="login" value="Вход"></form></body></html>`

const captchaPageHTML = `<html><body><form id="login-form" action="login.php" method="post">
<img src="/captcha/12345.jpg"><input name="cap_sid" value="abc"><input name="cap_code_xyz">
<input name="login_username"><input type="submit" name="login" value="Вход"></form></body></html>`

// fileTreeHTML mimics the UTF-8 viewtorrent.php fragment: one folder with two
// files plus a top-level file. Cyrillic here verifies the fragment is parsed as
// UTF-8 (not windows-1251 like the rest of the site).
const fileTreeHTML = `<ul class="ftree">` +
	`<li class="dir"><div><b>Сезон 1</b><s></s></div>` +
	`<ul>` +
	`<li><div><b>e01.mkv</b><s></s><i>1500000000</i></div></li>` +
	`<li><div><b>e02.mkv</b><s></s><i>1600000000</i></div></li>` +
	`</ul></li>` +
	`<li><div><b>readme.txt</b><s></s><i>1024</i></div></li>` +
	`</ul>`

// torrentBytes is a minimal but valid single-file bencoded .torrent.
var torrentBytes = []byte("d4:infod6:lengthi1024e4:name8:file.txt12:piece lengthi16384eee")

// mockServer is a configurable rutracker stand-in for tests.
type mockServer struct {
	server *httptest.Server
	// Counters are written from concurrent HTTP handler goroutines, so they are
	// atomic to keep the mock race-free under the concurrency tests.
	loginCount  atomic.Int32
	trackerHits atomic.Int32

	// loginIssuesCookie controls whether a POST to login.php sets bb_session.
	loginIssuesCookie bool
	// loginBody is the page returned on a failed login (captcha vs plain).
	loginBody string
	// firstTrackerRedirects makes the first tracker.php hit redirect to login.
	firstTrackerRedirects bool
	// downloadReturnsHTML makes dl.php return an HTML page instead of a torrent,
	// simulating a stale session that yields a login page with status 200.
	downloadReturnsHTML bool
	// emptyFileTree makes viewtorrent.php return a present-but-empty ftree.
	emptyFileTree bool
	// topicNoTitle makes viewtopic.php return a 200 body without a topic title,
	// as an anti-bot interstitial would.
	topicNoTitle bool
}

func newMockServer(t *testing.T) *mockServer {
	t.Helper()

	mock := &mockServer{loginIssuesCookie: true, loginBody: loginPageHTML}
	mux := http.NewServeMux()

	mux.HandleFunc("/forum/login.php", mock.handleLogin)
	mux.HandleFunc("/forum/tracker.php", mock.handleTracker)
	mux.HandleFunc("/forum/viewtopic.php", mock.handleTopic)
	mux.HandleFunc("/forum/viewtorrent.php", mock.handleFileList)
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

	m.loginCount.Add(1)

	if m.loginIssuesCookie {
		http.SetCookie(w, &http.Cookie{Name: "bb_session", Value: "server-issued-session", Path: "/"})
		writeCP1251(w, http.StatusOK, "ok")

		return
	}

	writeCP1251(w, http.StatusOK, m.loginBody)
}

func (m *mockServer) handleTracker(w http.ResponseWriter, r *http.Request) {
	hits := m.trackerHits.Add(1)

	if m.firstTrackerRedirects && hits == 1 {
		http.Redirect(w, r, "/forum/login.php", http.StatusFound)

		return
	}

	writeCP1251(w, http.StatusOK, searchHTML)
}

func (m *mockServer) handleTopic(w http.ResponseWriter, _ *http.Request) {
	if m.topicNoTitle {
		writeCP1251(w, http.StatusOK, `<html><body>checking your browser…</body></html>`)

		return
	}

	writeCP1251(w, http.StatusOK, topicHTML)
}

// handleFileList serves the file tree as UTF-8, matching the real endpoint.
func (m *mockServer) handleFileList(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if m.emptyFileTree {
		_, _ = w.Write([]byte(`<ul class="ftree"></ul>`))

		return
	}

	_, _ = w.Write([]byte(fileTreeHTML))
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

func TestNew_InsecureBaseURL(t *testing.T) {
	t.Parallel()

	_, err := New(&Options{BaseURL: "http://rutracker.org/forum/"})
	if !errors.Is(err, ErrInsecureBaseURL) {
		t.Fatalf("expected ErrInsecureBaseURL for an http base, got %v", err)
	}
}

func TestTopicInfo_NoTitleIsParseError(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	mock.topicNoTitle = true
	scraper := mock.newScraper(t, &Options{Cookie: testSession})

	_, err := scraper.TopicInfo(t.Context(), testTopicID)
	if !errors.Is(err, ErrParse) {
		t.Fatalf("expected ErrParse for a titleless page, got %v", err)
	}
}

func TestSortFieldCode(t *testing.T) {
	t.Parallel()

	cases := map[SortField]string{
		SortDate:      "1",
		SortDownloads: "4",
		SortSize:      "7",
		SortSeeders:   "10",
		"":            "10",
		"bogus":       "10",
	}

	for field, want := range cases {
		if got := sortFieldCode(field); got != want {
			t.Errorf("sortFieldCode(%q) = %q, want %q", field, got, want)
		}
	}
}

func TestSortOrderCode(t *testing.T) {
	t.Parallel()

	cases := map[SortOrder]string{
		OrderAsc:  "1",
		OrderDesc: "2",
		"":        "2",
		"bogus":   "2",
	}

	for order, want := range cases {
		if got := sortOrderCode(order); got != want {
			t.Errorf("sortOrderCode(%q) = %q, want %q", order, got, want)
		}
	}
}

func TestTopicIDFromHref(t *testing.T) {
	t.Parallel()

	if got := topicIDFromHref("viewtopic.php?t=12345"); got != 12345 {
		t.Errorf("topicIDFromHref(valid) = %d, want 12345", got)
	}

	if got := topicIDFromHref("tracker.php?f=10"); got != 0 {
		t.Errorf("topicIDFromHref(no t) = %d, want 0", got)
	}

	if got := topicIDFromHref("::not a url::"); got != 0 {
		t.Errorf("topicIDFromHref(garbage) = %d, want 0", got)
	}
}

func TestSearch_LimitExceedsResults(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	scraper := mock.newScraper(t, &Options{Cookie: testSession})

	// The mock serves 2 rows; a larger limit must return all of them, not error.
	results, err := scraper.Search(t.Context(), "матрица", SearchOptions{Limit: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestEncodeWindows1251_RepresentableAndNot(t *testing.T) {
	t.Parallel()

	// Cyrillic and Latin encode cleanly...
	encoded, err := encodeWindows1251("Матрица")
	if err != nil {
		t.Fatalf("Cyrillic must encode without error, got %v", err)
	}

	if len(encoded) != 7 {
		t.Errorf("encoded length = %d, want 7 (one byte per Cyrillic letter)", len(encoded))
	}

	// ...while runes outside windows-1251 are rejected, not silently mangled
	// into substitution bytes — so a search query is never corrupted. The CJK
	// sample is built from runes to avoid a non-Latin source literal.
	cjk := string([]rune{0x65E5, 0x672C, 0x8A9E})
	for _, unmappable := range []string{cjk, "🎬"} {
		_, err := encodeWindows1251(unmappable)
		if err == nil {
			t.Errorf("expected an error encoding %q, got nil", unmappable)
		}
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

	if mock.loginCount.Load() != 0 {
		t.Errorf("expected no login with cookie override, got %d logins", mock.loginCount.Load())
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

	if mock.loginCount.Load() != 1 {
		t.Errorf("expected exactly 1 login, got %d", mock.loginCount.Load())
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

	if mock.loginCount.Load() != 2 {
		t.Errorf("expected 2 logins (initial + reauth), got %d", mock.loginCount.Load())
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

	if info.Forum != testForum+" (UHD Video)" {
		t.Errorf("Forum = %q, want the last breadcrumb link", info.Forum)
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

	if MagnetInfoHash(magnet) != testInfoHash {
		t.Errorf("magnet hash = %q, want %q", MagnetInfoHash(magnet), testInfoHash)
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

func TestFiles_ParsesTreeFromTopicPage(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	scraper := mock.newScraper(t, &Options{Cookie: testSession})

	list, err := scraper.Files(t.Context(), testTopicID)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}

	if list.FileCount != 3 {
		t.Fatalf("FileCount = %d, want 3", list.FileCount)
	}

	want := []FileEntry{
		{Path: "Сезон 1/e01.mkv", SizeBytes: 1500000000},
		{Path: "Сезон 1/e02.mkv", SizeBytes: 1600000000},
		{Path: "readme.txt", SizeBytes: 1024},
	}

	for i, entry := range want {
		if list.Files[i] != entry {
			t.Errorf("file[%d] = %+v, want %+v", i, list.Files[i], entry)
		}
	}

	if list.TotalSizeBytes != 3100001024 {
		t.Errorf("TotalSizeBytes = %d, want 3100001024", list.TotalSizeBytes)
	}
}

func TestDownloadTorrent_TooLargeIsRejected(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	scraper := mock.newScraper(t, &Options{Cookie: testSession})
	// The mock serves a 62-byte torrent; cap below that to force the over-size
	// path, proving the body is rejected rather than silently truncated.
	scraper.maxTorrentBytes = 10

	_, err := scraper.DownloadTorrent(t.Context(), testTopicID)
	if !errors.Is(err, ErrTorrentTooLarge) {
		t.Fatalf("expected ErrTorrentTooLarge, got %v", err)
	}
}

func TestFiles_EmptyTreeIsNotFound(t *testing.T) {
	t.Parallel()

	mock := newMockServer(t)
	mock.emptyFileTree = true
	// Cookie-only client: a re-login is impossible, so a present-but-empty tree
	// must surface as ErrNotFound rather than a misleading auth error.
	scraper := mock.newScraper(t, &Options{Cookie: testSession})

	_, err := scraper.Files(t.Context(), testTopicID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an empty tree, got %v", err)
	}

	if mock.loginCount.Load() != 0 {
		t.Errorf("empty tree must not trigger a re-login, got %d logins", mock.loginCount.Load())
	}
}

// twoMirrorScraper builds a scraper whose first mirror always answers 5xx (like
// rutracker.org behind Cloudflare) and whose second mirror is a healthy mock.
// It returns the scraper and the healthy mirror's host.
func twoMirrorScraper(t *testing.T) (*Scraper, string) {
	t.Helper()

	bad := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(bad.Close)

	good := newMockServer(t)

	badURL, err := url.Parse(bad.URL + "/forum/")
	if err != nil {
		t.Fatalf("parse bad URL: %v", err)
	}

	goodURL, err := url.Parse(good.server.URL + "/forum/")
	if err != nil {
		t.Fatalf("parse good URL: %v", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}

	insecure := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}

	scraper := &Scraper{
		bases:     []*url.URL{badURL, goodURL},
		http:      &http.Client{Jar: jar, Transport: insecure},
		jar:       jar,
		cookie:    testSession,
		userAgent: defaultUserAgent,
	}
	scraper.seedCookies()

	return scraper, goodURL.Host
}

func TestMirrorFailover_OnServerError(t *testing.T) {
	t.Parallel()

	scraper, goodHost := twoMirrorScraper(t)

	results, err := scraper.Search(t.Context(), "матрица", SearchOptions{})
	if err != nil {
		t.Fatalf("Search across mirrors: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected results from the healthy mirror")
	}

	if scraper.currentBase().Host != goodHost {
		t.Errorf("active mirror = %s, want %s", scraper.currentBase().Host, goodHost)
	}
}

func TestMirrorFailover_StaleCookieReauthsOnNewMirror(t *testing.T) {
	t.Parallel()

	// Realistic production failure: rutracker.org answers 52x, we fail over to
	// rutracker.net, but the pasted/persisted cookie is expired. A client with a
	// username and password must recover by logging in on the new mirror rather
	// than dead-ending on the stale seeded cookie.
	bad := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(bad.Close)

	good := newMockServer(t)
	good.firstTrackerRedirects = true // the stale cookie is rejected once, then login works

	badURL, _ := url.Parse(bad.URL + "/forum/")
	goodURL, _ := url.Parse(good.server.URL + "/forum/")

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}

	insecure := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}

	scraper := &Scraper{
		bases:     []*url.URL{badURL, goodURL},
		http:      &http.Client{Jar: jar, Transport: insecure},
		jar:       jar,
		username:  testUsername,
		password:  testPassword,
		cookie:    "bb_session=stale-and-expired",
		userAgent: defaultUserAgent,
	}
	scraper.seedCookies()

	results, err := scraper.Search(t.Context(), "матрица", SearchOptions{})
	if err != nil {
		t.Fatalf("Search did not recover from a stale cookie after failover: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected results after re-login on the healthy mirror")
	}

	if good.loginCount.Load() != 1 {
		t.Errorf("expected exactly 1 login on the new mirror, got %d", good.loginCount.Load())
	}

	if scraper.currentBase().Host != goodURL.Host {
		t.Errorf("active mirror = %s, want %s", scraper.currentBase().Host, goodURL.Host)
	}
}

func TestMirrorFailover_Concurrent(t *testing.T) {
	t.Parallel()

	scraper, goodHost := twoMirrorScraper(t)

	const workers = 24

	var wg sync.WaitGroup

	errs := make([]error, workers)
	counts := make([]int, workers)

	for i := range workers {
		wg.Go(func() {
			results, err := scraper.Search(t.Context(), "матрица", SearchOptions{})
			errs[i] = err
			counts[i] = len(results)
		})
	}

	wg.Wait()

	for i := range workers {
		if errs[i] != nil {
			t.Errorf("worker %d: %v", i, errs[i])
		}

		if counts[i] == 0 {
			t.Errorf("worker %d: got no results", i)
		}
	}

	if scraper.currentBase().Host != goodHost {
		t.Errorf("active mirror = %s, want %s after concurrent failover", scraper.currentBase().Host, goodHost)
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

	if mock.loginCount.Load() != 1 {
		t.Errorf("expected exactly 1 login across both scrapers, got %d", mock.loginCount.Load())
	}
}
