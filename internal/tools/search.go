package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
)

// SearchParams defines the parameters for the rutracker_search tool.
type SearchParams struct {
	Query   string `json:"query"             jsonschema:"Search query (title keywords)"`
	ForumID int    `json:"forumId,omitempty" jsonschema:"Restrict to a forum/category ID (0 = all)"`
	Sort    string `json:"sort,omitempty"    jsonschema:"Sort field: seeders (default), size, date, downloads"`
	Order   string `json:"order,omitempty"   jsonschema:"Sort order: desc (default) or asc"`
	Limit   int    `json:"limit,omitempty"   jsonschema:"Maximum number of results to return"`
}

// SearchResult is the output of the rutracker_search tool.
type SearchResult struct {
	Count   int                 `json:"count"`
	Results []rutracker.Torrent `json:"results"`
}

// SearchTool returns the MCP tool definition for rutracker_search.
func SearchTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "rutracker_search",
		Description: "Search rutracker torrents by keywords, optionally restricted to a forum and sorted by seeders, size, date, or downloads",
		Annotations: readOnly("Search Torrents"),
	}
}

// NewSearchHandler creates a handler for the rutracker_search tool.
func NewSearchHandler(client rutracker.Client) mcp.ToolHandlerFor[SearchParams, SearchResult] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		params SearchParams,
	) (*mcp.CallToolResult, SearchResult, error) {
		opts, vErr := searchOptions(&params)
		if vErr != nil {
			return &mcp.CallToolResult{IsError: true}, SearchResult{}, validationErr(vErr)
		}

		results, err := client.Search(ctx, params.Query, opts)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, SearchResult{}, rutrackerErr("search failed", err)
		}

		return nil, SearchResult{Count: len(results), Results: results}, nil
	}
}

// searchOptions validates parameters and maps them to rutracker.SearchOptions.
func searchOptions(params *SearchParams) (rutracker.SearchOptions, error) {
	if params.Query == "" {
		return rutracker.SearchOptions{}, ErrQueryRequired
	}

	sort, err := parseSort(params.Sort)
	if err != nil {
		return rutracker.SearchOptions{}, err
	}

	order, err := parseOrder(params.Order)
	if err != nil {
		return rutracker.SearchOptions{}, err
	}

	return rutracker.SearchOptions{
		ForumID: params.ForumID,
		Sort:    sort,
		Order:   order,
		Limit:   params.Limit,
	}, nil
}

func parseSort(value string) (rutracker.SortField, error) {
	switch value {
	case "":
		return "", nil
	case string(rutracker.SortSeeders), string(rutracker.SortSize),
		string(rutracker.SortDate), string(rutracker.SortDownloads):
		return rutracker.SortField(value), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidSort, value)
	}
}

func parseOrder(value string) (rutracker.SortOrder, error) {
	switch value {
	case "":
		return "", nil
	case string(rutracker.OrderAsc), string(rutracker.OrderDesc):
		return rutracker.SortOrder(value), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidOrder, value)
	}
}
