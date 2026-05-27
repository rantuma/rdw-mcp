package telemetry_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/rantuma/rdw-mcp/internal/telemetry"
)

// otlpEndpointVars are the env vars that, when any is set, enable telemetry.
func otlpEndpointVars() []string {
	return []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
	}
}

// clearEndpoints unsets every OTLP endpoint var for the duration of the test.
// (t.Setenv to "" is treated as unset by telemetry.Enabled.) Also clears
// OTEL_SDK_DISABLED so a prior test can't leak it into Enabled() decisions.
func clearEndpoints(t *testing.T) {
	t.Helper()
	for _, k := range otlpEndpointVars() {
		t.Setenv(k, "")
	}
	t.Setenv("OTEL_SDK_DISABLED", "")
}

func TestEnabled(t *testing.T) {
	t.Run("disabled when no endpoint is set", func(t *testing.T) {
		clearEndpoints(t)
		assert.False(t, telemetry.Enabled())
	})

	for _, key := range otlpEndpointVars() {
		t.Run("enabled via "+key, func(t *testing.T) {
			clearEndpoints(t)
			t.Setenv(key, "http://localhost:4318")
			assert.True(t, telemetry.Enabled())
		})
	}
}

func TestSDKDisabledOverridesEndpoint(t *testing.T) {
	// Setting an endpoint normally enables telemetry; OTEL_SDK_DISABLED=true must
	// override it so operators can kill exports without unsetting endpoint vars.
	clearEndpoints(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	require.True(t, telemetry.Enabled(), "precondition: endpoint alone enables telemetry")

	t.Setenv("OTEL_SDK_DISABLED", "true")
	assert.False(t, telemetry.Enabled(), "OTEL_SDK_DISABLED=true must disable telemetry")

	shutdown, err := telemetry.Setup(context.Background(), "rdw-mcp-test", "v0.0.0")
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	assert.NoError(t, shutdown(context.Background()),
		"no exporters were constructed, so shutdown must be a clean no-op")
}

func TestSetupDisabledIsFailOpen(t *testing.T) {
	clearEndpoints(t)

	shutdown, err := telemetry.Setup(context.Background(), "rdw-mcp-test", "v0.0.0")
	require.NoError(t, err)
	require.NotNil(t, shutdown, "shutdown must always be non-nil")
	assert.NoError(t, shutdown(context.Background()))
}

func TestSetupInstallsPropagator(t *testing.T) {
	clearEndpoints(t)

	_, err := telemetry.Setup(context.Background(), "rdw-mcp-test", "v0.0.0")
	require.NoError(t, err)

	// Both propagators must be installed even when export is disabled: traceparent
	// so the server joins an inbound distributed trace, baggage so caller context
	// propagates alongside it.
	fields := otel.GetTextMapPropagator().Fields()
	assert.Contains(t, fields, "traceparent")
	assert.Contains(t, fields, "baggage")
}

func TestLogsEnabled(t *testing.T) {
	t.Run("disabled when no endpoint is set", func(t *testing.T) {
		clearEndpoints(t)
		assert.False(t, telemetry.LogsEnabled())
	})

	for _, key := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"} {
		t.Run("enabled via "+key, func(t *testing.T) {
			clearEndpoints(t)
			t.Setenv(key, "http://localhost:4318")
			assert.True(t, telemetry.LogsEnabled())
		})
	}

	t.Run("not enabled by a traces-only endpoint", func(t *testing.T) {
		clearEndpoints(t)
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://localhost:4318")
		assert.False(t, telemetry.LogsEnabled())
	})
}

func TestLogHandlerTraceCorrelation(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(telemetry.NewLogHandler(slog.NewTextHandler(&buf, nil)))

	t.Run("no trace IDs outside a span", func(t *testing.T) {
		buf.Reset()
		log.InfoContext(context.Background(), "no span")
		assert.NotContains(t, buf.String(), "trace_id")
		assert.NotContains(t, buf.String(), "span_id")
	})

	t.Run("trace IDs added inside a span", func(t *testing.T) {
		buf.Reset()
		tid, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
		require.NoError(t, err)
		sid, err := trace.SpanIDFromHex("0102030405060708")
		require.NoError(t, err)
		ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(
			trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled},
		))

		log.InfoContext(ctx, "in span")

		out := buf.String()
		assert.Contains(t, out, "trace_id="+tid.String())
		assert.Contains(t, out, "span_id="+sid.String())
	})

	t.Run("correlation survives a derived logger", func(t *testing.T) {
		buf.Reset()
		tid, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
		require.NoError(t, err)
		sid, err := trace.SpanIDFromHex("0102030405060708")
		require.NoError(t, err)
		ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(
			trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled},
		))

		log.With("component", "test").InfoContext(ctx, "in span")

		assert.Contains(t, buf.String(), "trace_id="+tid.String())
	})
}
