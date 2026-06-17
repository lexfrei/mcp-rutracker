package config_test

import (
	"strings"
	"testing"
	"time"

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
		"RUTRACKER_PROXY", "RUTRACKER_ARTIFACT_BASE_URL", "RUTRACKER_ARTIFACT_TTL",
		"MCP_HTTP_PORT", "MCP_HTTP_HOST",
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

	if cfg.ArtifactTTL != 15*time.Minute {
		t.Errorf("ArtifactTTL = %s, want 15m", cfg.ArtifactTTL)
	}

	if cfg.ArtifactBaseURLOrDefault() != "" {
		t.Errorf("ArtifactBaseURLOrDefault = %q, want empty without HTTP", cfg.ArtifactBaseURLOrDefault())
	}
}

func TestLoad_ArtifactTTL(t *testing.T) {
	t.Setenv("RUTRACKER_ARTIFACT_TTL", "30s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ArtifactTTL != 30*time.Second {
		t.Errorf("ArtifactTTL = %s, want 30s", cfg.ArtifactTTL)
	}
}

func TestLoad_InvalidArtifactTTL(t *testing.T) {
	t.Setenv("RUTRACKER_ARTIFACT_TTL", "nonsense")

	_, err := config.Load()
	if !errors.Is(err, config.ErrInvalidArtifactTTL) {
		t.Fatalf("expected ErrInvalidArtifactTTL, got %v", err)
	}
}

func TestLoad_NonPositiveArtifactTTL(t *testing.T) {
	// A duration that parses but is zero or negative is a distinct failure from a
	// parse error: an expiry of "now or earlier" would make every artifact URL
	// dead on arrival, so it must be rejected too.
	for _, raw := range []string{"0s", "-5m"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("RUTRACKER_ARTIFACT_TTL", raw)

			_, err := config.Load()
			if !errors.Is(err, config.ErrInvalidArtifactTTL) {
				t.Fatalf("TTL %q: expected ErrInvalidArtifactTTL, got %v", raw, err)
			}
		})
	}
}

func TestArtifactBaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("MCP_HTTP_PORT", "9090")
	t.Setenv("MCP_HTTP_HOST", "0.0.0.0")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// A wildcard bind derives a loopback URL (fetchable), not an unusable 0.0.0.0.
	if got := cfg.ArtifactBaseURLOrDefault(); got != "http://127.0.0.1:9090" {
		t.Errorf("derived base = %q, want http://127.0.0.1:9090", got)
	}

	const explicit = "http://mcp.internal:9090"

	cfg.ArtifactBaseURL = explicit
	if got := cfg.ArtifactBaseURLOrDefault(); got != explicit {
		t.Errorf("explicit base = %q", got)
	}

	// A trailing slash must be stripped so URL joining never doubles it.
	cfg.ArtifactBaseURL = explicit + "/"
	if got := cfg.ArtifactBaseURLOrDefault(); got != explicit {
		t.Errorf("trailing-slash base = %q, want it stripped", got)
	}

	// An explicit IPv6 host derives a bracketed URL.
	cfg.ArtifactBaseURL = ""
	cfg.HTTPHost = "::1"
	if got := cfg.ArtifactBaseURLOrDefault(); got != "http://[::1]:9090" {
		t.Errorf("IPv6 derived base = %q, want http://[::1]:9090", got)
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
