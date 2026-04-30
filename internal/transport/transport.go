// Package transport provides the HTTP transport layer for the RDW MCP server:
// a configurable [http.Server] with CORS, health, readiness and structured
// request-logging middleware.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
)

// readinessProbeTimeout bounds the readiness checker invocation per /ready request.
const readinessProbeTimeout = 3 * time.Second

type (
	// Config controls the behaviour of the HTTP transport server.
	Config struct {
		// Port is the TCP port to listen on.
		Port int
		// AllowedOrigins is the CORS allow-list. Use ["*"] to allow any origin.
		// An empty list disables CORS responses entirely (only same-origin works).
		AllowedOrigins []string
		// ReadTimeout / WriteTimeout bound a single HTTP request lifecycle.
		ReadTimeout  time.Duration
		WriteTimeout time.Duration
		// ServerName, Version and TransportName are reported by /health.
		ServerName    string
		Version       string
		TransportName string
	}

	// ReadinessChecker is invoked by /ready to verify upstream connectivity.
	// Returning a non-nil error causes /ready to respond with 503.
	ReadinessChecker func(ctx context.Context) error

	// statusRecorder captures the HTTP status code written to the underlying
	// ResponseWriter so middleware can log it.
	statusRecorder struct {
		http.ResponseWriter

		status int
	}
)

// NewServer wires a configured [http.Server] with the MCP handler and the
// /health and /ready endpoints. The MCP handler is wrapped with CORS and
// request-logging middleware.
func NewServer(
	cfg Config,
	mcpHandler http.Handler,
	ready ReadinessChecker,
	log *slog.Logger,
) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/mcp", CORS(cfg.AllowedOrigins, mcpHandler))
	mux.Handle("/health", HealthHandler(cfg.ServerName, cfg.Version, cfg.TransportName, log))
	mux.Handle("/ready", ReadyHandler(ready, log))

	addr := fmt.Sprintf(":%d", cfg.Port)

	return &http.Server{
		Addr:         addr,
		Handler:      RequestLogging(log)(mux),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}
}

// CORS returns a middleware that applies the given allow-list. If the list
// contains "*" the wildcard origin is sent for all requests. Otherwise the
// request Origin is matched against the list and echoed back when allowed.
func CORS(allowed []string, next http.Handler) http.Handler {
	wildcard := slices.Contains(allowed, "*")

	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		origin := req.Header.Get("Origin")

		switch {
		case wildcard:
			rw.Header().Set("Access-Control-Allow-Origin", "*")
		case origin != "" && slices.Contains(allowed, origin):
			rw.Header().Set("Access-Control-Allow-Origin", origin)
			rw.Header().Add("Vary", "Origin")
		}

		rw.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		rw.Header().Set(
			"Access-Control-Allow-Headers",
			"Origin, Content-Type, Accept, Authorization, mcp-session-id",
		)
		rw.Header().Set("Access-Control-Expose-Headers", "mcp-session-id")

		if req.Method == http.MethodOptions {
			rw.WriteHeader(http.StatusOK)

			return
		}

		next.ServeHTTP(rw, req)
	})
}

// HealthHandler returns a liveness handler. It accepts only GET and HEAD
// requests; other methods receive 405 Method Not Allowed.
func HealthHandler(serverName, version, transportName string, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			rw.Header().Set("Allow", "GET, HEAD")
			http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)

			return
		}

		rw.Header().Set("Content-Type", "application/json")

		if req.Method == http.MethodHead {
			return
		}

		resp := map[string]string{
			"status":    "healthy",
			"server":    serverName,
			"version":   version,
			"transport": transportName,
		}

		if err := json.NewEncoder(rw).Encode(resp); err != nil && log != nil {
			log.WarnContext(req.Context(), "health handler encode error.", "err", err)
		}
	})
}

// ReadyHandler returns a readiness probe that invokes the supplied checker.
// A nil checker always reports ready.
func ReadyHandler(check ReadinessChecker, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			rw.Header().Set("Allow", "GET, HEAD")
			http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)

			return
		}

		if !runReadinessCheck(rw, req, check, log) {
			return
		}

		rw.Header().Set("Content-Type", "application/json")

		if req.Method == http.MethodHead {
			return
		}

		payload := map[string]string{"status": "ready"}
		if err := json.NewEncoder(rw).Encode(payload); err != nil && log != nil {
			log.WarnContext(req.Context(), "readiness handler encode error.", "err", err)
		}
	})
}

// runReadinessCheck executes check (if non-nil) and writes an unready
// response when it fails. It returns true when the request should continue
// with a ready response.
func runReadinessCheck(
	rw http.ResponseWriter,
	req *http.Request,
	check ReadinessChecker,
	log *slog.Logger,
) bool {
	if check == nil {
		return true
	}

	ctx, cancel := context.WithTimeout(req.Context(), readinessProbeTimeout)
	defer cancel()

	err := check(ctx)
	if err == nil {
		return true
	}

	if log != nil {
		log.WarnContext(req.Context(), "readiness probe failed.", "err", err)
	}

	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusServiceUnavailable)

	payload := map[string]string{
		"status": "unready",
		"error":  err.Error(),
	}
	if err = json.NewEncoder(rw).Encode(payload); err != nil && log != nil {
		log.WarnContext(req.Context(), "readiness handler encode error.", "err", err)
	}

	return false
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// RequestLogging returns a middleware that emits a structured log entry per
// request with method, path, status, duration and remote address.
func RequestLogging(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: rw, status: http.StatusOK}

			next.ServeHTTP(rec, req)

			if log == nil {
				return
			}

			log.LogAttrs(
				req.Context(),
				slog.LevelInfo,
				"http request",
				slog.String("method", req.Method),
				slog.String("path", req.URL.Path),
				slog.Int("status", rec.status),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote", remoteAddr(req)),
			)
		})
	}
}

func remoteAddr(req *http.Request) string {
	if fwd := req.Header.Get("X-Forwarded-For"); fwd != "" {
		if first, _, ok := strings.Cut(fwd, ","); ok {
			return strings.TrimSpace(first)
		}

		return strings.TrimSpace(fwd)
	}

	return req.RemoteAddr
}
