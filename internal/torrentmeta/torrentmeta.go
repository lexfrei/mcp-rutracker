// Package torrentmeta decodes a bencoded .torrent file into structured
// metadata: the contained file list with exact sizes, the piece length, the
// total size, and the canonical BitTorrent v1 info-hash.
//
// It ships a small, dependency-free bencode decoder sufficient for reading
// torrent metainfo; it is not a general-purpose bencode library.
package torrentmeta

import (
	"crypto/sha1" //nolint:gosec // BitTorrent v1 info-hash is defined as SHA-1; this is a protocol constant, not a security choice.
	"encoding/hex"
	"strings"

	"github.com/cockroachdb/errors"
)

// ErrInvalidTorrent indicates the bytes are not a well-formed .torrent file.
var ErrInvalidTorrent = errors.New("invalid .torrent data")

// FileEntry is a single file inside a torrent.
type FileEntry struct {
	// Path is the slash-joined path of the file within the torrent.
	Path string `json:"path"`
	// SizeBytes is the exact file size in bytes.
	SizeBytes int64 `json:"sizeBytes"`
}

// Meta is the decoded metadata of a .torrent file.
type Meta struct {
	Name           string      `json:"name"`
	InfoHash       string      `json:"infoHash"`
	TotalSizeBytes int64       `json:"totalSizeBytes"`
	PieceLength    int64       `json:"pieceLength"`
	FileCount      int         `json:"fileCount"`
	Files          []FileEntry `json:"files"`
}

// Parse decodes a .torrent file into Meta.
func Parse(data []byte) (*Meta, error) {
	dec := &decoder{data: data}

	root, err := dec.decode()
	if err != nil {
		return nil, err
	}

	if dec.pos != len(data) {
		return nil, errors.Wrap(ErrInvalidTorrent, "trailing data after root value")
	}

	top, ok := asDict(root)
	if !ok {
		return nil, errors.Wrap(ErrInvalidTorrent, "top-level value is not a dictionary")
	}

	info, ok := asDict(top["info"])
	if !ok {
		return nil, errors.Wrap(ErrInvalidTorrent, "missing info dictionary")
	}

	if dec.infoStart >= dec.infoEnd {
		return nil, errors.Wrap(ErrInvalidTorrent, "could not locate info dictionary bytes")
	}

	meta := &Meta{
		InfoHash:    infoHash(data[dec.infoStart:dec.infoEnd]),
		PieceLength: dictInt(info, "piece length"),
	}

	name, _ := asString(info["name"])
	if name == "" {
		return nil, errors.Wrap(ErrInvalidTorrent, "info dictionary has no name")
	}

	meta.Name = name
	meta.Files = extractFiles(info, name)
	meta.FileCount = len(meta.Files)

	// A well-formed torrent always lists at least one file; an empty result
	// means a missing length or an empty/garbled files list.
	if meta.FileCount == 0 {
		return nil, errors.Wrap(ErrInvalidTorrent, "info dictionary lists no files")
	}

	for _, file := range meta.Files {
		meta.TotalSizeBytes += file.SizeBytes
	}

	return meta, nil
}

// extractFiles builds the file list from either the single-file "length" key or
// the multi-file "files" list, prefixing multi-file paths with the torrent name.
// Malformed entries (non-dict, or missing path) are skipped rather than failing
// the whole parse; if that leaves zero files, Parse rejects the torrent.
func extractFiles(info map[string]any, name string) []FileEntry {
	rawFiles, ok := asList(info["files"])
	if !ok {
		// Single-file torrent: name is the file, length is its size.
		return []FileEntry{{Path: name, SizeBytes: dictInt(info, "length")}}
	}

	files := make([]FileEntry, 0, len(rawFiles))

	for _, raw := range rawFiles {
		entry, ok := asDict(raw)
		if !ok {
			continue
		}

		segments, ok := asList(entry["path"])
		if !ok {
			continue
		}

		parts := make([]string, 0, len(segments)+1)
		if name != "" {
			parts = append(parts, name)
		}

		for _, segment := range segments {
			part, ok := asString(segment)
			if ok {
				parts = append(parts, part)
			}
		}

		files = append(files, FileEntry{
			Path:      strings.Join(parts, "/"),
			SizeBytes: dictInt(entry, "length"),
		})
	}

	return files
}

// infoHash returns the upper-case hex SHA-1 of the raw info dictionary bytes.
func infoHash(infoBytes []byte) string {
	sum := sha1.Sum(infoBytes) //nolint:gosec // BitTorrent v1 info-hash is SHA-1 by definition.

	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// dictInt reads an integer value from a bencode dictionary, returning 0 when the
// key is absent or not an integer.
func dictInt(dict map[string]any, key string) int64 {
	value, _ := asInt(dict[key])

	return value
}

func asDict(value any) (map[string]any, bool) {
	dict, ok := value.(map[string]any)

	return dict, ok
}

func asList(value any) ([]any, bool) {
	list, ok := value.([]any)

	return list, ok
}

func asString(value any) (string, bool) {
	str, ok := value.(string)

	return str, ok
}

func asInt(value any) (int64, bool) {
	num, ok := value.(int64)

	return num, ok
}
