package rdw_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rantuma/rdw-mcp/internal/rdw"
)

type (
	// flakyTransport returns the configured statuses in order, then 200 with body
	// `[{}]` for any subsequent call.
	flakyTransport struct {
		statuses []int
		calls    atomic.Int32
	}
)

func (f *flakyTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	idx := int(f.calls.Add(1)) - 1
	rec := httptest.NewRecorder()

	if idx < len(f.statuses) {
		http.Error(rec, "fail", f.statuses[idx])
	} else {
		rec.Header().Set("Content-Type", "application/json")

		if _, err := rec.WriteString(`[{"kenteken":"AB12CD"}]`); err != nil {
			return nil, err
		}
	}

	return rec.Result(), nil
}

func TestRetryBehaviour(t *testing.T) {
	tests := []struct {
		name        string
		attempts    int
		statuses    []int
		wantErr     bool
		wantErrText string
		wantCalls   int32
	}{
		{
			name:      "succeeds on first try",
			attempts:  3,
			statuses:  nil,
			wantCalls: 1,
		},
		{
			name:      "retries on 503 then succeeds",
			attempts:  3,
			statuses:  []int{http.StatusServiceUnavailable},
			wantCalls: 2,
		},
		{
			name:      "retries on 429 then succeeds",
			attempts:  3,
			statuses:  []int{http.StatusTooManyRequests, http.StatusTooManyRequests},
			wantCalls: 3,
		},
		{
			name:        "no retry on 400",
			attempts:    3,
			statuses:    []int{http.StatusBadRequest, http.StatusBadRequest},
			wantErr:     true,
			wantErrText: "400",
			wantCalls:   1,
		},
		{
			name:     "exhausts retries on persistent 500",
			attempts: 2,
			statuses: []int{
				http.StatusInternalServerError,
				http.StatusInternalServerError,
				http.StatusInternalServerError,
			},
			wantErr:     true,
			wantErrText: "500",
			wantCalls:   2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rdw.SetClientConfig(rdw.ClientConfig{
				MaxAttempts:    tc.attempts,
				BaseBackoff:    time.Millisecond,
				MaxBackoff:     2 * time.Millisecond,
				PerCallTimeout: time.Second,
			})
			t.Cleanup(func() {
				rdw.SetClientConfig(rdw.ClientConfig{MaxAttempts: 1})
			})

			ft := &flakyTransport{statuses: tc.statuses}
			client := &http.Client{Transport: ft}

			_, err := rdw.FetchBase(context.Background(), client, "AB12CD")

			if tc.wantErr {
				require.Error(t, err)

				if tc.wantErrText != "" {
					assert.Contains(t, err.Error(), tc.wantErrText)
				}
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.wantCalls, ft.calls.Load())
		})
	}
}

func TestRetryRespectsContextCancel(t *testing.T) {
	rdw.SetClientConfig(rdw.ClientConfig{
		MaxAttempts:    5,
		BaseBackoff:    50 * time.Millisecond,
		MaxBackoff:     200 * time.Millisecond,
		PerCallTimeout: time.Second,
	})
	t.Cleanup(func() {
		rdw.SetClientConfig(rdw.ClientConfig{MaxAttempts: 1})
	})

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
	assert.True(t, errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled),
		"expected deadline error, got %v", err)
}

func TestCacheHitAvoidsNetwork(t *testing.T) {
	rdw.SetClientConfig(rdw.ClientConfig{
		MaxAttempts: 1,
		CacheTTL:    time.Minute,
		CacheSize:   8,
	})
	t.Cleanup(func() {
		rdw.SetClientConfig(rdw.ClientConfig{MaxAttempts: 1})
	})

	rdw.ResetCache()

	ft := &flakyTransport{}
	client := &http.Client{Transport: ft}

	_, err := rdw.FetchBase(context.Background(), client, "AB12CD")
	require.NoError(t, err)

	_, err = rdw.FetchBase(context.Background(), client, "AB12CD")
	require.NoError(t, err)

	assert.Equal(t, int32(1), ft.calls.Load(), "second call should be served from cache")
}

func TestCacheDisabledByDefault(t *testing.T) {
	// Default test config disables cache (MaxAttempts:1, no CacheTTL).
	rdw.SetClientConfig(rdw.ClientConfig{MaxAttempts: 1})

	ft := &flakyTransport{}
	client := &http.Client{Transport: ft}

	_, err := rdw.FetchBase(context.Background(), client, "AB12CD")
	require.NoError(t, err)

	_, err = rdw.FetchBase(context.Background(), client, "AB12CD")
	require.NoError(t, err)

	assert.Equal(t, int32(2), ft.calls.Load())
}
