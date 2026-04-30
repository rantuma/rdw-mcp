//go:build integration

package rdw_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rantuma/rdw-mcp/internal/rdw"
)

// TestIntegrationFetchBase hits the real opendata.rdw.nl endpoint with the
// known plate TT601D. Run with:
//
//	make test-integration
func TestIntegrationFetchBase(t *testing.T) {
	t.Parallel()

	rdw.SetClientConfig(rdw.ClientConfig{
		MaxAttempts:    rdw.DefaultMaxAttempts,
		BaseBackoff:    rdw.DefaultBaseBackoff,
		MaxBackoff:     rdw.DefaultMaxBackoff,
		PerCallTimeout: 10 * time.Second,
	})

	client := &http.Client{Timeout: 15 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := rdw.FetchBase(ctx, client, "TT601D")
	require.NoError(t, err)
	require.NotEmpty(t, results, "expected at least one record for TT601D")
	assert.NotEmpty(t, results[0].Kenteken)
}
