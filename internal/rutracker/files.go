package rutracker

import (
	"context"

	"github.com/cockroachdb/errors"

	"github.com/lexfrei/mcp-rutracker/internal/torrentmeta"
)

// TorrentFiles downloads the .torrent for a topic and decodes its contained
// file list, exact sizes, piece length, and canonical info-hash. It reuses
// DownloadTorrent, so it inherits the same authentication and retry behaviour
// and needs no extra request beyond the download itself.
func (s *Scraper) TorrentFiles(ctx context.Context, topicID int) (*torrentmeta.Meta, error) {
	file, err := s.DownloadTorrent(ctx, topicID)
	if err != nil {
		return nil, err
	}

	meta, err := torrentmeta.Parse(file.Content)
	if err != nil {
		return nil, errors.Wrap(err, "parse torrent metadata")
	}

	return meta, nil
}
