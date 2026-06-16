package rutracker_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
	"github.com/lexfrei/mcp-rutracker/internal/torrentmeta"
)

// TestLive exercises the full flow against the real rutracker site. It is
// skipped unless RUTRACKER_LIVE is set, and reads credentials from the
// environment so they never live in the repository:
//
//	RUTRACKER_LIVE=1 RUTRACKER_USERNAME=... RUTRACKER_PASSWORD=... \
//	  RUTRACKER_BASE_URL=https://rutracker.net/forum/ \
//	  go test -run TestLive -count=1 ./internal/rutracker/
func TestLive(t *testing.T) {
	if os.Getenv("RUTRACKER_LIVE") == "" {
		t.Skip("set RUTRACKER_LIVE=1 to run the live integration test")
	}

	client, err := rutracker.New(&rutracker.Options{
		BaseURL:  os.Getenv("RUTRACKER_BASE_URL"),
		Username: os.Getenv("RUTRACKER_USERNAME"),
		Password: os.Getenv("RUTRACKER_PASSWORD"),
		Cookie:   os.Getenv("RUTRACKER_COOKIE"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	results, err := client.Search(ctx, "matrix", rutracker.SearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Search returned no results")
	}

	top := results[0]
	t.Logf("top result: t=%d %q seeders=%d size=%d forum=%q",
		top.TopicID, top.Title, top.Seeders, top.SizeBytes, top.Forum)

	info, err := client.TopicInfo(ctx, top.TopicID)
	if err != nil {
		t.Fatalf("TopicInfo: %v", err)
	}

	t.Logf("topic: size=%d hash=%s magnet=%t", info.SizeBytes, info.InfoHash, info.Magnet != "")

	list, err := client.Files(ctx, top.TopicID)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}

	t.Logf("files: count=%d total=%d first=%q", list.FileCount, list.TotalSizeBytes, firstPath(list))

	file, err := client.DownloadTorrent(ctx, top.TopicID)
	if err != nil {
		t.Fatalf("DownloadTorrent: %v", err)
	}

	if len(file.Content) == 0 || file.Content[0] != 'd' {
		t.Fatalf("download is not a bencoded torrent: %q", file.Filename)
	}

	meta, err := torrentmeta.Parse(file.Content)
	if err != nil {
		t.Fatalf("torrentmeta.Parse: %v", err)
	}

	t.Logf("torrent: name=%q files=%d total=%d hash=%s",
		meta.Name, meta.FileCount, meta.TotalSizeBytes, meta.InfoHash)
}

func firstPath(list *rutracker.FileList) string {
	if len(list.Files) == 0 {
		return ""
	}

	return list.Files[0].Path
}
