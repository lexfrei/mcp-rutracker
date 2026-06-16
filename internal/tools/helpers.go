package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ptrBool returns a pointer to b, for the *bool annotation hint fields.
func ptrBool(value bool) *bool { return &value }

// topicLookup runs a topic-ID-keyed fetch that returns a pointer result,
// applying the validation, nil guard, and error mapping shared by the
// topic-oriented tools.
func topicLookup[R any](
	ctx context.Context,
	topicID int,
	failMessage string,
	fetch func(context.Context, int) (*R, error),
) (*mcp.CallToolResult, R, error) {
	var zero R

	if topicID <= 0 {
		return &mcp.CallToolResult{IsError: true}, zero, validationErr(ErrTopicIDRequired)
	}

	result, err := fetch(ctx, topicID)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, zero, rutrackerErr(failMessage, err)
	}

	if result == nil {
		return &mcp.CallToolResult{IsError: true}, zero, rutrackerErr(failMessage, ErrEmptyResponse)
	}

	return nil, *result, nil
}

// readOnly builds annotations for a tool that only reads remote state.
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:         title,
		ReadOnlyHint:  true,
		OpenWorldHint: ptrBool(true),
	}
}

// deref returns the pointed-to value, or the zero value when ptr is nil.
func deref[T any](ptr *T) T {
	if ptr != nil {
		return *ptr
	}

	var zero T

	return zero
}
