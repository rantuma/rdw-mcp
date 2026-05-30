package telemetry_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rantuma/rdw-mcp/internal/telemetry"
)

func TestNewFanoutHandlerSingleIsUnwrapped(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, nil)
	assert.Same(t, inner, telemetry.NewFanoutHandler(inner),
		"a single handler should be returned unwrapped")
}

func TestFanoutHandlerWritesToAll(t *testing.T) {
	var bufA, bufB bytes.Buffer
	log := slog.New(telemetry.NewFanoutHandler(
		slog.NewTextHandler(&bufA, nil),
		slog.NewTextHandler(&bufB, nil),
	))

	log.Info("hello", "k", "v")

	for name, buf := range map[string]*bytes.Buffer{"a": &bufA, "b": &bufB} {
		assert.Contains(t, buf.String(), "hello", "destination %s missing message", name)
		assert.Contains(t, buf.String(), "k=v", "destination %s missing attribute", name)
	}
}

func TestFanoutHandlerWithAttrsAndGroup(t *testing.T) {
	var bufA, bufB bytes.Buffer
	base := telemetry.NewFanoutHandler(
		slog.NewTextHandler(&bufA, nil),
		slog.NewTextHandler(&bufB, nil),
	)

	// Derived loggers (WithAttrs via With, and WithGroup) must keep fanning out to
	// every destination.
	slog.New(base).With("component", "test").WithGroup("g").Info("derived", "k", "v")

	for _, buf := range []*bytes.Buffer{&bufA, &bufB} {
		assert.Contains(t, buf.String(), "component=test")
		assert.Contains(t, buf.String(), "g.k=v", "grouped attribute should fan out")
	}
}

func TestFanoutHandlerEnabled(t *testing.T) {
	var buf bytes.Buffer
	// One child only enabled at Error, one at Debug: the fan-out is enabled at
	// Info because at least one child handles it.
	handler := telemetry.NewFanoutHandler(
		slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}),
		slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	)

	require.True(t, handler.Enabled(context.Background(), slog.LevelInfo))
}
