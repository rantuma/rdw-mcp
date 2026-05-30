// Package telemetry wires OpenTelemetry traces, metrics and logs to an OTLP/HTTP
// backend via the standard OTEL_* env vars. Setup is fail-open: with no endpoint
// configured it installs no-op providers and can never take the process down.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

type (
	// ShutdownFunc flushes and shuts down the installed providers. Always non-nil.
	ShutdownFunc func(context.Context) error
)

func noopShutdown(context.Context) error { return nil }

// Per-signal gating matters: building an exporter for a signal with no endpoint
// makes it silently default to localhost:4318 and spam export errors.
func tracesEnabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

// metricsEnabled reports whether metrics should be exported. See tracesEnabled.
func metricsEnabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != ""
}

// LogsEnabled reports whether logs should be exported. See tracesEnabled.
func LogsEnabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") != ""
}

// Enabled reports whether any OTLP endpoint is configured. Honors the standard
// OTEL_SDK_DISABLED kill switch so operators can disable telemetry without
// unsetting endpoint vars.
func Enabled() bool {
	if disabled, _ := strconv.ParseBool(os.Getenv("OTEL_SDK_DISABLED")); disabled {
		return false
	}

	return tracesEnabled() || metricsEnabled() || LogsEnabled()
}

func Setup(
	ctx context.Context,
	serviceName, serviceVersion string,
	metricViews ...sdkmetric.View,
) (ShutdownFunc, error) {
	// Always installed (even with export off) so the server joins inbound traces
	// and forwards baggage.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !Enabled() {
		return noopShutdown, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
			// Before WithFromEnv so OTEL_RESOURCE_ATTRIBUTES can still override it.
			semconv.ServiceInstanceID(instanceID()),
		),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithHost(),
	)
	if err != nil {
		if res == nil {
			return noopShutdown, err
		}
		// A partial resource is still usable; surface the error, don't drop it.
		slog.Default().WarnContext(ctx, "telemetry: partial resource detection", "err", err)
	}

	var shutdowns []func(context.Context) error

	if tracesEnabled() {
		traceExp, traceErr := otlptracehttp.New(ctx)
		if traceErr != nil {
			return noopShutdown, traceErr
		}

		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExp),
			sdktrace.WithResource(res),
			sdktrace.WithSampler(newSampler()),
		)
		otel.SetTracerProvider(tp)
		shutdowns = append(shutdowns, tp.Shutdown)
	}

	if metricsEnabled() {
		metricExp, metricErr := otlpmetrichttp.New(ctx)
		if metricErr != nil {
			// Tear down anything already installed before bailing out.
			return noopShutdown, errors.Join(metricErr, combine(shutdowns)(ctx))
		}

		// PeriodicReader respects OTEL_METRIC_EXPORT_INTERVAL (spec default 60s) when
		// WithInterval is not set, so we leave it off and let operators tune via env.
		mp := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
			sdkmetric.WithResource(res),
			sdkmetric.WithView(metricViews...),
		)
		otel.SetMeterProvider(mp)
		shutdowns = append(shutdowns, mp.Shutdown)

		// Go runtime metrics (goroutines, heap, GC). Fail-open: don't abort on error.
		if rtErr := runtime.Start(runtime.WithMeterProvider(mp)); rtErr != nil {
			slog.Default().WarnContext(ctx, "telemetry: runtime metrics start failed", "err", rtErr)
		}
	}

	if LogsEnabled() {
		logExp, logErr := otlploghttp.New(ctx)
		if logErr != nil {
			return noopShutdown, errors.Join(logErr, combine(shutdowns)(ctx))
		}

		lp := sdklog.NewLoggerProvider(
			sdklog.WithResource(res),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		)
		// Global so otelslog.NewHandler (wired in main) resolves it with no plumbing.
		otellogglobal.SetLoggerProvider(lp)
		shutdowns = append(shutdowns, lp.Shutdown)
	}

	return combine(shutdowns), nil
}

// Random UUIDv4 for service.instance.id; falls back to a timestamp string if the
// system entropy source fails.
func instanceID() string {
	id, err := uuid.NewRandom()
	if err != nil {
		return fmt.Sprintf("instance-%d", time.Now().UnixNano())
	}

	return id.String()
}

// Builds the sampler from OTEL_TRACES_SAMPLER / _ARG. The base Go SDK doesn't read
// these itself, so without this it would sample every span. Default: parentbased_always_on.
func newSampler() sdktrace.Sampler {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER"))) {
	case "", "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(samplerRatio(1))
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(samplerRatio(1)))
	default:
		slog.Default().Warn("telemetry: unknown OTEL_TRACES_SAMPLER; using parentbased_always_on",
			"value", os.Getenv("OTEL_TRACES_SAMPLER"))

		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}

func samplerRatio(def float64) float64 {
	raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG"))
	if raw == "" {
		return def
	}

	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		slog.Default().Warn("telemetry: invalid OTEL_TRACES_SAMPLER_ARG; using default",
			"value", raw, "default", def)

		return def
	}

	switch {
	case ratio < 0:
		return 0
	case ratio > 1:
		return 1
	default:
		return ratio
	}
}

// Folds the providers' shutdowns into one, joining their errors. Always non-nil.
func combine(shutdowns []func(context.Context) error) ShutdownFunc {
	return func(ctx context.Context) error {
		errs := make([]error, 0, len(shutdowns))
		for _, fn := range shutdowns {
			errs = append(errs, fn(ctx))
		}

		return errors.Join(errs...)
	}
}
