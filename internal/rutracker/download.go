package rutracker

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cockroachdb/errors"
)

// downloadPath is the rutracker .torrent download endpoint, relative to base.
const downloadPath = "/forum/dl.php"

// maxTorrentSize caps how much of a download response is read into memory.
// .torrent files are small; this guards against an unexpected large body.
const maxTorrentSize = 32 << 20

// bencodeDictPrefix is the first byte of a valid bencoded .torrent (a dict).
const bencodeDictPrefix = 'd'

// ErrDownloadFailed indicates dl.php returned something other than a .torrent.
var ErrDownloadFailed = errors.New("download did not return a .torrent file")

// DownloadTorrent fetches the .torrent file for a topic, re-authenticating once
// if the session expired.
func (s *Scraper) DownloadTorrent(ctx context.Context, topicID int) (*TorrentFile, error) {
	return runAuthed(ctx, s, func() (*TorrentFile, error) {
		return s.downloadTorrent(ctx, topicID)
	})
}

func (s *Scraper) downloadTorrent(ctx context.Context, topicID int) (*TorrentFile, error) {
	resp, err := s.getRaw(ctx, downloadPath, url.Values{"t": {strconv.Itoa(topicID)}})
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Wrapf(ErrDownloadFailed, "status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTorrentSize))
	if err != nil {
		return nil, errors.Wrap(err, "read torrent body")
	}

	// rutracker serves an HTML page (login or error) instead of a redirect when
	// the session is stale; a valid torrent always starts with a bencode dict.
	if len(data) == 0 || data[0] != bencodeDictPrefix {
		return nil, ErrNotAuthenticated
	}

	return &TorrentFile{
		Filename:  filenameFromResponse(resp, topicID),
		Content:   data,
		SizeBytes: len(data),
	}, nil
}

// Magnet resolves the magnet link for a topic by reading its topic page.
func (s *Scraper) Magnet(ctx context.Context, topicID int) (string, error) {
	return runAuthed(ctx, s, func() (string, error) {
		info, err := s.topicInfo(ctx, topicID)
		if err != nil {
			return "", err
		}

		if info.Magnet == "" {
			return "", errors.Wrapf(ErrNotFound, "no magnet link for topic %d", topicID)
		}

		return info.Magnet, nil
	})
}

// filenameFromResponse derives a download filename from the Content-Disposition
// header, falling back to "<topicID>.torrent".
func filenameFromResponse(resp *http.Response, topicID int) string {
	fallback := strconv.Itoa(topicID) + ".torrent"

	disposition := resp.Header.Get("Content-Disposition")
	if disposition == "" {
		return fallback
	}

	_, params, err := mime.ParseMediaType(disposition)
	if err != nil {
		return fallback
	}

	filename := params["filename"]
	if filename == "" {
		return fallback
	}

	return filename
}
