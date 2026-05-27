# RDW MCP Server

[![CI](https://github.com/rantuma/rdw-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/rantuma/rdw-mcp/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/rantuma/rdw-mcp)](https://goreportcard.com/report/github.com/rantuma/rdw-mcp)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A [Model Context Protocol](https://modelcontextprotocol.io) server that exposes
the Dutch [RDW open vehicle registration data](https://opendata.rdw.nl) to
LLM-powered clients such as Claude Desktop, VS Code Copilot Chat, and any other
MCP-compatible host.

Built on the official [`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).

## Features

- Look up a Dutch license plate (`kenteken`) and receive a complete vehicle
  report covering registration, fuel & emissions, axles, and bodywork.
- Returns both human-readable text and structured JSON output for typed
  consumption by LLMs.
- Two transports out of the box: stdio (for desktop clients) and Streamable
  HTTP (for hosted deployments).
- No authentication required — RDW data is fully public open data.

## Quickstart

### Install

```sh
go install github.com/rantuma/rdw-mcp@latest
```

Or download a binary from the [releases page](https://github.com/rantuma/rdw-mcp/releases).

### Run

```sh
# stdio transport (default — for Claude Desktop, Cursor, etc.)
rdw-mcp

# HTTP transport (Streamable HTTP)
rdw-mcp --http
rdw-mcp --http --port=8080
```

## Client configuration

### Claude Desktop

Add the following to `~/Library/Application Support/Claude/claude_desktop_config.json`
(macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "rdw": {
      "command": "rdw-mcp"
    }
  }
}
```

### VS Code

Add to `.vscode/mcp.json`:

```json
{
  "servers": {
    "rdw": {
      "type": "stdio",
      "command": "rdw-mcp"
    }
  }
}
```

## Tools

| Name | Description |
| --- | --- |
| `rdw_vehicle_full` | Look up all available RDW data for a kenteken (base, fuel, axles, body). |
| `rdw-license-plate-lookup` | Deprecated alias of `rdw_vehicle_full` for back-compat. |

### Input

```json
{ "kenteken": "AB-12-CD" }
```

Hyphens, spaces, and case are normalised automatically.

### Output

The tool returns both unstructured text (a multi-section human-readable report)
and a structured payload:

```jsonc
{
  "kenteken": "AB12CD",
  "found": true,
  "base":  { /* RDW base registration */ },
  "fuel":  [ /* fuel & emissions records */ ],
  "axles": [ /* axle records */ ],
  "body":  [ /* body / carrosserie records */ ],
  "report": "COMPLETE RDW Database Information for AB12CD: …"
}
```

## HTTP endpoints

When running with `--http`, the server exposes:

- `POST /mcp` — Streamable HTTP MCP endpoint
- `GET  /health` — Liveness check (JSON status, server name, version)
- `GET  /ready` — Readiness check; verifies upstream RDW connectivity and
  responds `503` when RDW is unreachable

## Observability

The server is instrumented with [OpenTelemetry](https://opentelemetry.io). It is
**fail-open and off by default**: with no OTLP endpoint configured it installs
no-op exporters and runs exactly as before, so observability can never take the
process down. Export is enabled purely through the standard `OTEL_*` environment
variables — no flags.

### Enabling export

Set an OTLP/HTTP endpoint to turn on export. The umbrella variable enables both
signals; the signal-specific variables enable only their own:

```sh
# Both traces and metrics → e.g. an OpenTelemetry Collector or Grafana Cloud
export OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp.example.com
export OTEL_EXPORTER_OTLP_HEADERS="authorization=Basic <token>"

# Or enable a single signal independently
export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://otlp.example.com/v1/traces
export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=https://otlp.example.com/v1/metrics
```

All other standard OTLP knobs (`OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`,
`OTEL_EXPORTER_OTLP_PROTOCOL`, etc.) are honoured. `service.name` and
`service.version` default to the server's own identity but are overridable.

### What is emitted

- **Traces** — every MCP method call gets a span, and inbound/outbound HTTP is
  instrumented via `otelhttp`. The W3C `traceparent` header is always
  propagated (even with export disabled), so the server joins a caller's
  distributed trace and calls into RDW continue it.
- **Metrics** — emitted per call, tagged with `mcp.method`, `status`
  (`ok` / `error` / `tool_error`), and `mcp.tool` (for `tools/call` only).
  `mcp.tool` is constrained to the registered tool names; any other (untrusted)
  name is recorded as `unknown`, so the label can't be used to blow up metric
  cardinality:

  | Metric | Type | Unit | Description |
  | --- | --- | --- | --- |
  | `mcp.server.call.duration` | histogram | s | Duration of MCP method calls. Buckets are seconds-scaled. |
  | `mcp.server.calls` | counter | — | Count of MCP method calls. |

- **Logs** — structured logs go to stderr and are automatically decorated with
  `trace_id` / `span_id` when emitted within a span, for correlation in the
  backend. (Correlation requires the `*Context` logging methods.)

### Sampling

Traces use the standard
[`OTEL_TRACES_SAMPLER`](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/#general-sdk-configuration)
/ `OTEL_TRACES_SAMPLER_ARG` variables. The default is `parentbased_always_on`
(sample everything, honouring the parent's decision). Supported values:
`always_on`, `always_off`, `traceidratio`, `parentbased_always_on`,
`parentbased_always_off`, `parentbased_traceidratio`.

```sh
# Sample 10% of root traces (and follow the parent decision for children)
export OTEL_TRACES_SAMPLER=parentbased_traceidratio
export OTEL_TRACES_SAMPLER_ARG=0.1
```

## Development

```sh
make build          # build local binary
make test           # run tests
make test-coverage  # run tests with coverage gate (>=80%)
make lint           # run golangci-lint
make build-all      # cross-compile for linux/darwin/windows × amd64/arm64
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## Data source

All vehicle data comes from RDW (Rijksdienst voor het Wegverkeer)
[open data](https://opendata.rdw.nl). RDW updates the dataset daily and the
data is provided as-is; this server simply formats and forwards it.

## License

[MIT](LICENSE)
