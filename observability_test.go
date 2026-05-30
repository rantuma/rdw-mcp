package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Package-level tracer/meter bind to the first provider installed in the process,
// so the collectors are installed once here and subtests isolate data by tool name.
func TestInstrumentMiddleware(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr)))

	reader := sdkmetric.NewManualReader()
	// Same exponential-histogram view as production, so callDuration records the
	// same way here.
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithView(callDurationView),
	))

	t.Run("instruments non-tool methods with a method span", func(t *testing.T) {
		called := false
		next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
			called = true

			return &mcp.ListToolsResult{}, nil
		}

		_, err := instrumentMiddleware(
			next,
		)(
			context.Background(),
			"tools/list",
			&mcp.ListToolsRequest{},
		)
		require.NoError(t, err)
		assert.True(t, called, "next handler must be invoked")

		span := findSpanByName(sr, "mcp tools/list")
		require.NotNil(t, span, "a span should be recorded for non-tool methods")
		assert.Equal(t, "tools/list", attrString(span.Attributes(), "mcp.method"))
		assert.Empty(t, attrString(span.Attributes(), "mcp.tool"),
			"non-tool methods must not carry an mcp.tool attribute")
	})

	t.Run("span status", func(t *testing.T) {
		tests := []struct {
			tool        string
			result      mcp.Result
			err         error
			wantStatus  codes.Code
			wantErrEvt  bool
			wantErrType string
		}{
			{toolNameFull, &mcp.CallToolResult{}, nil, codes.Unset, false, ""},
			{toolNameBasic, nil, errors.New("boom"), codes.Error, true, "internal"},
			{
				toolNameTechnical, &mcp.CallToolResult{IsError: true}, nil,
				codes.Error, false, "tool_error",
			},
		}

		for _, tc := range tests {
			t.Run(tc.tool, func(t *testing.T) {
				next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
					return tc.result, tc.err
				}

				_, err := instrumentMiddleware(next)(
					context.Background(), "tools/call", callToolReq(tc.tool),
				)
				require.ErrorIs(t, err, tc.err)

				span := findSpan(sr, tc.tool)
				require.NotNil(t, span)

				assert.Equal(t, "mcp.tool "+tc.tool, span.Name())
				assert.Equal(t, tc.tool, attrString(span.Attributes(), "mcp.tool"))
				assert.Equal(t, tc.wantStatus, span.Status().Code)
				assert.Equal(t, tc.wantErrEvt, hasExceptionEvent(span), "exception event presence")
				assert.Equal(t, tc.wantErrType, attrString(span.Attributes(), "error.type"),
					"error.type span attribute")

				// When an exception event is recorded it must also carry the
				// classified error.type, so backends that key off events (Sentry,
				// Honeycomb) see the classification.
				if tc.wantErrEvt {
					assert.Equal(t, tc.wantErrType, exceptionEventAttr(span, "error.type"),
						"error.type exception event attribute")
				}
			})
		}
	})

	t.Run("panic in handler is recovered, recorded, and re-thrown", func(t *testing.T) {
		next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
			panic("boom")
		}

		// A panic must not silently drop the span or the duration measurement —
		// telemetry is least reliable exactly when you need it most. The middleware
		// re-throws so application semantics are unchanged.
		assert.PanicsWithValue(t, "boom", func() {
			_, _ = instrumentMiddleware(next)(
				context.Background(), "tools/call", callToolReq(toolNameDefects),
			)
		})

		span := findSpan(sr, toolNameDefects)
		require.NotNil(t, span, "span must be ended even when next panics")
		assert.Equal(t, codes.Error, span.Status().Code)
		assert.Equal(t, "panic", attrString(span.Attributes(), "error.type"))
		assert.True(t, hasExceptionEvent(span), "panic must record an exception event")
		assert.Equal(t, "panic", exceptionEventAttr(span, "error.type"))

		var rm metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(context.Background(), &rm))
		hist := findHistogram(t, rm, "mcp.server.call.duration")
		assert.Equal(t, uint64(1), histogramCountFor(hist, toolNameDefects, "error", "panic"),
			"a panic must produce one duration sample tagged error/panic")
	})

	t.Run("unregistered tool name is bucketed to unknown", func(t *testing.T) {
		next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
			return nil, errors.New("no such tool")
		}

		// An arbitrary, attacker-controlled name must not appear verbatim: it
		// would poison the mcp.tool metric label with unbounded cardinality.
		_, _ = instrumentMiddleware(next)(
			context.Background(), "tools/call", callToolReq("definitely-not-a-real-tool"),
		)

		assert.Nil(t, findSpan(sr, "definitely-not-a-real-tool"),
			"unregistered name must not become a span name")
		span := findSpanByName(sr, "mcp.tool unknown")
		require.NotNil(t, span, "unregistered tool must collapse to \"unknown\"")
		assert.Equal(t, "unknown", attrString(span.Attributes(), "mcp.tool"))
	})

	t.Run("metrics", func(t *testing.T) {
		const tool = toolNameRecalls

		cases := []struct {
			result      mcp.Result
			err         error
			status      string
			wantErrType string
		}{
			{&mcp.CallToolResult{}, nil, "ok", ""},
			{nil, errors.New("boom"), "error", "internal"},
			{nil, context.DeadlineExceeded, "error", "timeout"},
			{&mcp.CallToolResult{IsError: true}, nil, "tool_error", "tool_error"},
		}

		for _, c := range cases {
			next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
				return c.result, c.err
			}
			_, _ = instrumentMiddleware(next)(
				context.Background(), "tools/call", callToolReq(tool),
			)
		}

		var rm metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(context.Background(), &rm))

		// The duration histogram must be exponential (native), not the SDK's
		// ms-tuned default explicit buckets — findHistogram asserts the type.
		hist := findHistogram(t, rm, "mcp.server.call.duration")
		require.NotEmpty(t, hist)

		// "error" appears twice (internal + timeout), so it carries two series
		// distinguished by error.type; the others appear once.
		for _, c := range cases {
			assert.Equalf(t, uint64(1), histogramCountFor(hist, tool, c.status, c.wantErrType),
				"duration count for status %q error.type %q", c.status, c.wantErrType)
		}
	})
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		toolErr bool
		want    string
	}{
		{"success", nil, false, ""},
		{"tool error", nil, true, "tool_error"},
		{"deadline exceeded", context.DeadlineExceeded, false, "timeout"},
		{"wrapped deadline", fmt.Errorf("rdw: %w", context.DeadlineExceeded), false, "timeout"},
		{"canceled", context.Canceled, false, "canceled"},
		{"generic error", errors.New("boom"), false, "internal"},
		// A transport error takes precedence over a tool-error result.
		{"error wins over toolErr", errors.New("boom"), true, "internal"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyError(tc.err, tc.toolErr))
		})
	}
}

func callToolReq(tool string) mcp.Request {
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: tool}}
}

func findSpan(sr *tracetest.SpanRecorder, tool string) sdktrace.ReadOnlySpan {
	return findSpanByName(sr, "mcp.tool "+tool)
}

func findSpanByName(sr *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	for _, s := range sr.Ended() {
		if s.Name() == name {
			return s
		}
	}

	return nil
}

func attrString(attrs []attribute.KeyValue, key string) string {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}

	return ""
}

func hasExceptionEvent(span sdktrace.ReadOnlySpan) bool {
	for _, e := range span.Events() {
		if e.Name == "exception" {
			return true
		}
	}

	return false
}

// exceptionEventAttr returns the first exception event's value for key, or "" if
// the span has no exception event or the key is absent.
func exceptionEventAttr(span sdktrace.ReadOnlySpan, key string) string {
	for _, e := range span.Events() {
		if e.Name != "exception" {
			continue
		}

		return attrString(e.Attributes, key)
	}

	return ""
}

// Asserts the metric is an exponential (native) histogram, not the SDK default.
func findHistogram(
	t *testing.T, rm metricdata.ResourceMetrics, name string,
) []metricdata.ExponentialHistogramDataPoint[float64] {
	t.Helper()

	hist, ok := findMetric(t, rm, name).Data.(metricdata.ExponentialHistogram[float64])
	require.Truef(t, ok, "%s is not a float64 ExponentialHistogram", name)

	return hist.DataPoints
}

func findMetric(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name != instrumentationScope {
			continue
		}
		// Build identity should travel with every metric so backends can split by it.
		assert.NotEmpty(t, sm.Scope.Version,
			"instrumentation scope %s must carry a Version", sm.Scope.Name)
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m
			}
		}
	}

	require.Failf(t, "metric not found", "%s under scope %s", name, instrumentationScope)

	return metricdata.Metrics{}
}

// Count for the data point tagged with the given tool, status and error.type.
func histogramCountFor(
	points []metricdata.ExponentialHistogramDataPoint[float64], tool, status, errType string,
) uint64 {
	for _, dp := range points {
		if matchesSeries(dp.Attributes, tool, status, errType) {
			return dp.Count
		}
	}

	return 0
}

func matchesSeries(set attribute.Set, tool, status, errType string) bool {
	gotTool, _ := set.Value("mcp.tool")
	gotStatus, _ := set.Value("mcp.status")
	gotErrType, _ := set.Value("error.type")

	return gotTool.AsString() == tool &&
		gotStatus.AsString() == status &&
		gotErrType.AsString() == errType
}
