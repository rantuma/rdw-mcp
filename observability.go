package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationScope = "github.com/rantuma/rdw-mcp"

// WithInstrumentationVersion stamps the build version (injected via -ldflags into
// main.version) onto the scope so dashboards can distinguish instrumentation revisions.
//
//nolint:gochecknoglobals // package handles to the global OTel provider
var (
	tracer = otel.Tracer(instrumentationScope, trace.WithInstrumentationVersion(version))
	meter  = otel.Meter(instrumentationScope, metric.WithInstrumentationVersion(version))

	callDuration = float64Histogram(
		callDurationName,
		metric.WithUnit("s"),
		metric.WithDescription("Duration of MCP method calls by method, tool and status."),
	)
)

const (
	callDurationName = "mcp.server.call.duration"
	// statusOK / statusError are the two mcp.status label values; the const
	// lets the panic + normal paths share one literal (goconst).
	statusOK    = "ok"
	statusError = "error"
)

// Base-2 exponential (native) histogram: resolution auto-adapts, no hand-tuned
// buckets to drift. Passed to telemetry.Setup, which applies it on the provider.
//
//nolint:gochecknoglobals,mnd // package handle for telemetry.Setup; MaxSize/MaxScale are SDK-recommended defaults
var callDurationView = sdkmetric.NewView(
	sdkmetric.Instrument{Name: callDurationName},
	sdkmetric.Stream{Aggregation: sdkmetric.AggregationBase2ExponentialHistogram{
		MaxSize:  160,
		MaxScale: 20,
	}},
)

// Logs (rather than drops) a construction error; OTel returns a usable no-op on
// error, so recording call sites stay safe.
func float64Histogram(name string, opts ...metric.Float64HistogramOption) metric.Float64Histogram {
	inst, err := meter.Float64Histogram(name, opts...)
	if err != nil {
		slog.Default().Error("telemetry: create instrument failed", "instrument", name, "err", err)
	}
	return inst
}

// Spans and records a duration histogram for every MCP method call. Signals carry
// mcp.method + status (and error.type on failure); tools/call adds mcp.tool.
//
// Named returns + a deferred recorder keep telemetry reliable: even on a panic in
// next, the span is ended and the histogram observes the failed call. The panic
// is then re-thrown so callers see the original behaviour.
//
//nolint:nonamedreturns // intentional: the defer reads result+err after next returns
func instrumentMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (result mcp.Result, err error) {
		spanName, attrs := describeCall(method, req)

		ctx, span := tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
		// Separate defer so spancheck can see it; LIFO ordering means the metric
		// recorder below runs while the span is still active.
		defer span.End()
		start := time.Now()

		status := statusOK
		errType := ""

		defer func() {
			if recovered := recover(); recovered != nil {
				status = statusError
				errType = "panic"
				panicErr := fmt.Errorf("panic: %v", recovered)
				span.RecordError(panicErr,
					trace.WithAttributes(attribute.String("error.type", errType)))
				span.SetAttributes(attribute.String("error.type", errType))
				span.SetStatus(codes.Error, "panic")

				recordCallDuration(ctx, attrs, status, errType, time.Since(start))

				panic(recovered)
			}

			recordCallDuration(ctx, attrs, status, errType, time.Since(start))
		}()

		result, err = next(ctx, method, req)

		toolErr := isToolError(result)
		errType = classifyError(err, toolErr)

		switch {
		case err != nil:
			status = statusError
			// error.type travels on both the exception event (for error-surfacing UIs)
			// and the span itself (for trace-attribute queries).
			span.RecordError(err, trace.WithAttributes(attribute.String("error.type", errType)))
			span.SetStatus(codes.Error, err.Error())
		case toolErr:
			status = "tool_error"
			span.SetStatus(codes.Error, "tool reported error")
		}

		if errType != "" {
			span.SetAttributes(attribute.String("error.type", errType))
		}

		return result, err
	}
}

// Builds the metric attribute set and records the call duration. Kept separate so
// the normal and panic paths in instrumentMiddleware share one definition.
func recordCallDuration(
	ctx context.Context,
	base []attribute.KeyValue,
	status, errType string,
	elapsed time.Duration,
) {
	// Fresh slice: base was already handed to the span, so don't append onto it.
	const outcomeAttrs = 2

	recordAttrs := make([]attribute.KeyValue, 0, len(base)+outcomeAttrs)
	recordAttrs = append(recordAttrs, base...)
	recordAttrs = append(recordAttrs, attribute.String("mcp.status", status))

	if errType != "" {
		recordAttrs = append(recordAttrs, attribute.String("error.type", errType))
	}

	// Rate is derived from the histogram's _count, so there is no separate counter.
	callDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(recordAttrs...))
}

// Maps an outcome to a bounded error.type value (or "" on success), so the metric
// dimension stays low-cardinality.
func classifyError(err error, toolErr bool) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case err != nil:
		return "internal"
	case toolErr:
		return "tool_error"
	default:
		return ""
	}
}

// Derives the span name and base attributes for a call. The tool name is untrusted
// client input, so it collapses to "unknown" unless registered — bounding cardinality.
func describeCall(method string, req mcp.Request) (string, []attribute.KeyValue) {
	if method != "tools/call" {
		return "mcp " + method, []attribute.KeyValue{attribute.String("mcp.method", method)}
	}

	tool := "unknown"
	if params, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok && isRegisteredTool(params.Name) {
		tool = params.Name
	}

	return "mcp.tool " + tool, []attribute.KeyValue{
		attribute.String("mcp.method", method),
		attribute.String("mcp.tool", tool),
	}
}

// Reports whether a result is a tool call that flagged an error in its payload
// (distinct from a transport/handler error).
func isToolError(result mcp.Result) bool {
	res, ok := result.(*mcp.CallToolResult)

	return ok && res.IsError
}
