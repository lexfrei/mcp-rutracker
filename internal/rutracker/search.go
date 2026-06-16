package rutracker

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// searchPath is the rutracker search endpoint, relative to the forum base.
const searchPath = "/forum/tracker.php"

// Search returns torrents matching query, applying opts and re-authenticating
// once if the session expired.
func (s *Scraper) Search(ctx context.Context, query string, opts SearchOptions) ([]Torrent, error) {
	return runAuthed(ctx, s, func() ([]Torrent, error) {
		return s.search(ctx, query, opts)
	})
}

func (s *Scraper) search(ctx context.Context, query string, opts SearchOptions) ([]Torrent, error) {
	values := url.Values{
		"nm": {query},
		"o":  {sortFieldCode(opts.Sort)},
		"s":  {sortOrderCode(opts.Order)},
	}

	if opts.ForumID > 0 {
		values.Set("f", strconv.Itoa(opts.ForumID))
	}

	doc, err := s.getDoc(ctx, searchPath, values)
	if err != nil {
		return nil, err
	}

	results := make([]Torrent, 0)

	doc.Find("#tor-tbl tbody tr").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		torrent, ok := s.parseSearchRow(sel)
		if ok {
			results = append(results, torrent)
		}

		return opts.Limit <= 0 || len(results) < opts.Limit
	})

	return results, nil
}

// parseSearchRow extracts a Torrent from a single result row. It returns
// ok=false for header or malformed rows lacking a topic link.
func (s *Scraper) parseSearchRow(sel *goquery.Selection) (Torrent, bool) {
	link := sel.Find("a.tLink").First()
	if link.Length() == 0 {
		return Torrent{}, false
	}

	topicID := atoiSafe(link.AttrOr("data-topic_id", ""))
	if topicID == 0 {
		topicID = topicIDFromHref(link.AttrOr("href", ""))
	}

	if topicID == 0 {
		return Torrent{}, false
	}

	forum := sel.Find("a.gen.f").First()
	sizeCell := sel.Find("td.tor-size").First()
	downloadsCell := sel.Find("td.number-format").First()

	torrent := Torrent{
		TopicID:   topicID,
		Title:     cleanText(link.Text()),
		ForumID:   forumIDFromHref(forum.AttrOr("href", "")),
		Forum:     cleanText(forum.Text()),
		SizeBytes: atoi64Safe(sizeCell.AttrOr("data-ts_text", "")),
		Seeders:   atoiSafe(sel.Find("b.seedmed").First().Text()),
		Leechers:  atoiSafe(sel.Find("td.leechmed").First().Text()),
		Downloads: atoiSafe(downloadsCell.AttrOr("data-ts_text", downloadsCell.Text())),
		Author:    cleanText(sel.Find("td.u-name-col a, div.u-name a").First().Text()),
		Added:     parseAddedDate(sel.Find("td").Last()),
		URL:       s.topicURL(topicID),
	}

	return torrent, true
}

// parseAddedDate reads a registration date from the trailing date cell, using
// its machine-readable data-ts_text unix timestamp when present.
func parseAddedDate(cell *goquery.Selection) time.Time {
	unix := atoi64Safe(cell.AttrOr("data-ts_text", ""))
	if unix > 0 {
		return time.Unix(unix, 0).UTC()
	}

	return time.Time{}
}

// topicURL builds the canonical viewtopic URL for a topic ID.
func (s *Scraper) topicURL(topicID int) string {
	return s.resolve("/forum/viewtopic.php", "t="+strconv.Itoa(topicID))
}

// sortFieldCode maps a SortField to rutracker's "o" (order-by) query value,
// defaulting to seeders.
func sortFieldCode(field SortField) string {
	switch field {
	case SortDate:
		return "1"
	case SortDownloads:
		return "4"
	case SortSize:
		return "7"
	case SortSeeders:
		return "10"
	default:
		return "10"
	}
}

// sortOrderCode maps a SortOrder to rutracker's "s" (sort-direction) value,
// defaulting to descending.
func sortOrderCode(order SortOrder) string {
	if order == OrderAsc {
		return "1"
	}

	return "2"
}
