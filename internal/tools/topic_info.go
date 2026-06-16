package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
)

// TopicInfoParams defines the parameters for the rutracker_topic_info tool.
type TopicInfoParams struct {
	TopicID int `json:"topicId" jsonschema:"Topic ID (from a search result)"`
}

// TopicInfoTool returns the MCP tool definition for rutracker_topic_info.
func TopicInfoTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "rutracker_topic_info",
		Description: "Fetch detailed information about a torrent topic: title, exact size, seeders/leechers, info-hash, magnet link, and description",
		Annotations: readOnly("Topic Info"),
	}
}

// NewTopicInfoHandler creates a handler for the rutracker_topic_info tool.
func NewTopicInfoHandler(client rutracker.Client) mcp.ToolHandlerFor[TopicInfoParams, rutracker.TopicInfo] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		params TopicInfoParams,
	) (*mcp.CallToolResult, rutracker.TopicInfo, error) {
		return topicLookup(ctx, params.TopicID, "topic info failed", client.TopicInfo)
	}
}
