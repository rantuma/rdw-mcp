package main

import (
	"os"
	"testing"

	"github.com/rantuma/rdw-mcp/internal/rdw"
)

// TestMain disables retry/backoff and the response cache so handler tests run
// fast and remain hermetic.
func TestMain(m *testing.M) {
	rdw.SetClientConfig(rdw.ClientConfig{MaxAttempts: 1})

	os.Exit(m.Run())
}
