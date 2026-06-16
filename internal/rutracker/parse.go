package rutracker

import (
	"net/url"
	"strconv"
	"strings"
)

// cleanText normalises scraped text: non-breaking spaces become regular spaces
// and runs of whitespace collapse to one.
func cleanText(text string) string {
	text = strings.ReplaceAll(text, "\u00a0", " ")

	return strings.Join(strings.Fields(text), " ")
}

// atoiSafe parses an int from possibly noisy text, returning 0 on failure.
func atoiSafe(text string) int {
	digits := keepDigits(text)
	if digits == "" {
		return 0
	}

	value, err := strconv.Atoi(digits)
	if err != nil {
		return 0
	}

	return value
}

// atoi64Safe parses an int64 from possibly noisy text, returning 0 on failure.
func atoi64Safe(text string) int64 {
	digits := keepDigits(text)
	if digits == "" {
		return 0
	}

	value, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0
	}

	return value
}

// keepDigits strips everything but ASCII digits from text.
func keepDigits(text string) string {
	var buf strings.Builder

	for _, r := range text {
		if r >= '0' && r <= '9' {
			buf.WriteRune(r)
		}
	}

	return buf.String()
}

// topicIDFromHref extracts the t=<id> topic identifier from a rutracker href.
func topicIDFromHref(href string) int {
	parsed, err := url.Parse(href)
	if err != nil {
		return 0
	}

	return atoiSafe(parsed.Query().Get("t"))
}

// forumIDFromHref extracts the f=<id> forum identifier from a rutracker href.
func forumIDFromHref(href string) int {
	parsed, err := url.Parse(href)
	if err != nil {
		return 0
	}

	return atoiSafe(parsed.Query().Get("f"))
}

// MagnetInfoHash returns the upper-case BitTorrent info-hash inside a magnet
// URI's xt=urn:btih:<hash> parameter, or "" when absent.
func MagnetInfoHash(magnet string) string {
	parsed, err := url.Parse(magnet)
	if err != nil {
		return ""
	}

	for _, xt := range parsed.Query()["xt"] {
		_, hash, found := strings.Cut(xt, "urn:btih:")
		if found && hash != "" {
			return strings.ToUpper(hash)
		}
	}

	return ""
}

// magnetExactLength returns the xl (exact length, in bytes) parameter of a
// magnet URI, or 0 when absent.
func magnetExactLength(magnet string) int64 {
	parsed, err := url.Parse(magnet)
	if err != nil {
		return 0
	}

	return atoi64Safe(parsed.Query().Get("xl"))
}
