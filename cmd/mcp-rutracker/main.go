// Command mcp-rutracker is an MCP server exposing rutracker.org search and
// download tools over stdio and, optionally, HTTP.
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"

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
	err := run()
	if err != nil {
		log.Printf("server error: %v", err)
		os.Exit(1)
	}
}

// requireCredentials fails fast at startup when nothing can authenticate, so the
// operator learns at launch rather than on the first query.
func requireCredentials(cfg *config.Config) error {
	if !cfg.HasCredentials() {
		return ErrNoCredentials
	}

	return nil
}

func run() error {
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
		newServerOptions(),
	)

	registerTools(server, client, cfg.DownloadDir)

	return serve(server, cfg)
}

func newServerOptions() *mcp.ServerOptions {
	return &mcp.ServerOptions{
		Instructions: "MCP server for rutracker.org. Provides tools to search " +
			"torrents, inspect a topic, list the files inside a torrent without " +
			"downloading it, resolve magnet links, and download .torrent files " +
			"(returned as base64, ready to hand to a BitTorrent client). " +
			"Authenticate with RUTRACKER_USERNAME/RUTRACKER_PASSWORD or a " +
			"RUTRACKER_COOKIE session override. With no RUTRACKER_BASE_URL set, " +
			"the client round-robins between rutracker.org and rutracker.net.",
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}
}

func registerTools(server *mcp.Server, client rutracker.Client, downloadDir string) {
	mcp.AddTool(server, tools.ServerVersionTool(),
		tools.NewServerVersionHandler(version, revision, runtime.Version()))
	mcp.AddTool(server, tools.SearchTool(), tools.NewSearchHandler(client))
	mcp.AddTool(server, tools.TopicInfoTool(), tools.NewTopicInfoHandler(client))
	mcp.AddTool(server, tools.FilesTool(), tools.NewFilesHandler(client))
	mcp.AddTool(server, tools.MagnetTool(), tools.NewMagnetHandler(client))
	mcp.AddTool(server, tools.DownloadTool(), tools.NewDownloadHandler(client, downloadDir))
}

// serve runs the stdio transport and, when configured, an HTTP transport.
func serve(server *mcp.Server, cfg *config.Config) error {
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
			return runHTTPServer(groupCtx, server, cfg.HTTPAddr())
		})
	}

	//nolint:wrapcheck // errors are already wrapped inside the group goroutines.
	return group.Wait()
}

// runHTTPServer starts an HTTP/SSE transport for the MCP server. Sharing a
// single *mcp.Server across transports is safe: the SDK guards internal state
// with a mutex.
func runHTTPServer(ctx context.Context, server *mcp.Server, addr string) error {
	handler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return server },
		nil,
	)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	//nolint:gosec // G118: shutdown uses a fresh context because ctx is already cancelled.
	go func() {
		<-ctx.Done()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()

		shutdownErr := httpServer.Shutdown(shutdownCtx) //nolint:contextcheck // fresh context for graceful shutdown.
		if shutdownErr != nil {
			log.Printf("HTTP server shutdown error: %v", shutdownErr)
		}
	}()

	log.Printf("HTTP server listening on %s", addr)

	listenErr := httpServer.ListenAndServe()
	if errors.Is(listenErr, http.ErrServerClosed) {
		return nil
	}

	return errors.Wrap(listenErr, "HTTP listen failed")
}
