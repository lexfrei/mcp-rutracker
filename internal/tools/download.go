package tools

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
	"github.com/lexfrei/mcp-rutracker/internal/torrentmeta"
)

// File permissions for a saved .torrent and its directory.
const (
	downloadDirPerm  = 0o755
	downloadFilePerm = 0o644
)

// DownloadParams defines the parameters for the rutracker_download tool.
type DownloadParams struct {
	TopicID    int   `json:"topicId"              jsonschema:"Topic ID (from a search result)"`
	SaveToDisk *bool `json:"saveToDisk,omitempty" jsonschema:"Also write the .torrent to the configured download directory"`
}

// DownloadResult is the output of the rutracker_download tool. The base64
// content is directly compatible with the transmission_torrent_add metainfo
// parameter of a sibling Transmission MCP server.
type DownloadResult struct {
	Filename       string `json:"filename"`
	ContentBase64  string `json:"contentBase64"`
	SizeBytes      int    `json:"sizeBytes"`
	InfoHash       string `json:"infoHash,omitempty"`
	FileCount      int    `json:"fileCount,omitempty"`
	TotalSizeBytes int64  `json:"totalSizeBytes,omitempty"`
	SavedPath      string `json:"savedPath,omitempty"`
}

// DownloadTool returns the MCP tool definition for rutracker_download.
func DownloadTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "rutracker_download",
		Description: "Download a topic's .torrent file as base64, enriched with the contained file list and info-hash; optionally save it to disk",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Download Torrent",
			DestructiveHint: ptrBool(false),
			OpenWorldHint:   ptrBool(true),
		},
	}
}

// NewDownloadHandler creates a handler for the rutracker_download tool. Saved
// files are written under downloadDir when saveToDisk is requested.
func NewDownloadHandler(client rutracker.Client, downloadDir string) mcp.ToolHandlerFor[DownloadParams, DownloadResult] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		params DownloadParams,
	) (*mcp.CallToolResult, DownloadResult, error) {
		if params.TopicID <= 0 {
			return &mcp.CallToolResult{IsError: true}, DownloadResult{}, validationErr(ErrTopicIDRequired)
		}

		file, err := client.DownloadTorrent(ctx, params.TopicID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, DownloadResult{}, rutrackerErr("download failed", err)
		}

		result := DownloadResult{
			Filename:      file.Filename,
			ContentBase64: base64.StdEncoding.EncodeToString(file.Content),
			SizeBytes:     file.SizeBytes,
		}

		enrichWithMeta(&result, file.Content)

		if deref(params.SaveToDisk) {
			saved, saveErr := saveTorrent(downloadDir, file)
			if saveErr != nil {
				return &mcp.CallToolResult{IsError: true}, DownloadResult{}, saveErr
			}

			result.SavedPath = saved
		}

		return nil, result, nil
	}
}

// enrichWithMeta decodes the torrent bytes and fills in the file count, total
// size, and info-hash. Parse failures are ignored: the raw download still works.
func enrichWithMeta(result *DownloadResult, content []byte) {
	meta, err := torrentmeta.Parse(content)
	if err != nil {
		return
	}

	result.InfoHash = meta.InfoHash
	result.FileCount = meta.FileCount
	result.TotalSizeBytes = meta.TotalSizeBytes
}

// saveTorrent writes the .torrent to downloadDir, sanitising the filename to a
// base name to prevent path traversal.
func saveTorrent(downloadDir string, file *rutracker.TorrentFile) (string, error) {
	if downloadDir == "" {
		return "", validationErr(ErrNoDownloadDir)
	}

	path := filepath.Join(downloadDir, filepath.Base(file.Filename))

	mkErr := os.MkdirAll(downloadDir, downloadDirPerm)
	if mkErr != nil {
		return "", rutrackerErr("create download directory", mkErr)
	}

	writeErr := os.WriteFile(path, file.Content, downloadFilePerm)
	if writeErr != nil {
		return "", rutrackerErr("write torrent file", writeErr)
	}

	return path, nil
}
