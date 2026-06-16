package rutracker

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/cockroachdb/errors"
)

// fileListPath is rutracker's AJAX file-list endpoint, relative to the base.
const fileListPath = "/forum/viewtorrent.php"

// FileEntry is one file inside a torrent, as listed on the topic page.
type FileEntry struct {
	// Path is the slash-joined path within the torrent.
	Path string `json:"path"`
	// SizeBytes is the exact file size in bytes.
	SizeBytes int64 `json:"sizeBytes"`
}

// FileList is the file tree of a torrent obtained from the topic page without
// downloading the .torrent file itself.
type FileList struct {
	TopicID        int         `json:"topicId"`
	FileCount      int         `json:"fileCount"`
	TotalSizeBytes int64       `json:"totalSizeBytes"`
	Files          []FileEntry `json:"files"`
}

// Files returns a topic's file list from rutracker's lightweight viewtorrent.php
// endpoint. Unlike DownloadTorrent it does not fetch the .torrent, so it is
// cheap, yet rutracker still reports exact per-file byte sizes.
func (s *Scraper) Files(ctx context.Context, topicID int) (*FileList, error) {
	return runAuthed(ctx, s, func() (*FileList, error) {
		return s.files(ctx, topicID)
	})
}

func (s *Scraper) files(ctx context.Context, topicID int) (*FileList, error) {
	doc, err := s.postFileListForm(ctx, topicID)
	if err != nil {
		return nil, err
	}

	root := doc.Find("ul.ftree").First()
	if root.Length() == 0 {
		// The AJAX endpoint returns the tree only when authenticated; a missing
		// tree means the session lapsed, so trigger a re-login and retry.
		return nil, ErrNotAuthenticated
	}

	files := make([]FileEntry, 0)
	collectFileTree(root, "", &files)

	if len(files) == 0 {
		// The tree element is present but lists nothing: this is a genuine
		// "no files" result, not an auth problem, so do not trigger a re-login.
		return nil, ErrNotFound
	}

	list := &FileList{TopicID: topicID, FileCount: len(files), Files: files}
	for _, file := range files {
		list.TotalSizeBytes += file.SizeBytes
	}

	return list, nil
}

// collectFileTree walks a rutracker file tree. Directory nodes are <li> with a
// nested <ul>; file nodes carry their exact size in an <i> element.
func collectFileTree(list *goquery.Selection, prefix string, out *[]FileEntry) {
	list.ChildrenFiltered("li").Each(func(_ int, item *goquery.Selection) {
		div := item.ChildrenFiltered("div")
		name := cleanText(div.ChildrenFiltered("b").First().Text())
		nested := item.ChildrenFiltered("ul").First()

		if nested.Length() > 0 {
			collectFileTree(nested, joinFilePath(prefix, name), out)

			return
		}

		*out = append(*out, FileEntry{
			Path:      strings.TrimPrefix(joinFilePath(prefix, name), "./"),
			SizeBytes: atoi64Safe(div.ChildrenFiltered("i").First().Text()),
		})
	})
}

// joinFilePath joins a directory prefix and a node name with a forward slash.
func joinFilePath(prefix, name string) string {
	if prefix == "" {
		return name
	}

	return prefix + "/" + name
}

// postFileListForm posts to viewtorrent.php and returns the parsed fragment.
// The response is UTF-8 (unlike the windows-1251 HTML pages), so it is parsed
// without transcoding.
func (s *Scraper) postFileListForm(ctx context.Context, topicID int) (*goquery.Document, error) {
	idStr := strconv.Itoa(topicID)

	body, err := encodeValues(url.Values{"t": {idStr}})
	if err != nil {
		return nil, err
	}

	req, err := s.newRequest(ctx, http.MethodPost, s.resolve(fileListPath, ""), strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", s.resolve("/forum/viewtopic.php", "t="+idStr))

	resp, err := s.doRequest(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if isLoginRedirect(resp) {
		return nil, ErrNotAuthenticated
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "parse file list")
	}

	return doc, nil
}
