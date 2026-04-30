package transport_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rantuma/rdw-mcp/internal/transport"
)

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCORS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		allowed           []string
		method            string
		origin            string
		wantStatus        int
		wantNextCalled    bool
		wantAllowOrigin   string
		wantHasVaryOrigin bool
	}{
		{
			name:            "wildcard allows any origin",
			allowed:         []string{"*"},
			method:          http.MethodGet,
			origin:          "https://example.com",
			wantStatus:      http.StatusOK,
			wantNextCalled:  true,
			wantAllowOrigin: "*",
		},
		{
			name:              "whitelisted origin echoed back",
			allowed:           []string{"https://app.example.com"},
			method:            http.MethodGet,
			origin:            "https://app.example.com",
			wantStatus:        http.StatusOK,
			wantNextCalled:    true,
			wantAllowOrigin:   "https://app.example.com",
			wantHasVaryOrigin: true,
		},
		{
			name:            "non-whitelisted origin gets no ACAO",
			allowed:         []string{"https://app.example.com"},
			method:          http.MethodGet,
			origin:          "https://evil.example.com",
			wantStatus:      http.StatusOK,
			wantNextCalled:  true,
			wantAllowOrigin: "",
		},
		{
			name:            "OPTIONS preflight short-circuits",
			allowed:         []string{"*"},
			method:          http.MethodOptions,
			origin:          "https://example.com",
			wantStatus:      http.StatusOK,
			wantNextCalled:  false,
			wantAllowOrigin: "*",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			called := false
			next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				called = true
				rw.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(tc.method, "/mcp", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}

			rec := httptest.NewRecorder()
			transport.CORS(tc.allowed, next).ServeHTTP(rec, req)

			resp := rec.Result()
			defer resp.Body.Close()

			assert.Equal(t, tc.wantStatus, resp.StatusCode)
			assert.Equal(t, tc.wantNextCalled, called)
			assert.Equal(t, tc.wantAllowOrigin, resp.Header.Get("Access-Control-Allow-Origin"))

			if tc.wantHasVaryOrigin {
				assert.Contains(t, resp.Header.Values("Vary"), "Origin")
			}
		})
	}
}

func TestHealthHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		wantStatus int
		wantBody   bool
	}{
		{"GET returns body", http.MethodGet, http.StatusOK, true},
		{"HEAD returns no body", http.MethodHead, http.StatusOK, false},
		{"POST is rejected", http.MethodPost, http.StatusMethodNotAllowed, false},
		{"DELETE is rejected", http.MethodDelete, http.StatusMethodNotAllowed, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tc.method, "/health", nil)
			rec := httptest.NewRecorder()

			transport.HealthHandler("rdw-test", "v0.0.1", "streamable-http", newDiscardLogger()).
				ServeHTTP(rec, req)

			resp := rec.Result()
			defer resp.Body.Close()

			assert.Equal(t, tc.wantStatus, resp.StatusCode)

			if !tc.wantBody {
				return
			}

			var body map[string]string

			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			assert.Equal(t, "healthy", body["status"])
			assert.Equal(t, "rdw-test", body["server"])
		})
	}
}

func TestReadyHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		check      transport.ReadinessChecker
		method     string
		wantStatus int
	}{
		{
			name:       "nil checker reports ready",
			check:      nil,
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
		},
		{
			name:       "passing checker reports ready",
			check:      func(context.Context) error { return nil },
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
		},
		{
			name:       "failing checker reports unready",
			check:      func(context.Context) error { return errors.New("boom") },
			method:     http.MethodGet,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "POST is rejected",
			check:      nil,
			method:     http.MethodPost,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tc.method, "/ready", nil)
			rec := httptest.NewRecorder()

			transport.ReadyHandler(tc.check, newDiscardLogger()).ServeHTTP(rec, req)

			resp := rec.Result()
			defer resp.Body.Close()

			assert.Equal(t, tc.wantStatus, resp.StatusCode)
		})
	}
}

func TestRequestLogging(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		called = true
		rw.WriteHeader(http.StatusTeapot)
	})

	req := httptest.NewRequest(http.MethodGet, "/path", nil)
	req.RemoteAddr = "10.0.0.1:1234"

	rec := httptest.NewRecorder()
	transport.RequestLogging(newDiscardLogger())(next).ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusTeapot, rec.Result().StatusCode)
}

func TestNewServerGracefulShutdown(t *testing.T) {
	t.Parallel()

	cfg := transport.Config{
		Port:           0,
		AllowedOrigins: []string{"*"},
		ReadTimeout:    time.Second,
		WriteTimeout:   time.Second,
		ServerName:     "rdw-test",
		Version:        "v0",
		TransportName:  "streamable-http",
	}

	srv := transport.NewServer(cfg, http.NotFoundHandler(), nil, newDiscardLogger())
	require.NotNil(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, srv.Shutdown(ctx))
}
