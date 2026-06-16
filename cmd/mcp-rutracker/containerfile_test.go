package main

import (
	"os"
	"strings"
	"testing"
)

// TestContainerfile_CookieFileEnv pins the container's RUTRACKER_COOKIE_FILE
// path so it cannot silently drift from the value documented in the README. The
// scratch image has no usable home directory, so the path must be set in the
// image rather than derived at runtime.
func TestContainerfile_CookieFileEnv(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../Containerfile")
	if err != nil {
		t.Fatalf("read Containerfile: %v", err)
	}

	const want = "ENV RUTRACKER_COOKIE_FILE=/home/nobody/.mcp-rutracker/cookies.json"
	if !strings.Contains(string(data), want) {
		t.Errorf("Containerfile must set %q (documented in README)", want)
	}
}
