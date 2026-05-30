# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

An MCP (Model Context Protocol) server, built on the official
[`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk),
that exposes Dutch RDW open vehicle-registration data (looked up by `kenteken` /
license plate) to LLM clients. Data comes from the public RDW Socrata API at
`opendata.rdw.nl` — no auth, fully public open data.

## Commands

```sh
make build          # build local binary (./rdw-mcp), with version ldflags
make test           # go test ./...
make test-coverage  # race + coverage, fails under 80%
make test-integration   # hits the REAL RDW API; build tag `integration`
make lint           # golangci-lint (config is .golangci.yml — strict)
make fmt            # gofumpt (falls back to gofmt)
make vuln           # govulncheck
make licenses       # refresh ./licenses third-party texts

# Run a single test
go test ./internal/rdw -run TestValidateKenteken
go test -run TestHandleLookup .        # root package (package main)
```

`make lint` must pass — `.golangci.yml` is large and strict (revive, staticcheck,
gocritic, mnd, dupl, etc.). Existing code uses targeted `//nolint:<linter> //
reason` comments; follow that pattern rather than loosening the config.

## Running the server

```sh
rdw-mcp                       # stdio transport (default — Claude Desktop, Cursor)
rdw-mcp --http                # Streamable HTTP on port 3000
rdw-mcp --http --port=8080
rdw-mcp --version
```

## Architecture

Two layers: the **root package (`package main`)** is the MCP/server wiring; the
**`internal/` packages** are the reusable pieces it composes.

- **`main.go`** — entry point. Chooses stdio vs HTTP transport, builds the
  `*http.Client` (wrapped in `otelhttp.NewTransport`), sets the RDW client config
  (retries + cache) and User-Agent, wires telemetry, handles graceful shutdown.
- **`mcp_register.go`** — the MCP surface. `registerAll` registers all tools,
  the `rdw://kenteken/{plate}` resource template, and the `summarize_vehicle`
  prompt. Each tool is a per-section lookup (`rdw_vehicle_full`, `_basic`,
  `_technical`, `_fuel_emissions`, `_recalls`, `_apk_history`, `_defects`) plus a
  deprecated alias. Tool handlers return both human-readable text and a typed
  structured output struct.
- **`observability.go`** — `instrumentMiddleware` is an MCP receiving middleware
  that spans + records a duration histogram for *every* MCP method call (rate is
  derived from the histogram's `_count`, so there is no separate counter). Base
  labels (`mcp.method`, `mcp.status`, `mcp.tool`) are derived in `describeCall`;
  failures add a bounded `error.type` from `classifyError`. The duration
  histogram is shaped as a base-2 exponential (native) histogram via
  `callDurationView`, passed into `telemetry.Setup` from `main`.
- **`config.go`** — env-var config (`envConfig`). CLI flags override env. See
  Configuration below.
- **`internal/rdw/`** — the RDW domain layer: typed record structs, the HTTP
  client (`doRDWGet` with retry/backoff + in-memory TTL-LRU cache in
  `client.go`), `kenteken.go` validation against the 14 official sidecodes, and
  `format.go` report formatting. `FetchAllVehicleData` fans out to 7 RDW
  endpoints concurrently; a nil `Base` means "vehicle not found".
- **`internal/transport/`** — the HTTP transport: configurable `http.Server`
  with CORS, `/health`, `/ready` (probes RDW connectivity), and structured
  request-logging middleware. Knows nothing about MCP.
- **`internal/telemetry/`** — OpenTelemetry setup (`Setup`) for traces, metrics
  (incl. Go runtime metrics) and logs, plus the slog `LogHandler` that injects
  `trace_id`/`span_id` on the stderr path and `NewFanoutHandler` that lets `main`
  log to stderr *and* the OTLP `otelslog` bridge simultaneously.

### Key conventions and invariants

- **`registeredToolNames` (mcp_register.go) is the source of truth for tool
  names** and is deliberately coupled to telemetry: the `mcp.tool` metric label
  is collapsed to `"unknown"` for any name not in this set, so an untrusted
  client can't blow up metric cardinality. When you add/remove a tool, update
  both `registerAll` and `registeredToolNames`.
- **Tool errors vs Go errors:** handlers surface RDW/validation failures as a
  `CallToolResult{IsError: true}` (via `errorResult`) and return a `nil` Go
  error — note the intentional `//nolint:nilerr`. Returning a non-nil Go error
  is reserved for transport-level failures.
- **Validate kenteken only at the boundary** (`normalizeKenteken` /
  `ValidateKenteken`); internal code assumes cleaned, valid input.
- **`rdw.SetClientConfig` opts into retries + caching.** The zero value leaves
  the cache *disabled* — `main` enables it; tests stay deterministic by default.

### Telemetry is fail-open and off by default

With no `OTEL_*` endpoint configured, `telemetry.Setup` installs no-op providers
and the server runs exactly as before — observability can never take the process
down. Export is enabled purely by setting standard OTLP env vars, per signal:
the umbrella `OTEL_EXPORTER_OTLP_ENDPOINT` turns on all three, or
`OTEL_EXPORTER_OTLP_{TRACES,METRICS,LOGS}_ENDPOINT` turns on just that signal.
There are no flags. `OTEL_SDK_DISABLED=true` is honored as a standard operator
kill switch — it short-circuits `telemetry.Enabled()` regardless of endpoint
configuration. The W3C `traceparent` and `baggage` propagators are always
installed (even with export off), so the server joins a caller's distributed
trace and forwards baggage. Sampling uses standard `OTEL_TRACES_SAMPLER` / `_ARG`
(parsed by `newSampler`, since the base Go SDK doesn't read them itself).

Instrumentation scopes carry the build version via `WithInstrumentationVersion`,
so dashboards can split signals by deploy. The root package emits the MCP
middleware histogram `mcp.server.call.duration`; the `internal/rdw` package
emits three upstream-health signals — `rdw.client.request.duration` (per-endpoint
latency, native exponential histogram), `rdw.client.retry.attempts` (attempts
per logical call), and `rdw.client.cache.operations` (hit/miss counter, silent
when caching is disabled). `rdw.endpoint` is bounded via `endpointName` the same
way `mcp.tool` is via `registeredToolNames`. `instrumentMiddleware` is
panic-safe: a panic in a handler still ends the span and records a duration
sample tagged `error.type="panic"` before re-throwing.

## Configuration (env vars)

CLI flags override env. Port precedence: `RDW_MCP_PORT` > `PORT` (platform
convention) > default 3000.

- `RDW_MCP_PORT` / `PORT` — HTTP port
- `RDW_MCP_LOG_LEVEL` — debug | info | warn | error
- `RDW_MCP_CORS_ORIGINS` — comma-separated allow-list (default `*`)
- `RDW_MCP_HTTP_TIMEOUT` — per-request timeout (Go duration)
- standard `OTEL_*` — see telemetry section

## Conventions (from CONTRIBUTING.md)

- Conventional Commits (`feat`, `fix`, `chore`, `docs`, `refactor`, `test`,
  `ci`, `build`, `perf`), optionally scoped: `feat(rdw): ...`.
- **Comments explain *why*, never *what*.** Omit them entirely when the code is
  self-explanatory; otherwise keep to 2 lines max. Don't restate the function
  name or narrate the steps — only document non-obvious rationale, invariants, or
  gotchas. (`//nolint:` directives are exempt.)
- Use `log/slog` with the `*Context` variants (`InfoContext`, `LogAttrs`) so
  context/trace correlation propagates.
- Tests use `stretchr/testify` (`assert` soft, `require` to stop the test).
- Releases are manual via GitHub Actions (Actions → Release → Run workflow,
  `vX.Y.Z`); GoReleaser builds binaries + GHCR images. No in-tree CHANGELOG.
