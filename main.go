// Package main is the entry point for the RDW MCP server.
//
// Usage:
//
//	rdw-mcp               # stdio transport (default, for Claude Desktop)
//	rdw-mcp --http        # HTTP transport on default port 3000
//	rdw-mcp --http --port=8080
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rantuma/rdw-mcp/internal/rdw"
	"github.com/rantuma/rdw-mcp/internal/transport"
)

const (
	serverName     = "rdw-mcp-server"
	defaultPort    = 3000
	shutdownPeriod = 10 * time.Second
)

// Build-time identification, injected via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
//
//nolint:gochecknoglobals // intentional injectable build identity
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func serverVersion() string { return version }

func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = rdw.HTTPTimeout
	}

	return &http.Client{Timeout: timeout}
}

func newMCPServer(client *http.Client) *mcp.Server {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: serverVersion()},
		nil,
	)
	registerAll(srv, client)

	return srv
}

func startStdio(ctx context.Context, log *slog.Logger, client *http.Client) error {
	srv := newMCPServer(client)

	log.InfoContext(ctx, "RDW MCP Server running on stdio.")

	return srv.Run(ctx, &mcp.StdioTransport{})
}

func startHTTP(ctx context.Context, log *slog.Logger, client *http.Client, env envConfig) error {
	mcpSrv := newMCPServer(client)
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpSrv },
		nil,
	)

	ready := newReadinessChecker(client)

	httpSrv := transport.NewServer(transport.Config{
		Port:           env.Port,
		AllowedOrigins: env.AllowedOrigins,
		ReadTimeout:    env.HTTPTimeout,
		WriteTimeout:   env.HTTPTimeout,
		ServerName:     serverName,
		Version:        serverVersion(),
		TransportName:  "streamable-http",
	}, mcpHandler, ready, log)

	log.LogAttrs(
		ctx,
		slog.LevelInfo,
		"RDW MCP Server (HTTP) listening.",
		slog.String("addr", httpSrv.Addr),
	)

	errCh := make(chan error, 1)

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err

			return
		}

		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		log.InfoContext(ctx, "shutdown signal received, stopping HTTP server.")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownPeriod)
		defer cancel()

		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}

		return <-errCh
	case err := <-errCh:
		return err
	}
}

// newReadinessChecker returns a checker that performs a HEAD request against
// the RDW base endpoint to verify upstream connectivity.
func newReadinessChecker(client *http.Client) transport.ReadinessChecker {
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(
			ctx, http.MethodHead, rdw.APIBase+"/m9d7-ebf2.json", nil,
		)
		if err != nil {
			return fmt.Errorf("build readiness request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("rdw unreachable: %w", err)
		}

		defer resp.Body.Close()

		if resp.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("rdw status %d", resp.StatusCode)
		}

		return nil
	}
}

func main() {
	env := loadEnvConfig()

	httpMode := flag.Bool("http", false, "Start HTTP server instead of stdio.")
	port := flag.Int("port", env.Port, "Port for HTTP server.")
	showVersion := flag.Bool("version", false, "Print version information and exit.")
	flag.Parse()

	if *showVersion {
		if _, err := fmt.Fprintf(
			os.Stdout,
			"%s %s (commit %s, built %s)\n",
			serverName, version, commit, date,
		); err != nil {
			fmt.Fprintln(os.Stderr, "failed to write version:", err)
			os.Exit(1)
		}

		return
	}

	env.Port = *port

	rdw.SetUserAgent(fmt.Sprintf("RDW-MCP-Server/%s", version))
	// Enable retries + response cache in production. Tests override via TestMain.
	rdw.SetClientConfig(rdw.ClientConfig{
		MaxAttempts:    rdw.DefaultMaxAttempts,
		BaseBackoff:    rdw.DefaultBaseBackoff,
		MaxBackoff:     rdw.DefaultMaxBackoff,
		PerCallTimeout: rdw.DefaultPerCallTimeout,
		CacheTTL:       rdw.DefaultCacheTTL,
		CacheSize:      rdw.DefaultCacheSize,
	})

	log := slog.New(slog.NewTextHandler(
		os.Stderr,
		&slog.HandlerOptions{Level: env.LogLevel},
	))
	client := newHTTPClient(env.HTTPTimeout)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var err error
	if *httpMode {
		err = startHTTP(ctx, log, client, env)
	} else {
		err = startStdio(ctx, log, client)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		log.LogAttrs(
			context.Background(),
			slog.LevelError,
			"server error.",
			slog.Any("err", err),
		)
		cancel()
		exit(1)
	}
}

// exit is a package-level indirection for [os.Exit] so the surrounding
// function can use defer without tripping gocritic's exitAfterDefer check.
//
//nolint:gochecknoglobals // intentional indirection for testability and lint compliance
var exit = os.Exit
