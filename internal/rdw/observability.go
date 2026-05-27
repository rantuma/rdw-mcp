package rdw

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/trace"
)

// instrumentationScope identifies this package as the source of its telemetry.
// The build version is stamped on at first use via SetInstrumentationVersion.
const instrumentationScope = "github.com/rantuma/rdw-mcp/internal/rdw"

// Metric names. The rdw.* namespace mirrors mcp.* in the root package so dashboards
// can split by subsystem without parsing the instrumentation scope.
const (
	metricRequestDuration = "rdw.client.request.duration"
	metricRetryAttempts   = "rdw.client.retry.attempts"
	metricCacheOperations = "rdw.client.cache.operations"
)

// endpointNames maps RDW dataset IDs to short, human-readable labels for the
// rdw.endpoint metric attribute. Keeping cardinality bounded is the whole point:
// an unmapped dataset collapses to "unknown" via endpointName.
//
//nolint:gochecknoglobals // closed-set lookup table, matches the package style
var endpointNames = map[string]string{
	endpointBase:       "base",
	endpointFuel:       "fuel",
	endpointAxles:      "axles",
	endpointBody:       "body",
	endpointRecalls:    "recalls",
	endpointRecallDesc: "recall_desc",
	endpointAPK:        "apk",
	endpointDefects:    "defects",
}

// endpointName returns the short label for a dataset ID, or "unknown" when the
// caller passes a dataset not in endpointNames — bounding metric cardinality.
func endpointName(dataset string) string {
	if name, ok := endpointNames[dataset]; ok {
		return name
	}

	return "unknown"
}

// Telemetry handles + the version stamped on the instrumentation scope. The
// version is taken at first instruments() call so SetInstrumentationVersion
// (invoked by main at startup) wins over the package-init default.
//
//nolint:gochecknoglobals // intentional process-wide OTel handles
var (
	instrumentsOnce sync.Once
	rdwVersion      = "dev"
	rdwTracer       trace.Tracer
	rdwReqDuration  metric.Float64Histogram
	rdwRetryHist    metric.Int64Histogram
	rdwCacheOps     metric.Int64Counter
)

// SetInstrumentationVersion records the build version reported on the rdw
// instrumentation scope. Call once at startup before any RDW request runs;
// later calls (or those passing an empty string) are ignored.
func SetInstrumentationVersion(v string) {
	if v == "" {
		return
	}

	rdwVersion = v
}

// instruments lazily builds the tracer + instruments on first call so the scope
// version reflects the SetInstrumentationVersion call from main without racing
// with package init. Subsequent calls take the [sync.Once] fast path.
func instruments() (
	trace.Tracer,
	metric.Float64Histogram,
	metric.Int64Histogram,
	metric.Int64Counter,
) {
	instrumentsOnce.Do(func() {
		rdwTracer = otel.Tracer(
			instrumentationScope,
			trace.WithInstrumentationVersion(rdwVersion),
		)

		meter := otel.Meter(
			instrumentationScope,
			metric.WithInstrumentationVersion(rdwVersion),
		)

		rdwReqDuration = float64Histogram(meter, metricRequestDuration,
			metric.WithUnit("s"),
			metric.WithDescription("Duration of RDW upstream calls by endpoint and status."),
		)
		rdwRetryHist = int64Histogram(meter, metricRetryAttempts,
			metric.WithDescription("Attempts used per logical RDW call (1 = no retries)."),
		)
		rdwCacheOps = int64Counter(meter, metricCacheOperations,
			metric.WithDescription("RDW response-cache lookups by endpoint and result."),
		)
	})

	return rdwTracer, rdwReqDuration, rdwRetryHist, rdwCacheOps
}

// RequestDurationView returns the metric view that aggregates the RDW request
// duration histogram as a base-2 exponential (native) histogram. main passes
// this into telemetry.Setup alongside the root package's view.
//
// Same shape as the MCP call-duration view: auto-adapting resolution, no
// hand-tuned buckets to drift as latencies change.
//
//nolint:mnd // MaxSize/MaxScale are SDK-recommended defaults for native histograms
func RequestDurationView() sdkmetric.View {
	return sdkmetric.NewView(
		sdkmetric.Instrument{Name: metricRequestDuration},
		sdkmetric.Stream{Aggregation: sdkmetric.AggregationBase2ExponentialHistogram{
			MaxSize:  160,
			MaxScale: 20,
		}},
	)
}

// classifyRDWError maps a doRDWGet outcome to a bounded error.type value so
// the metric dimension stays low-cardinality. Returns "" on success.
func classifyRDWError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, errRetryable):
		// errRetryable bubbles up only after exhausting MaxAttempts, so this
		// distinguishes "we retried but upstream stayed flaky" from one-shot bugs.
		return "retryable_exhausted"
	default:
		return "internal"
	}
}

// callStatus is the rdw.status label value for a duration sample.
func callStatus(err error) string {
	if err == nil {
		return "ok"
	}

	return "error"
}

// float64Histogram / int64Histogram / int64Counter mirror the root package's
// helper: log construction errors (OTel returns a usable no-op on failure) so
// the recording call sites stay branch-free.
func float64Histogram(
	meter metric.Meter,
	name string,
	opts ...metric.Float64HistogramOption,
) metric.Float64Histogram {
	inst, err := meter.Float64Histogram(name, opts...)
	if err != nil {
		slog.Default().Error("rdw telemetry: create instrument failed",
			"instrument", name, "err", err)
	}

	return inst
}

func int64Histogram(
	meter metric.Meter,
	name string,
	opts ...metric.Int64HistogramOption,
) metric.Int64Histogram {
	inst, err := meter.Int64Histogram(name, opts...)
	if err != nil {
		slog.Default().Error("rdw telemetry: create instrument failed",
			"instrument", name, "err", err)
	}

	return inst
}

func int64Counter(
	meter metric.Meter,
	name string,
	opts ...metric.Int64CounterOption,
) metric.Int64Counter {
	inst, err := meter.Int64Counter(name, opts...)
	if err != nil {
		slog.Default().Error("rdw telemetry: create instrument failed",
			"instrument", name, "err", err)
	}

	return inst
}

// recordCacheLookup increments the cache hit/miss counter. Only invoked when
// the cache is enabled, so the metric stays silent in cache-off configurations
// rather than reporting every call as a miss.
func recordCacheLookup(
	ctx context.Context,
	counter metric.Int64Counter,
	endpoint string,
	hit bool,
) {
	result := "miss"
	if hit {
		result = "hit"
	}

	counter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("rdw.endpoint", endpoint),
		attribute.String("result", result),
	))
}
