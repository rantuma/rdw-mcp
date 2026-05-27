package rdw_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/rantuma/rdw-mcp/internal/rdw"
)

// testMetricReader collects rdw.* metric samples for the assertions below.
// Set up via init() so it is installed before [sync.Once] in instruments() fires.
//
//nolint:gochecknoglobals // shared test fixture across this package's tests
var testMetricReader *sdkmetric.ManualReader

const testInstrumentationVersion = "test-version"

//nolint:gochecknoinits // installing the meter provider before any test runs
func init() {
	rdw.SetInstrumentationVersion(testInstrumentationVersion)

	testMetricReader = sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(testMetricReader),
		sdkmetric.WithView(rdw.RequestDurationView()),
	))
}

// collectAndFind returns the named metric or fails the test. Other tests in
// this binary record into the same reader; tests therefore scope assertions to
// data-point attributes rather than to total counts.
func collectAndFind(t *testing.T, name string) metricdata.Metrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, testMetricReader.Collect(context.Background(), &rm))

	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name != "github.com/rantuma/rdw-mcp/internal/rdw" {
			continue
		}

		assert.Equal(t, testInstrumentationVersion, sm.Scope.Version,
			"instrumentation scope must carry the build version")

		for _, metrics := range sm.Metrics {
			if metrics.Name == name {
				return metrics
			}
		}
	}

	require.Failf(t, "metric not found", "%s under rdw scope", name)

	return metricdata.Metrics{}
}

func attrSet(set attribute.Set) map[string]string {
	out := make(map[string]string, set.Len())
	for _, kv := range set.ToSlice() {
		out[string(kv.Key)] = kv.Value.AsString()
	}

	return out
}

func expDurationPoint(
	t *testing.T,
	metrics metricdata.Metrics,
	want map[string]string,
) (metricdata.ExponentialHistogramDataPoint[float64], bool) {
	t.Helper()

	hist, ok := metrics.Data.(metricdata.ExponentialHistogram[float64])
	require.Truef(t, ok, "%s must be a float64 exponential histogram", metrics.Name)

	for _, dp := range hist.DataPoints {
		got := attrSet(dp.Attributes)
		if attrsMatch(got, want) {
			return dp, true
		}
	}

	return metricdata.ExponentialHistogramDataPoint[float64]{}, false
}

func int64HistPoint(
	t *testing.T,
	metrics metricdata.Metrics,
	want map[string]string,
) (metricdata.HistogramDataPoint[int64], bool) {
	t.Helper()

	hist, ok := metrics.Data.(metricdata.Histogram[int64])
	require.Truef(t, ok, "%s must be an int64 histogram", metrics.Name)

	for _, dp := range hist.DataPoints {
		if attrsMatch(attrSet(dp.Attributes), want) {
			return dp, true
		}
	}

	return metricdata.HistogramDataPoint[int64]{}, false
}

func sumPoint(
	t *testing.T,
	metrics metricdata.Metrics,
	want map[string]string,
) (metricdata.DataPoint[int64], bool) {
	t.Helper()

	sum, ok := metrics.Data.(metricdata.Sum[int64])
	require.Truef(t, ok, "%s must be an int64 sum", metrics.Name)

	for _, dp := range sum.DataPoints {
		if attrsMatch(attrSet(dp.Attributes), want) {
			return dp, true
		}
	}

	return metricdata.DataPoint[int64]{}, false
}

func attrsMatch(got, want map[string]string) bool {
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}

	return true
}

func TestRDWRequestDurationRecorded(t *testing.T) {
	rdw.SetClientConfig(rdw.ClientConfig{MaxAttempts: 1})
	t.Cleanup(func() { rdw.SetClientConfig(rdw.ClientConfig{MaxAttempts: 1}) })

	ft := &flakyTransport{}
	client := &http.Client{Transport: ft}

	_, err := rdw.FetchBase(context.Background(), client, "AB12CD")
	require.NoError(t, err)

	metrics := collectAndFind(t, "rdw.client.request.duration")
	dp, found := expDurationPoint(t, metrics, map[string]string{
		"rdw.endpoint": "base",
		"rdw.status":   "ok",
	})
	require.True(t, found, "expected an ok duration sample for endpoint=base")
	assert.Positive(t, dp.Count, "the data point should have at least one observation")
}

func TestRDWRetryAttemptsHistogram(t *testing.T) {
	rdw.SetClientConfig(rdw.ClientConfig{
		MaxAttempts:    3,
		BaseBackoff:    time.Millisecond,
		MaxBackoff:     2 * time.Millisecond,
		PerCallTimeout: time.Second,
	})
	t.Cleanup(func() { rdw.SetClientConfig(rdw.ClientConfig{MaxAttempts: 1}) })

	// Two 503s force two retries; the third call returns 200, so attempts=3.
	ft := &flakyTransport{statuses: []int{
		http.StatusServiceUnavailable,
		http.StatusServiceUnavailable,
	}}
	client := &http.Client{Transport: ft}

	_, err := rdw.FetchBase(context.Background(), client, "AB12CD")
	require.NoError(t, err)

	metrics := collectAndFind(t, "rdw.client.retry.attempts")
	dp, found := int64HistPoint(t, metrics, map[string]string{"rdw.endpoint": "base"})
	require.True(t, found, "expected a retry-attempts sample for endpoint=base")

	// Sum across all samples in this series is the total attempts observed; for
	// a fresh data point in this run we expect at least 3.
	assert.GreaterOrEqualf(t, dp.Sum, int64(3),
		"sum of attempts should be ≥ 3 (got %d)", dp.Sum)
}

func TestRDWCacheHitMissCounter(t *testing.T) {
	rdw.SetClientConfig(rdw.ClientConfig{
		MaxAttempts: 1,
		CacheTTL:    time.Minute,
		CacheSize:   8,
	})
	t.Cleanup(func() { rdw.SetClientConfig(rdw.ClientConfig{MaxAttempts: 1}) })

	rdw.ResetCache()

	ft := &flakyTransport{}
	client := &http.Client{Transport: ft}

	missBefore := readCacheCount(t, "miss")
	hitBefore := readCacheCount(t, "hit")

	_, err := rdw.FetchBase(context.Background(), client, "AB12CD")
	require.NoError(t, err)
	_, err = rdw.FetchBase(context.Background(), client, "AB12CD")
	require.NoError(t, err)

	missAfter := readCacheCount(t, "miss")
	hitAfter := readCacheCount(t, "hit")

	assert.Equal(t, int64(1), missAfter-missBefore, "first call should record one miss")
	assert.Equal(t, int64(1), hitAfter-hitBefore, "second call should record one hit")
}

// readCacheCount returns the cache counter for endpoint=base and the given
// result. Tolerates "metric not present" since the counter only exists after
// the first cache op.
func readCacheCount(t *testing.T, result string) int64 {
	t.Helper()

	// Soft lookup: the counter only exists after the first cache op, so the
	// "before" call legitimately observes "metric not present" and must return 0
	// rather than failing the test.
	var rm metricdata.ResourceMetrics
	require.NoError(t, testMetricReader.Collect(context.Background(), &rm))

	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name != "github.com/rantuma/rdw-mcp/internal/rdw" {
			continue
		}
		for _, metrics := range sm.Metrics {
			if metrics.Name != "rdw.client.cache.operations" {
				continue
			}
			if dp, ok := sumPoint(t, metrics, map[string]string{
				"rdw.endpoint": "base",
				"result":       result,
			}); ok {
				return dp.Value
			}
		}
	}

	return 0
}

func TestRDWErrorClassification(t *testing.T) {
	rdw.SetClientConfig(rdw.ClientConfig{
		MaxAttempts:    5,
		BaseBackoff:    50 * time.Millisecond,
		MaxBackoff:     200 * time.Millisecond,
		PerCallTimeout: time.Second,
	})
	t.Cleanup(func() { rdw.SetClientConfig(rdw.ClientConfig{MaxAttempts: 1}) })

	ft := &flakyTransport{statuses: []int{
		http.StatusServiceUnavailable,
		http.StatusServiceUnavailable,
		http.StatusServiceUnavailable,
	}}
	client := &http.Client{Transport: ft}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := rdw.FetchBase(ctx, client, "AB12CD")
	require.Error(t, err)

	// A deadline aborting a retry sleep should surface as error.type=timeout (or
	// canceled if the parent context cancelled first). Accept either to keep the
	// test robust on slow CI hardware.
	metrics := collectAndFind(t, "rdw.client.request.duration")
	_, timeoutFound := expDurationPoint(t, metrics, map[string]string{
		"rdw.endpoint": "base",
		"rdw.status":   "error",
		"error.type":   "timeout",
	})
	_, canceledFound := expDurationPoint(t, metrics, map[string]string{
		"rdw.endpoint": "base",
		"rdw.status":   "error",
		"error.type":   "canceled",
	})
	assert.True(t, timeoutFound || canceledFound,
		"expected a duration sample tagged timeout or canceled")
}
