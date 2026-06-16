// Package rutracker provides an HTTP client and scraper for rutracker.org.
//
// The package logs in with a username/password (or a pre-obtained session
// cookie), persists the session to disk for reuse, and exposes typed helpers
// for searching, inspecting topics, downloading .torrent files, and resolving
// magnet links. All HTML responses are decoded from windows-1251 to UTF-8.
package rutracker

import "time"

// SortField selects the column rutracker sorts search results by.
type SortField string

const (
	// SortSeeders sorts by the number of seeders.
	SortSeeders SortField = "seeders"
	// SortSize sorts by torrent size.
	SortSize SortField = "size"
	// SortDate sorts by the registration date.
	SortDate SortField = "date"
	// SortDownloads sorts by the number of completed downloads.
	SortDownloads SortField = "downloads"
)

// SortOrder selects ascending or descending result ordering.
type SortOrder string

const (
	// OrderDesc sorts results in descending order (the rutracker default).
	OrderDesc SortOrder = "desc"
	// OrderAsc sorts results in ascending order.
	OrderAsc SortOrder = "asc"
)

// SearchOptions tunes a search request. The zero value is valid and yields
// results sorted by seeders descending across all forums.
type SearchOptions struct {
	// ForumID restricts the search to a single forum/category (0 = all).
	ForumID int
	// Sort selects the sort column (empty = SortSeeders).
	Sort SortField
	// Order selects ascending/descending (empty = OrderDesc).
	Order SortOrder
	// Limit caps the number of returned results (0 = no client-side cap).
	Limit int
}

// Torrent is a single row from a search-result listing.
type Torrent struct {
	TopicID   int       `json:"topicId"`
	Title     string    `json:"title"`
	ForumID   int       `json:"forumId"`
	Forum     string    `json:"forum"`
	SizeBytes int64     `json:"sizeBytes"`
	Seeders   int       `json:"seeders"`
	Leechers  int       `json:"leechers"`
	Downloads int       `json:"downloads"`
	Author    string    `json:"author,omitempty"`
	Added     time.Time `json:"added,omitzero"`
	URL       string    `json:"url"`
}

// TopicInfo is the detailed view of a single torrent topic.
type TopicInfo struct {
	TopicID int    `json:"topicId"`
	Title   string `json:"title"`
	Forum   string `json:"forum,omitempty"`
	// SizeBytes is the total size in bytes; 0 means the size could not be
	// determined from the page, not a genuinely empty torrent.
	SizeBytes   int64  `json:"sizeBytes"`
	Seeders     int    `json:"seeders"`
	Leechers    int    `json:"leechers"`
	Downloads   int    `json:"downloads"`
	InfoHash    string `json:"infoHash,omitempty"`
	Magnet      string `json:"magnet,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
}

// TorrentFile is a downloaded .torrent payload.
type TorrentFile struct {
	Filename  string
	Content   []byte
	SizeBytes int
}
