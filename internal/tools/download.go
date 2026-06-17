package tools

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lexfrei/mcp-rutracker/internal/artifact"
	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
	"github.com/lexfrei/mcp-rutracker/internal/torrentmeta"
)

// Download delivery modes.
const (
	modeMetadata = "metadata"
	modeBase64   = "base64"
	modeArtifact = "artifact"
)

// DownloadParams defines the parameters for the rutracker_download tool.
type DownloadParams struct {
	TopicID int    `json:"topicId"        jsonschema:"Topic ID (from a search result)"`
	Mode    string `json:"mode,omitempty" jsonschema:"How to deliver the .torrent: 'metadata' (info only), 'base64' (inline content for piping to a torrent client), or 'artifact' (a one-time download URL; requires the HTTP transport). Default: artifact when HTTP is enabled, otherwise metadata."`
}

// DownloadResult is the output of the rutracker_download tool. Metadata fields
// are always present; the content is delivered inline (base64) or via a
// one-time download URL (artifact) depending on the mode.
type DownloadResult struct {
	Filename       string    `json:"filename"`
	SizeBytes      int       `json:"sizeBytes"`
	SHA256         string    `json:"sha256"`
	InfoHash       string    `json:"infoHash,omitempty"`
	FileCount      int       `json:"fileCount,omitempty"`
	TotalSizeBytes int64     `json:"totalSizeBytes,omitempty"`
	ContentBase64  string    `json:"contentBase64,omitempty"`
	ArtifactID     string    `json:"artifactId,omitempty"`
	DownloadURL    string    `json:"downloadUrl,omitempty"`
	ExpiresAt      time.Time `json:"expiresAt,omitzero"`
}

// DownloadTool returns the MCP tool definition for rutracker_download.
func DownloadTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "rutracker_download",
		Description: "Fetch a topic's .torrent, enriched with its file list and info-hash. Returns a one-time download URL (HTTP mode) or metadata (stdio) by default; set mode=base64 for inline content",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Download Torrent",
			DestructiveHint: ptrBool(false),
			OpenWorldHint:   ptrBool(true),
		},
	}
}

// NewDownloadHandler creates a handler for the rutracker_download tool. In
// artifact mode the .torrent is stored and served once from artifactBaseURL.
func NewDownloadHandler(
	client rutracker.Client,
	store *artifact.Store,
	artifactBaseURL string,
	httpEnabled bool,
) mcp.ToolHandlerFor[DownloadParams, DownloadResult] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		params DownloadParams,
	) (*mcp.CallToolResult, DownloadResult, error) {
		if params.TopicID <= 0 {
			return &mcp.CallToolResult{IsError: true}, DownloadResult{}, validationErr(ErrTopicIDRequired)
		}

		mode, modeErr := resolveMode(params.Mode, httpEnabled)
		if modeErr != nil {
			return &mcp.CallToolResult{IsError: true}, DownloadResult{}, validationErr(modeErr)
		}

		if mode == modeArtifact && !httpEnabled {
			return &mcp.CallToolResult{IsError: true}, DownloadResult{}, validationErr(ErrArtifactUnavailable)
		}

		file, err := client.DownloadTorrent(ctx, params.TopicID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, DownloadResult{}, rutrackerErr("download failed", err)
		}

		sum := sha256.Sum256(file.Content)
		result := DownloadResult{
			Filename:  file.Filename,
			SizeBytes: file.SizeBytes,
			SHA256:    hex.EncodeToString(sum[:]),
		}

		enrichWithMeta(&result, file.Content)

		deliverErr := deliver(&result, mode, store, artifactBaseURL, file)
		if deliverErr != nil {
			return &mcp.CallToolResult{IsError: true}, DownloadResult{}, deliverErr
		}

		return nil, result, nil
	}
}

// resolveMode validates the requested mode and applies the adaptive default:
// artifact when the HTTP transport is enabled, otherwise metadata.
func resolveMode(raw string, httpEnabled bool) (string, error) {
	switch raw {
	case "":
		if httpEnabled {
			return modeArtifact, nil
		}

		return modeMetadata, nil
	case modeMetadata, modeBase64, modeArtifact:
		return raw, nil
	default:
		return "", errors.Wrapf(ErrInvalidMode, "%q", raw)
	}
}

// deliver fills the mode-specific output fields (inline base64 or a one-time
// artifact URL); metadata mode adds nothing beyond the common fields.
func deliver(
	result *DownloadResult,
	mode string,
	store *artifact.Store,
	artifactBaseURL string,
	file *rutracker.TorrentFile,
) error {
	switch mode {
	case modeBase64:
		result.ContentBase64 = base64.StdEncoding.EncodeToString(file.Content)
	case modeArtifact:
		art, putErr := store.Put(file.Filename, file.Content)
		if putErr != nil {
			return rutrackerErr("store artifact", putErr)
		}

		result.ArtifactID = art.Token
		result.DownloadURL = artifactBaseURL + "/artifacts/" + art.Token
		result.ExpiresAt = art.ExpiresAt
	case modeMetadata:
	default:
		// Unreachable: resolveMode validates the mode first. The arm makes the
		// invariant self-defending if a new mode is ever added.
		return rutrackerErr("deliver", errors.Wrapf(ErrInvalidMode, "%q", mode))
	}

	return nil
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
