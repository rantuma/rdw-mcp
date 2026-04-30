---
applyTo: "**"
---

# Copilot Instructions

This is an MCP (Model Context Protocol) server written in **Go** that exposes
tools, resources, and prompts for querying the Dutch RDW (Rijksdienst voor het
Wegverkeer) open-data vehicle registry.

## Project Layout

```
main.go                  # entry point, flag parsing, server lifecycle
mcp_register.go          # MCP tool / resource / prompt registration + handlers
config.go                # runtime configuration
internal/rdw/            # RDW API client, kenteken validation, formatting
internal/transport/      # stdio / streamable-HTTP MCP transports
.github/workflows/       # CI + release pipelines (call Makefile targets)
Makefile                 # canonical build / test / lint commands
.golangci.yml            # strict golangci-lint v2 configuration
```

Dependencies point inward: `internal/transport` and the root `main` package
may depend on `internal/rdw`; `internal/rdw` must not import the transport or
root packages.

## Coding Standards

Follow the same conventions as
[rantuma/hue-dial](https://github.com/rantuma/hue-dial):

- **Go 1.26+**, strict typing, no `interface{}` unless an SDK forces it.
- All linting goes through the strict `.golangci.yml`. New code must pass
  `make lint` without `//nolint` directives unless justified inline.
- Format with `gofumpt` (or `make fmt`); imports are grouped stdlib /
  third-party / local (`github.com/rantuma/rdw-mcp/...`).
- Use `log/slog` with `*Context` variants (`InfoContext`, `LogAttrs`) so
  request context propagates. Avoid the global default logger.
- Validate inputs at system boundaries (MCP tool input, HTTP requests). Do
  not double-validate internally.
- Errors: wrap with `fmt.Errorf("...: %w", err)`. Sentinel errors are named
  `ErrXxx`; error types are suffixed `Error`.
- **Never ignore an error.** Do not use `_ =` or `_, _ =` to discard error
  returns — handle, log, propagate, or in tests assert via
  `require.NoError`. Restructure code (e.g. extract values into locals,
  reuse the same `err` via `=`) rather than introducing alternative names.
- **Always name error variables `err`.** Do not introduce alternative names
  like `encErr`, `werr`, or `runErr` to avoid shadowing — restructure the
  scope or reuse `err` instead.
- HTTP: always pass `context.Context`, always close `resp.Body`.
- Tests use `stretchr/testify` — `require` for fatal preconditions, `assert`
  for soft checks. Run with `-race`. Prefer `t.Parallel()` and table-driven
  subtests. Test helpers stay in the same `_test.go` file as their
  consumers; do not split closely related tests across multiple files.
- Avoid global mutable state. Build-time identity (`version`, `commit`,
  `date`) is the only allowed `gochecknoglobals` exception, marked inline.

## CI / CD

CI and release pipelines invoke **Makefile targets** rather than inlining
shell. Add or extend a Make target instead of duplicating commands in YAML.
Available targets:

| Target                  | Purpose                                              |
| ----------------------- | ---------------------------------------------------- |
| `make build`            | Build the local `rdw-mcp` binary                     |
| `make build-all`        | Cross-compile for linux/darwin/windows × amd64/arm64 |
| `make verify`           | `go mod download` + `go mod verify`                  |
| `make test`             | Run all unit tests                                   |
| `make test-coverage`    | Race + coverage with the >=80% gate                  |
| `make test-integration` | Run `-tags=integration` tests against live RDW       |
| `make lint`             | Run `golangci-lint` (strict config)                  |
| `make fmt`              | `gofumpt -w .` (falls back to `gofmt`)               |
| `make vuln`             | Run `govulncheck ./...`                              |

## Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):
`feat(rdw): ...`, `fix(transport): ...`, `chore(deps): ...`. Common types:
`feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`, `build`, `perf`.
Use `!` or a `BREAKING CHANGE:` footer for major bumps.

## RDW Domain

- Base URL: `https://opendata.rdw.nl/resource/`
- All endpoints are public (no authentication).
- License plates ("kentekens") are normalized via `rdw.CleanKenteken` and
  validated against the official sidecode rules in `internal/rdw/kenteken.go`.
- Endpoints currently consumed (datasets):
  `m9d7-ebf2` (base), `8ys7-d773` (fuel), `3huj-srit` (axles),
  `vezc-m2t6` (body), `t49b-isb7` / `j9yg-7rg9` (recalls),
  `sgfe-77wx` (APK history), `a34c-vvps` (defects).

## References

- MCP Go SDK: https://github.com/modelcontextprotocol/go-sdk
- MCP spec & examples: https://modelcontextprotocol.io/llms-full.txt
- RDW Open Data: https://opendata.rdw.nl/
- Lint config source: https://github.com/maratori/golangci-lint-config
