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
- `GET  /health` — Health check (JSON status, server name, version)

CORS is permissive (`*`) by default to ease local development.

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
