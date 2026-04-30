package main

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultHTTPTimeout is the default per-request HTTP timeout when
// RDW_MCP_HTTP_TIMEOUT is not set.
const defaultHTTPTimeout = 10 * time.Second

type (
	// envConfig holds runtime configuration sourced from environment variables.
	// CLI flags take precedence over env values.
	envConfig struct {
		Port           int
		LogLevel       slog.Level
		AllowedOrigins []string
		HTTPTimeout    time.Duration
	}
)

func loadEnvConfig() envConfig {
	return envConfig{
		Port:           getEnvInt("RDW_MCP_PORT", defaultPort),
		LogLevel:       parseLogLevel(os.Getenv("RDW_MCP_LOG_LEVEL")),
		AllowedOrigins: getEnvStringSlice("RDW_MCP_CORS_ORIGINS", []string{"*"}),
		HTTPTimeout:    getEnvDuration("RDW_MCP_HTTP_TIMEOUT", defaultHTTPTimeout),
	}
}

func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}

	return n
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}

	return d
}

func getEnvStringSlice(key string, def []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}

	parts := strings.Split(v, ",")

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	if len(out) == 0 {
		return def
	}

	return out
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "", "info":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}
