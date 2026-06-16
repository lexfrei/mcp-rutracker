package rutracker

import (
	"context"
	"net/url"
	"strconv"

	"github.com/PuerkitoBio/goquery"
	"github.com/cockroachdb/errors"
)

// topicPath is the rutracker topic-view endpoint, relative to the forum base.
const topicPath = "/forum/viewtopic.php"

// TopicInfo returns the detailed view of a topic, re-authenticating once if the
// session expired.
func (s *Scraper) TopicInfo(ctx context.Context, topicID int) (*TopicInfo, error) {
	return runAuthed(ctx, s, func() (*TopicInfo, error) {
		return s.topicInfo(ctx, topicID)
	})
}

func (s *Scraper) topicInfo(ctx context.Context, topicID int) (*TopicInfo, error) {
	doc, err := s.getDoc(ctx, topicPath, url.Values{"t": {strconv.Itoa(topicID)}})
	if err != nil {
		return nil, err
	}

	title := cleanText(doc.Find("a#topic-title, h1.maintitle").First().Text())
	if title == "" {
		// A 200 response without a topic title is not necessarily a deleted
		// topic: it is also what an anti-bot interstitial returns. Report a
		// parse failure rather than a misleading "not found".
		return nil, errors.Wrap(ErrParse, "topic page has no title")
	}

	info := &TopicInfo{
		TopicID:     topicID,
		Title:       title,
		Forum:       cleanText(doc.Find(".nav.pad_8 a, .topic-nav a").Last().Text()),
		Seeders:     atoiSafe(doc.Find("span.seed b, .seedmed").First().Text()),
		Leechers:    atoiSafe(doc.Find("span.leech b, .leechmed").First().Text()),
		Downloads:   atoiSafe(doc.Find("p.dl_links_count, span.dl-count").First().Text()),
		Description: cleanText(doc.Find("div.post_body").First().Text()),
		URL:         s.topicURL(topicID),
	}

	info.Magnet = extractMagnet(doc)
	info.InfoHash = magnetInfoHash(info.Magnet)
	info.SizeBytes = extractSize(doc, info.Magnet)

	return info, nil
}

// extractMagnet returns the magnet URI from the dedicated anchor, falling back
// to any anchor whose href is a magnet link.
func extractMagnet(doc *goquery.Document) string {
	magnet, ok := doc.Find("a.magnet-link").First().Attr("href")
	if ok && magnet != "" {
		return magnet
	}

	magnet, ok = doc.Find("a[href^='magnet:']").First().Attr("href")
	if ok {
		return magnet
	}

	return ""
}

// extractSize resolves the torrent size in bytes. rutracker exposes the exact
// byte count in the title attribute of the size span (and sometimes via the
// magnet's xl parameter), so both are preferred over the human-readable text.
func extractSize(doc *goquery.Document, magnet string) int64 {
	if size := magnetExactLength(magnet); size > 0 {
		return size
	}

	span := doc.Find("#tor-size-humn, span.size-humn").First()

	for _, attr := range []string{"title", "data-ts_text"} {
		if bytes := atoi64Safe(span.AttrOr(attr, "")); bytes > 0 {
			return bytes
		}
	}

	return 0
}
