package tools_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lexfrei/mcp-rutracker/internal/artifact"
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

func downloadClient() *mockClient {
	return &mockClient{torrentFile: &rutracker.TorrentFile{
		Filename:  torrentName,
		Content:   validTorrent,
		SizeBytes: len(validTorrent),
	}}
}

func TestDownloadHandler_DefaultMetadataInStdio(t *testing.T) {
	t.Parallel()

	// HTTP disabled -> default mode is metadata: enriched info, no content/URL.
	handler := tools.NewDownloadHandler(downloadClient(), artifact.NewStore(time.Minute), "", false)

	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	sum := sha256.Sum256(validTorrent)
	if result.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("SHA256 = %q", result.SHA256)
	}

	if result.FileCount != 1 || result.TotalSizeBytes != 1024 || len(result.InfoHash) != 40 {
		t.Errorf("enrichment failed: %+v", result)
	}

	if result.ContentBase64 != "" || result.DownloadURL != "" {
		t.Errorf("metadata mode must not carry content/URL: %+v", result)
	}
}

func TestDownloadHandler_DefaultArtifactInHTTP(t *testing.T) {
	t.Parallel()

	store := artifact.NewStore(time.Minute)
	handler := tools.NewDownloadHandler(downloadClient(), store, "http://srv:9090", true)

	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if result.ArtifactID == "" || result.ExpiresAt.IsZero() {
		t.Errorf("artifact fields missing: %+v", result)
	}

	if result.DownloadURL != "http://srv:9090/artifacts/"+result.ArtifactID {
		t.Errorf("DownloadURL = %q", result.DownloadURL)
	}

	if result.ContentBase64 != "" {
		t.Error("artifact mode must not include base64")
	}

	// The token must resolve from the store exactly once.
	if _, ok := store.Take(result.ArtifactID); !ok {
		t.Error("stored artifact not retrievable")
	}
}

func TestDownloadHandler_Base64Mode(t *testing.T) {
	t.Parallel()

	handler := tools.NewDownloadHandler(downloadClient(), artifact.NewStore(time.Minute), "http://srv", true)

	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1, Mode: "base64"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	decoded, decErr := base64.StdEncoding.DecodeString(result.ContentBase64)
	if decErr != nil || string(decoded) != string(validTorrent) {
		t.Error("base64 round-trip failed")
	}

	if result.DownloadURL != "" {
		t.Error("base64 mode must not produce a download URL")
	}
}

func TestDownloadHandler_ArtifactWithoutHTTP(t *testing.T) {
	t.Parallel()

	handler := tools.NewDownloadHandler(downloadClient(), artifact.NewStore(time.Minute), "", false)

	result, _, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1, Mode: "artifact"})
	if !errors.Is(err, tools.ErrArtifactUnavailable) || !isError(result) {
		t.Fatalf("expected ErrArtifactUnavailable, got %v", err)
	}
}

func TestDownloadResult_ExpiresAtOmitzero(t *testing.T) {
	t.Parallel()

	store := artifact.NewStore(time.Minute)

	artifactHandler := tools.NewDownloadHandler(downloadClient(), store, "http://srv", true)
	_, artResult, err := artifactHandler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1})
	if err != nil {
		t.Fatalf("artifact handler: %v", err)
	}

	artJSON, _ := json.Marshal(artResult)
	if !strings.Contains(string(artJSON), `"expiresAt"`) {
		t.Errorf("artifact result must serialize expiresAt: %s", artJSON)
	}

	metaHandler := tools.NewDownloadHandler(downloadClient(), store, "", false)
	_, metaResult, err := metaHandler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1})
	if err != nil {
		t.Fatalf("metadata handler: %v", err)
	}

	metaJSON, _ := json.Marshal(metaResult)
	if strings.Contains(string(metaJSON), `"expiresAt"`) {
		t.Errorf("metadata result must omit expiresAt (omitzero): %s", metaJSON)
	}
}

func TestDownloadHandler_InvalidMode(t *testing.T) {
	t.Parallel()

	handler := tools.NewDownloadHandler(downloadClient(), artifact.NewStore(time.Minute), "", false)

	_, _, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1, Mode: "bogus"})
	if !errors.Is(err, tools.ErrInvalidMode) || !errors.Is(err, tools.ErrValidation) {
		t.Fatalf("expected ErrInvalidMode + ErrValidation, got %v", err)
	}
}

func TestDownloadHandler_UnparseableBodyEnrichmentEmpty(t *testing.T) {
	t.Parallel()

	content := []byte("not a valid bencoded torrent")
	client := &mockClient{torrentFile: &rutracker.TorrentFile{Filename: torrentName, Content: content, SizeBytes: len(content)}}
	handler := tools.NewDownloadHandler(client, artifact.NewStore(time.Minute), "http://srv", true)

	_, result, err := handler(t.Context(), &mcp.CallToolRequest{}, tools.DownloadParams{TopicID: 1, Mode: "base64"})
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
