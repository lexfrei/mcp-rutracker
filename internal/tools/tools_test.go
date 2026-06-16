package tools_test

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
	"github.com/lexfrei/mcp-rutracker/internal/tools"
)

// validTorrent is a minimal single-file bencoded .torrent.
var validTorrent = []byte("d4:infod6:lengthi1024e4:name8:file.txt12:piece lengthi16384eee")

const torrentName = "x.torrent"

func isError(result *mcp.CallToolResult) bool {
	return result != nil && result.IsError
}

func TestSearchHandler_Success(t *testing.T) {
	t.Parallel()

	client := &mockClient{searchResults: []rutracker.Torrent{
		{TopicID: 1, Title: "A", Seeders: 10},
		{TopicID: 2, Title: "B", Seeders: 5},
	}}
	handler := tools.NewSearchHandler(client)

	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.SearchParams{
		Query: "matrix",
		Sort:  "seeders",
		Order: "desc",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if result.Count != 2 || len(result.Results) != 2 {
		t.Errorf("Count = %d, want 2", result.Count)
	}

	if client.lastQuery != "matrix" {
		t.Errorf("lastQuery = %q, want matrix", client.lastQuery)
	}

	if client.lastOptions.Sort != rutracker.SortSeeders {
		t.Errorf("Sort = %q, want seeders", client.lastOptions.Sort)
	}
}

func TestSearchHandler_EmptyQuery(t *testing.T) {
	t.Parallel()

	handler := tools.NewSearchHandler(&mockClient{})

	result, _, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.SearchParams{})
	if !errors.Is(err, tools.ErrValidation) || !isError(result) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestSearchHandler_InvalidSort(t *testing.T) {
	t.Parallel()

	handler := tools.NewSearchHandler(&mockClient{})

	_, _, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.SearchParams{
		Query: "x",
		Sort:  "bogus",
	})
	if !errors.Is(err, tools.ErrInvalidSort) {
		t.Fatalf("expected ErrInvalidSort, got %v", err)
	}
}

func TestSearchHandler_ClientError(t *testing.T) {
	t.Parallel()

	handler := tools.NewSearchHandler(&mockClient{err: errMock})

	result, _, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.SearchParams{Query: "x"})
	if !errors.Is(err, tools.ErrRutracker) || !isError(result) {
		t.Fatalf("expected ErrRutracker, got %v", err)
	}
}

func TestTopicInfoHandler_Success(t *testing.T) {
	t.Parallel()

	client := &mockClient{topicInfo: &rutracker.TopicInfo{TopicID: 42, Title: "T", SizeBytes: 1000}}
	handler := tools.NewTopicInfoHandler(client)

	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.TopicInfoParams{TopicID: 42})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if result.TopicID != 42 || result.SizeBytes != 1000 {
		t.Errorf("result = %+v", result)
	}
}

func TestTopicInfoHandler_BadID(t *testing.T) {
	t.Parallel()

	handler := tools.NewTopicInfoHandler(&mockClient{})

	result, _, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.TopicInfoParams{TopicID: 0})
	if !errors.Is(err, tools.ErrValidation) || !isError(result) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestTopicInfoHandler_NilResult(t *testing.T) {
	t.Parallel()

	// A client returning (nil, nil) must yield an error, not panic on deref.
	handler := tools.NewTopicInfoHandler(&mockClient{topicInfo: nil})

	result, _, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.TopicInfoParams{TopicID: 1})
	if !errors.Is(err, tools.ErrEmptyResponse) || !isError(result) {
		t.Fatalf("expected ErrEmptyResponse, got %v", err)
	}
}

func TestFilesHandler_NilResult(t *testing.T) {
	t.Parallel()

	handler := tools.NewFilesHandler(&mockClient{fileList: nil})

	result, _, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.FilesParams{TopicID: 1})
	if !errors.Is(err, tools.ErrEmptyResponse) || !isError(result) {
		t.Fatalf("expected ErrEmptyResponse, got %v", err)
	}
}

func TestFilesHandler_Success(t *testing.T) {
	t.Parallel()

	client := &mockClient{fileList: &rutracker.FileList{
		TopicID:        7,
		FileCount:      2,
		TotalSizeBytes: 300,
		Files: []rutracker.FileEntry{
			{Path: "a", SizeBytes: 100},
			{Path: "b", SizeBytes: 200},
		},
	}}
	handler := tools.NewFilesHandler(client)

	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.FilesParams{TopicID: 7})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if result.FileCount != 2 || result.TotalSizeBytes != 300 {
		t.Errorf("result = %+v", result)
	}
}

func TestFilesHandler_BadID(t *testing.T) {
	t.Parallel()

	handler := tools.NewFilesHandler(&mockClient{})

	_, _, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.FilesParams{TopicID: -1})
	if !errors.Is(err, tools.ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestMagnetHandler_Success(t *testing.T) {
	t.Parallel()

	magnet := "magnet:?xt=urn:btih:abcdef0123456789abcdef0123456789abcdef01&dn=x"
	handler := tools.NewMagnetHandler(&mockClient{magnet: magnet})

	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.MagnetParams{TopicID: 1})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if result.Magnet != magnet {
		t.Errorf("Magnet = %q", result.Magnet)
	}

	if result.InfoHash != "ABCDEF0123456789ABCDEF0123456789ABCDEF01" {
		t.Errorf("InfoHash = %q", result.InfoHash)
	}
}

func TestDownloadHandler_SuccessEnriched(t *testing.T) {
	t.Parallel()

	client := &mockClient{torrentFile: &rutracker.TorrentFile{
		Filename:  torrentName,
		Content:   validTorrent,
		SizeBytes: len(validTorrent),
	}}
	handler := tools.NewDownloadHandler(client, "")

	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	decoded, decErr := base64.StdEncoding.DecodeString(result.ContentBase64)
	if decErr != nil || string(decoded) != string(validTorrent) {
		t.Errorf("base64 round-trip failed")
	}

	if result.FileCount != 1 || result.TotalSizeBytes != 1024 {
		t.Errorf("enrichment failed: %+v", result)
	}

	if len(result.InfoHash) != 40 {
		t.Errorf("InfoHash = %q, want 40 hex chars", result.InfoHash)
	}

	if result.SavedPath != "" {
		t.Errorf("SavedPath = %q, want empty without saveToDisk", result.SavedPath)
	}
}

func TestDownloadHandler_UnparseableBodyStillReturnsBase64(t *testing.T) {
	t.Parallel()

	// A body that is not a valid torrent must still download: the base64 is
	// returned and the enrichment fields stay empty rather than failing.
	content := []byte("not a valid bencoded torrent")
	client := &mockClient{torrentFile: &rutracker.TorrentFile{
		Filename:  torrentName,
		Content:   content,
		SizeBytes: len(content),
	}}
	handler := tools.NewDownloadHandler(client, "")

	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if result.ContentBase64 != base64.StdEncoding.EncodeToString(content) {
		t.Error("base64 content missing for unparseable body")
	}

	if result.InfoHash != "" || result.FileCount != 0 {
		t.Errorf("expected empty enrichment, got hash=%q count=%d", result.InfoHash, result.FileCount)
	}
}

func TestDownloadHandler_SaveToDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	client := &mockClient{torrentFile: &rutracker.TorrentFile{
		Filename:  torrentName,
		Content:   validTorrent,
		SizeBytes: len(validTorrent),
	}}
	handler := tools.NewDownloadHandler(client, dir)

	save := true
	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{
		TopicID:    1,
		SaveToDisk: &save,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if result.SavedPath != filepath.Join(dir, torrentName) {
		t.Errorf("SavedPath = %q", result.SavedPath)
	}
}

func TestDownloadHandler_SaveSanitizesFilename(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// A server-controlled filename with traversal must be reduced to its base
	// name and stay inside the download directory.
	client := &mockClient{torrentFile: &rutracker.TorrentFile{
		Filename:  "../../../etc/evil.torrent",
		Content:   validTorrent,
		SizeBytes: len(validTorrent),
	}}
	handler := tools.NewDownloadHandler(client, dir)

	save := true
	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1, SaveToDisk: &save})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	want := filepath.Join(dir, "evil.torrent")
	if result.SavedPath != want {
		t.Errorf("SavedPath = %q, want %q (traversal not contained)", result.SavedPath, want)
	}

	if filepath.Dir(result.SavedPath) != dir {
		t.Errorf("saved outside the download dir: %q", result.SavedPath)
	}
}

func TestDownloadHandler_SaveNoDir(t *testing.T) {
	t.Parallel()

	client := &mockClient{torrentFile: &rutracker.TorrentFile{Filename: torrentName, Content: validTorrent}}
	handler := tools.NewDownloadHandler(client, "")

	save := true
	result, _, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1, SaveToDisk: &save})
	if !errors.Is(err, tools.ErrNoDownloadDir) || !isError(result) {
		t.Fatalf("expected ErrNoDownloadDir, got %v", err)
	}
}

func TestServerVersionHandler(t *testing.T) {
	t.Parallel()

	handler := tools.NewServerVersionHandler("1.2.3", "abc123", "go1.26.4")

	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.ServerVersionParams{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if result.Version != "1.2.3" || result.Revision != "abc123" {
		t.Errorf("result = %+v", result)
	}

	if !strings.Contains(result.Output, "1.2.3") {
		t.Errorf("Output = %q", result.Output)
	}
}
