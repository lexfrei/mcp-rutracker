package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
)

// FilesParams defines the parameters for the rutracker_files tool.
type FilesParams struct {
	TopicID int `json:"topicId" jsonschema:"Topic ID (from a search result)"`
}

// FilesTool returns the MCP tool definition for rutracker_files.
func FilesTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "rutracker_files",
		Description: "List the files inside a torrent with exact per-file sizes, read from the topic page without downloading the .torrent",
		Annotations: readOnly("List Files"),
	}
}

// NewFilesHandler creates a handler for the rutracker_files tool.
func NewFilesHandler(client rutracker.Client) mcp.ToolHandlerFor[FilesParams, rutracker.FileList] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		params FilesParams,
	) (*mcp.CallToolResult, rutracker.FileList, error) {
		return topicLookup(ctx, params.TopicID, "file list failed", client.Files)
	}
}
