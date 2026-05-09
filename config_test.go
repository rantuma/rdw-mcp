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

func TestGetEnvIntFromKeys(t *testing.T) {
	const (
		primary   = "RDW_MCP_TEST_PRIMARY"
		secondary = "RDW_MCP_TEST_SECONDARY"
	)

	tests := []struct {
		name      string
		primary   string
		secondary string
		setPrim   bool
		setSec    bool
		want      int
	}{
		{name: "neither set returns default", want: 99},
		{name: "only secondary set", secondary: "8080", setSec: true, want: 8080},
		{name: "only primary set", primary: "9000", setPrim: true, want: 9000},
		{
			name:    "primary wins over secondary",
			primary: "9000", secondary: "8080",
			setPrim: true, setSec: true,
			want: 9000,
		},
		{
			name:    "invalid primary falls through to secondary",
			primary: "abc", secondary: "8080",
			setPrim: true, setSec: true,
			want: 8080,
		},
		{
			name:    "all invalid falls back to default",
			primary: "abc", secondary: "xyz",
			setPrim: true, setSec: true,
			want: 99,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.Unsetenv(primary))
			require.NoError(t, os.Unsetenv(secondary))

			if tc.setPrim {
				t.Setenv(primary, tc.primary)
			}

			if tc.setSec {
				t.Setenv(secondary, tc.secondary)
			}

			assert.Equal(t, tc.want, getEnvIntFromKeys(99, primary, secondary))
		})
	}
}

func TestLoadEnvConfigPortPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		rdwPort  string
		port     string
		setRDW   bool
		setPort  bool
		wantPort int
	}{
		{name: "neither set uses default", wantPort: defaultPort},
		{name: "PORT used when RDW_MCP_PORT unset", port: "8080", setPort: true, wantPort: 8080},
		{
			name:    "RDW_MCP_PORT wins over PORT",
			rdwPort: "9000", port: "8080",
			setRDW: true, setPort: true,
			wantPort: 9000,
		},
		{name: "RDW_MCP_PORT alone", rdwPort: "9000", setRDW: true, wantPort: 9000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.Unsetenv("RDW_MCP_PORT"))
			require.NoError(t, os.Unsetenv("PORT"))

			if tc.setRDW {
				t.Setenv("RDW_MCP_PORT", tc.rdwPort)
			}

			if tc.setPort {
				t.Setenv("PORT", tc.port)
			}

			assert.Equal(t, tc.wantPort, loadEnvConfig().Port)
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
