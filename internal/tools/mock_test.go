package tools_test

import (
	"context"

	"github.com/cockroachdb/errors"

	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
)

var errMock = errors.New("mock error")

// mockClient implements rutracker.Client for testing.
type mockClient struct {
	err error

	searchResults []rutracker.Torrent
	topicInfo     *rutracker.TopicInfo
	fileList      *rutracker.FileList
	torrentFile   *rutracker.TorrentFile
	magnet        string

	lastQuery   string
	lastOptions rutracker.SearchOptions
	lastTopicID int
}

func (m *mockClient) Search(
	_ context.Context,
	query string,
	opts rutracker.SearchOptions,
) ([]rutracker.Torrent, error) {
	m.lastQuery = query
	m.lastOptions = opts

	if m.err != nil {
		return nil, m.err
	}

	return m.searchResults, nil
}

func (m *mockClient) TopicInfo(_ context.Context, topicID int) (*rutracker.TopicInfo, error) {
	m.lastTopicID = topicID

	if m.err != nil {
		return nil, m.err
	}

	return m.topicInfo, nil
}

func (m *mockClient) Files(_ context.Context, topicID int) (*rutracker.FileList, error) {
	m.lastTopicID = topicID

	if m.err != nil {
		return nil, m.err
	}

	return m.fileList, nil
}

func (m *mockClient) DownloadTorrent(_ context.Context, topicID int) (*rutracker.TorrentFile, error) {
	m.lastTopicID = topicID

	if m.err != nil {
		return nil, m.err
	}

	return m.torrentFile, nil
}

func (m *mockClient) Magnet(_ context.Context, topicID int) (string, error) {
	m.lastTopicID = topicID

	if m.err != nil {
		return "", m.err
	}

	return m.magnet, nil
}
