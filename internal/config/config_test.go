package config_test

import (
	"strings"
	"testing"

	"github.com/cockroachdb/errors"

	"github.com/lexfrei/mcp-rutracker/internal/config"
)

// clearEnv makes a config test hermetic by emptying every variable Load reads,
// so the developer's ambient shell environment cannot affect the result.
func clearEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"RUTRACKER_USERNAME", "RUTRACKER_PASSWORD", "RUTRACKER_COOKIE",
		"RUTRACKER_COOKIE_FILE", "RUTRACKER_BASE_URL", "RUTRACKER_USER_AGENT",
		"RUTRACKER_PROXY", "RUTRACKER_DOWNLOAD_DIR", "MCP_HTTP_PORT", "MCP_HTTP_HOST",
	} {
		t.Setenv(key, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.HTTPHost != "127.0.0.1" {
		t.Errorf("HTTPHost = %q, want 127.0.0.1", cfg.HTTPHost)
	}

	if cfg.HTTPEnabled() {
		t.Error("HTTPEnabled should be false without MCP_HTTP_PORT")
	}

	if !strings.HasSuffix(cfg.CookieFile, "/.mcp-rutracker/cookies.json") {
		t.Errorf("CookieFile = %q, want default under home", cfg.CookieFile)
	}
}

func TestLoad_CredentialsAndCookie(t *testing.T) {
	t.Setenv("RUTRACKER_USERNAME", "lexfrei")
	t.Setenv("RUTRACKER_PASSWORD", "secret")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.HasCredentials() {
		t.Error("HasCredentials should be true with username and password")
	}

	if cfg.Username != "lexfrei" || cfg.Password != "secret" {
		t.Errorf("credentials = %q/%q", cfg.Username, cfg.Password)
	}
}

func TestHasCredentials_CookieOnly(t *testing.T) {
	t.Setenv("RUTRACKER_COOKIE", "bb_session=abc")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.HasCredentials() {
		t.Error("HasCredentials should be true with a cookie override")
	}
}

func TestLoad_InvalidHTTPPort(t *testing.T) {
	t.Setenv("MCP_HTTP_PORT", "not-a-port")

	_, err := config.Load()
	if !errors.Is(err, config.ErrInvalidHTTPPort) {
		t.Fatalf("expected ErrInvalidHTTPPort, got %v", err)
	}
}

func TestLoad_HTTPAddr(t *testing.T) {
	t.Setenv("MCP_HTTP_PORT", "9090")
	t.Setenv("MCP_HTTP_HOST", "0.0.0.0")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.HTTPEnabled() {
		t.Error("HTTPEnabled should be true")
	}

	if cfg.HTTPAddr() != "0.0.0.0:9090" {
		t.Errorf("HTTPAddr = %q, want 0.0.0.0:9090", cfg.HTTPAddr())
	}
}

func TestLoad_InvalidProxy(t *testing.T) {
	t.Setenv("RUTRACKER_PROXY", "not-a-url")

	_, err := config.Load()
	if !errors.Is(err, config.ErrInvalidProxy) {
		t.Fatalf("expected ErrInvalidProxy, got %v", err)
	}
}

func TestProxyTransport(t *testing.T) {
	t.Setenv("RUTRACKER_PROXY", "socks5://127.0.0.1:1080")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	transport, err := cfg.ProxyTransport()
	if err != nil {
		t.Fatalf("ProxyTransport: %v", err)
	}

	if transport == nil {
		t.Fatal("expected a transport for a configured proxy")
	}
}

func TestProxyTransport_None(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	transport, err := cfg.ProxyTransport()
	if err != nil {
		t.Fatalf("ProxyTransport: %v", err)
	}

	if transport != nil {
		t.Error("expected nil transport without a proxy")
	}
}
