package telemetry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rantuma/rdw-mcp/internal/telemetry"
)

// Drives Setup's full enabled path against a stub OTLP endpoint, so install and a
// clean shutdown run without a real collector. http:// makes the exporters insecure.
func TestSetupEnabledLifecycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	clearEndpoints(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	require.True(t, telemetry.Enabled())

	shutdown, err := telemetry.Setup(context.Background(), "rdw-mcp-test", "v0.0.0")
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	assert.NoError(t, shutdown(ctx), "shutdown should flush cleanly against a healthy endpoint")
}
