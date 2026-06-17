// Command mcp-rutracker is an MCP server exposing rutracker.org search and
// download tools over stdio and, optionally, HTTP.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"

	"github.com/lexfrei/mcp-rutracker/internal/artifact"
	"github.com/lexfrei/mcp-rutracker/internal/config"
	"github.com/lexfrei/mcp-rutracker/internal/rutracker"
	"github.com/lexfrei/mcp-rutracker/internal/tools"
)

const (
	serverName        = "mcp-rutracker"
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
)

// version and revision are set via ldflags at build time.
var (
	version  = "dev"
	revision = "unknown"
)

// ErrNoCredentials indicates the server was started without any way to
// authenticate, so it would fail on every request.
var ErrNoCredentials = errors.New(
	"no credentials: set RUTRACKER_USERNAME/RUTRACKER_PASSWORD or RUTRACKER_COOKIE")

func main() {
	logger := newLogger()

	err := run(logger)
	if err != nil {
		logger.Error("server failed", slog.Any("error", err))
		os.Exit(1)
	}
}

// newLogger builds the structured JSON logger. Logs go to stderr because stdout
// carries the JSON-RPC stream.
func newLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

// requireCredentials fails fast at startup when nothing can authenticate, so the
// operator learns at launch rather than on the first query.
func requireCredentials(cfg *config.Config) error {
	if !cfg.HasCredentials() {
		return ErrNoCredentials
	}

	return nil
}

func run(logger *slog.Logger) error {
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		return errors.Wrap(cfgErr, "invalid configuration")
	}

	credErr := requireCredentials(cfg)
	if credErr != nil {
		return credErr
	}

	transport, transportErr := cfg.ProxyTransport()
	if transportErr != nil {
		return errors.Wrap(transportErr, "invalid proxy configuration")
	}

	client, clientErr := rutracker.New(&rutracker.Options{
		BaseURL:    cfg.BaseURL,
		Username:   cfg.Username,
		Password:   cfg.Password,
		Cookie:     cfg.Cookie,
		CookiePath: cfg.CookieFile,
		UserAgent:  cfg.UserAgent,
		Transport:  transport,
	})
	if clientErr != nil {
		return errors.Wrap(clientErr, "failed to create rutracker client")
	}

	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    serverName,
			Version: version + "+" + revision,
		},
		newServerOptions(logger),
	)

	store := artifact.NewStore(cfg.ArtifactTTL)
	registerTools(server, client, store, cfg)

	return serve(logger, server, store, cfg)
}

// artifactHandler serves a stored .torrent once from its capability token.
func artifactHandler(logger *slog.Logger, store *artifact.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("id")

		// Take consumes the token now, so a URL is one-time per fetch attempt:
		// a failed transfer (dropped client) also burns it, and the caller must
		// request a fresh download. This keeps the capability strictly single-use
		// with no replay window.
		art, ok := store.Take(token)
		if !ok {
			http.NotFound(w, r)

			return
		}

		name := filepath.Base(art.Filename)

		// Content-Length catches a short read; X-Content-Sha256 carries the
		// content digest (identical to the download tool's sha256) so the fetcher
		// can detect a corrupted or truncated transfer. Both attest the bytes the
		// server holds, not the upstream torrent's correctness — a
		// corrupt-but-complete .torrent hashes consistently.
		w.Header().Set("Content-Type", "application/x-bittorrent")
		w.Header().Set("Content-Length", strconv.Itoa(art.Size))

		sum := sha256.Sum256(art.Content)
		w.Header().Set("X-Content-Sha256", hex.EncodeToString(sum[:]))
		// An ASCII fallback for old clients plus an RFC 5987 filename* that
		// preserves the real (often Cyrillic) name. Both are injection-safe:
		// safeFilename strips to a safe charset, rfc5987Value percent-encodes.
		w.Header().Set("Content-Disposition",
			`attachment; filename="`+safeFilename(name)+`"; filename*=UTF-8''`+rfc5987Value(name))

		_, writeErr := w.Write(art.Content)
		if writeErr != nil {
			logger.Error("artifact write failed",
				slog.String("artifact_ref", tokenRef(token)), slog.Any("error", writeErr))
		}
	}
}

// fallbackFilename is used when a server-controlled name sanitizes to nothing
// usable (empty, ".", "/", or all separators), so the Content-Disposition never
// degenerates to filename="." or filename="_".
const fallbackFilename = "download.torrent"

// safeFilename reduces a server-controlled filename to its base name over a safe
// charset, so it can never inject headers or carry path components. A name that
// reduces to no alphanumeric character falls back to a fixed safe name.
func safeFilename(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_', r == ' ':
			return r
		default:
			return '_'
		}
	}, filepath.Base(name))

	hasAlnum := strings.ContainsFunc(cleaned, func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
	})
	if !hasAlnum {
		return fallbackFilename
	}

	return cleaned
}

// tokenRef derives a short, non-reversible reference to a capability token so a
// log line can identify an artifact without writing the bearer credential into
// logs.
func tokenRef(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])[:12]
}

// rfc5987Value percent-encodes a filename for an RFC 5987 filename* parameter,
// keeping only attr-chars verbatim. The result can never contain CR, LF, or a
// quote, so it is safe to embed in a header value.
func rfc5987Value(name string) string {
	const attrChars = "!#$&+-.^_`|~"

	var buf strings.Builder

	for _, b := range []byte(name) {
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9',
			strings.IndexByte(attrChars, b) >= 0:
			buf.WriteByte(b)
		default:
			buf.WriteByte('%')
			buf.WriteString(strings.ToUpper(hex.EncodeToString([]byte{b})))
		}
	}

	return buf.String()
}

// newServerOptions wires the shared logger into the MCP server so its internal
// logs share the structured JSON format used by the rest of the binary.
func newServerOptions(logger *slog.Logger) *mcp.ServerOptions {
	return &mcp.ServerOptions{
		Instructions: "MCP server for rutracker.org. Provides tools to search torrents, inspect a topic, list the files inside a torrent without downloading it, resolve magnet links, and download .torrent files. The download tool's 'mode' selects delivery: a one-time download URL (default with the HTTP transport), compact metadata (default over stdio), or inline base64. Authenticate with RUTRACKER_USERNAME/RUTRACKER_PASSWORD or a RUTRACKER_COOKIE session override. With no RUTRACKER_BASE_URL set, the client round-robins between rutracker.org and rutracker.net.",
		Logger:       logger,
	}
}

func registerTools(server *mcp.Server, client rutracker.Client, store *artifact.Store, cfg *config.Config) {
	mcp.AddTool(server, tools.ServerVersionTool(),
		tools.NewServerVersionHandler(version, revision, runtime.Version()))
	mcp.AddTool(server, tools.SearchTool(), tools.NewSearchHandler(client))
	mcp.AddTool(server, tools.TopicInfoTool(), tools.NewTopicInfoHandler(client))
	mcp.AddTool(server, tools.FilesTool(), tools.NewFilesHandler(client))
	mcp.AddTool(server, tools.MagnetTool(), tools.NewMagnetHandler(client))
	mcp.AddTool(server, tools.DownloadTool(),
		tools.NewDownloadHandler(client, store, cfg.ArtifactBaseURLOrDefault(), cfg.HTTPEnabled()))
}

// serve runs the stdio transport and, when configured, an HTTP transport.
func serve(logger *slog.Logger, server *mcp.Server, store *artifact.Store, cfg *config.Config) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigChan:
			cancel()
		case <-ctx.Done():
		}

		signal.Stop(sigChan)
	}()

	group, groupCtx := errgroup.WithContext(ctx)
	httpEnabled := cfg.HTTPEnabled()

	// Artifacts only exist when the HTTP transport serves them, so the janitor
	// is pointless in stdio-only mode. Run it inside the group bound to groupCtx
	// so its lifetime matches the transports: it stops when any of them exits.
	if httpEnabled {
		group.Go(func() error {
			store.StartGC(groupCtx)

			return nil
		})
	}

	group.Go(func() error {
		runErr := server.Run(groupCtx, &mcp.StdioTransport{})
		if runErr != nil && groupCtx.Err() == nil {
			return errors.Wrap(runErr, "stdio server failed")
		}

		if !httpEnabled {
			cancel()
		}

		return nil
	})

	if httpEnabled {
		group.Go(func() error {
			return runHTTPServer(groupCtx, logger, server, store, cfg.HTTPAddr())
		})
	}

	//nolint:wrapcheck // errors are already wrapped inside the group goroutines.
	return group.Wait()
}

// runHTTPServer starts an HTTP/SSE transport for the MCP server. Sharing a
// single *mcp.Server across transports is safe: the SDK guards internal state
// with a mutex.
func runHTTPServer(
	ctx context.Context,
	logger *slog.Logger,
	server *mcp.Server,
	store *artifact.Store,
	addr string,
) error {
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return server },
		nil,
	)

	mux := http.NewServeMux()
	mux.Handle("GET /artifacts/{id}", artifactHandler(logger, store))
	mux.Handle("/", mcpHandler)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	//nolint:gosec // G118: shutdown uses a fresh context because ctx is already cancelled.
	go func() {
		<-ctx.Done()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()

		shutdownErr := httpServer.Shutdown(shutdownCtx) //nolint:contextcheck // fresh context for graceful shutdown.
		if shutdownErr != nil {
			logger.Error("http server shutdown failed", slog.Any("error", shutdownErr))
		}
	}()

	logger.Info("http server listening", slog.String("addr", addr))

	listenErr := httpServer.ListenAndServe()
	if errors.Is(listenErr, http.ErrServerClosed) {
		return nil
	}

	return errors.Wrap(listenErr, "HTTP listen failed")
}
