package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
)

// MagnetParams defines the parameters for the rutracker_magnet tool.
type MagnetParams struct {
	TopicID int `json:"topicId" jsonschema:"Topic ID (from a search result)"`
}

// MagnetResult is the output of the rutracker_magnet tool.
type MagnetResult struct {
	Magnet   string `json:"magnet"`
	InfoHash string `json:"infoHash"`
}

// MagnetTool returns the MCP tool definition for rutracker_magnet.
func MagnetTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "rutracker_magnet",
		Description: "Resolve the magnet link and info-hash for a torrent topic",
		Annotations: readOnly("Get Magnet"),
	}
}

// NewMagnetHandler creates a handler for the rutracker_magnet tool.
func NewMagnetHandler(client rutracker.Client) mcp.ToolHandlerFor[MagnetParams, MagnetResult] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		params MagnetParams,
	) (*mcp.CallToolResult, MagnetResult, error) {
		if params.TopicID <= 0 {
			return &mcp.CallToolResult{IsError: true}, MagnetResult{}, validationErr(ErrTopicIDRequired)
		}

		magnet, err := client.Magnet(ctx, params.TopicID)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, MagnetResult{}, rutrackerErr("magnet failed", err)
		}

		return nil, MagnetResult{
			Magnet:   magnet,
			InfoHash: rutracker.MagnetInfoHash(magnet),
		}, nil
	}
}
