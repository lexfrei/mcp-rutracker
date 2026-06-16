package main

import (
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lexfrei/mcp-rutracker/internal/config"
	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
)

func TestRequireCredentials(t *testing.T) {
	t.Parallel()

	if err := requireCredentials(&config.Config{}); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("no credentials: expected ErrNoCredentials, got %v", err)
	}

	if err := requireCredentials(&config.Config{Cookie: "bb_session=x"}); err != nil {
		t.Errorf("cookie override: expected nil, got %v", err)
	}

	if err := requireCredentials(&config.Config{Username: "u", Password: "p"}); err != nil {
		t.Errorf("username+password: expected nil, got %v", err)
	}
}

func TestRegisterTools_ListsAllTools(t *testing.T) {
	t.Parallel()

	client, err := rutracker.New(&rutracker.Options{})
	if err != nil {
		t.Fatalf("rutracker.New: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: "test"}, newServerOptions())
	registerTools(server, client, "")

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer func() { _ = serverSession.Close() }()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)

	clientSession, err := mcpClient.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = clientSession.Close() }()

	result, err := clientSession.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	got := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		got[tool.Name] = true
	}

	want := []string{
		"rutracker_server_version",
		"rutracker_search",
		"rutracker_topic_info",
		"rutracker_files",
		"rutracker_magnet",
		"rutracker_download",
	}

	if len(got) != len(want) {
		t.Errorf("tool count = %d, want %d (%v)", len(got), len(want), result.Tools)
	}

	for _, name := range want {
		if !got[name] {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestNewServerOptions_HasInstructions(t *testing.T) {
	t.Parallel()

	opts := newServerOptions()
	if opts.Instructions == "" {
		t.Error("server instructions must not be empty")
	}
}
