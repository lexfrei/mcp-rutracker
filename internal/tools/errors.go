// Package tools provides MCP tool definitions and handlers for rutracker.
package tools

import "github.com/cockroachdb/errors"

// ErrValidation indicates invalid parameters provided by the caller.
var ErrValidation = errors.New("validation error")

// ErrQueryRequired is returned when a search query is empty.
var ErrQueryRequired = errors.New("query is required")

// ErrTopicIDRequired is returned when a topic ID is missing or non-positive.
var ErrTopicIDRequired = errors.New("topicId must be a positive integer")

// ErrInvalidSort is returned when an unknown sort field is requested.
var ErrInvalidSort = errors.New("sort must be one of: seeders, size, date, downloads")

// ErrInvalidOrder is returned when an unknown sort order is requested.
var ErrInvalidOrder = errors.New("order must be one of: asc, desc")

// ErrRutracker indicates a failure talking to rutracker.
var ErrRutracker = errors.New("rutracker request error")

// ErrEmptyResponse indicates the client returned neither data nor an error,
// which a well-behaved client never does but the interface does not forbid.
var ErrEmptyResponse = errors.New("rutracker returned no data")

// ErrInvalidMode is returned when an unknown download mode is requested.
var ErrInvalidMode = errors.New("mode must be one of: metadata, base64, artifact")

// ErrArtifactUnavailable is returned when the artifact mode is requested but the
// HTTP transport that serves download URLs is not enabled.
var ErrArtifactUnavailable = errors.New("artifact mode requires the HTTP transport (set MCP_HTTP_PORT)")

// validationErr marks an error as a validation error.
func validationErr(err error) error {
	//nolint:wrapcheck // Mark adds a sentinel category; the caller supplies the message.
	return errors.Mark(err, ErrValidation)
}

// rutrackerErr wraps a message and underlying error as a rutracker error.
func rutrackerErr(msg string, err error) error {
	//nolint:wrapcheck // Mark adds a sentinel category on top of Wrap which adds context.
	return errors.Mark(errors.Wrap(err, msg), ErrRutracker)
}
