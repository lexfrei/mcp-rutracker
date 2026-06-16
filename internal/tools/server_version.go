package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerVersionToolName is the name of the version tool.
const ServerVersionToolName = "rutracker_server_version"

// ServerVersionParams has no parameters.
type ServerVersionParams struct{}

// ServerVersionResult reports build information.
type ServerVersionResult struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	GoVersion string `json:"goVersion"`
	Output    string `json:"output"`
}

// ServerVersionTool returns the MCP tool definition for the version tool.
func ServerVersionTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        ServerVersionToolName,
		Description: "Report the mcp-rutracker server version, git revision, and Go runtime version",
		Annotations: readOnly("Server Version"),
	}
}

// NewServerVersionHandler creates a handler reporting the given build info.
func NewServerVersionHandler(version, revision, goVersion string) mcp.ToolHandlerFor[ServerVersionParams, ServerVersionResult] {
	return func(
		_ context.Context,
		_ *mcp.CallToolRequest,
		_ ServerVersionParams,
	) (*mcp.CallToolResult, ServerVersionResult, error) {
		result := ServerVersionResult{
			Version:   version,
			Revision:  revision,
			GoVersion: goVersion,
			Output:    fmt.Sprintf("mcp-rutracker %s (%s), built with %s", version, revision, goVersion),
		}

		return nil, result, nil
	}
}
