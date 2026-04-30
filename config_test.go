package main

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name  string
		set   string
		value string
		def   int
		want  int
	}{
		{"unset uses default", "", "", 42, 42},
		{"valid int parsed", "set", "8080", 3000, 8080},
		{"invalid int falls back", "set", "abc", 5, 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key := "RDW_MCP_TEST_INT"

			if tc.set == "" {
				require.NoError(t, os.Unsetenv(key))
			} else {
				t.Setenv(key, tc.value)
			}

			assert.Equal(t, tc.want, getEnvInt(key, tc.def))
		})
	}
}

func TestGetEnvDuration(t *testing.T) {
	tests := []struct {
		name  string
		set   string
		value string
		def   time.Duration
		want  time.Duration
	}{
		{"unset uses default", "", "", 5 * time.Second, 5 * time.Second},
		{"valid duration parsed", "set", "30s", time.Second, 30 * time.Second},
		{"invalid duration falls back", "set", "abc", time.Second, time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key := "RDW_MCP_TEST_DUR"

			if tc.set == "" {
				require.NoError(t, os.Unsetenv(key))
			} else {
				t.Setenv(key, tc.value)
			}

			assert.Equal(t, tc.want, getEnvDuration(key, tc.def))
		})
	}
}

func TestGetEnvStringSlice(t *testing.T) {
	tests := []struct {
		name  string
		set   string
		value string
		def   []string
		want  []string
	}{
		{"unset uses default", "", "", []string{"*"}, []string{"*"}},
		{
			"single value",
			"set", "https://a.com",
			[]string{"*"},
			[]string{"https://a.com"},
		},
		{
			"comma split with whitespace",
			"set", "https://a.com, https://b.com ,https://c.com",
			[]string{"*"},
			[]string{"https://a.com", "https://b.com", "https://c.com"},
		},
		{
			"only commas falls back to default",
			"set", " , , ",
			[]string{"x"},
			[]string{"x"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key := "RDW_MCP_TEST_LIST"

			if tc.set == "" {
				require.NoError(t, os.Unsetenv(key))
			} else {
				t.Setenv(key, tc.value)
			}

			assert.Equal(t, tc.want, getEnvStringSlice(key, tc.def))
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"chatty", slog.LevelInfo},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, parseLogLevel(tc.in))
		})
	}
}
