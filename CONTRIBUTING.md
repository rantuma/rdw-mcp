# Contributing

Thanks for taking the time to contribute! This document describes the
development workflow and the conventions this project follows.

## Prerequisites

- Go **1.26** or newer (matches `go.mod`)
- [`golangci-lint`](https://golangci-lint.run/usage/install/) (the version
  pinned in CI is the source of truth)
- Optional: [`goreleaser`](https://goreleaser.com) for testing release
  artefacts locally (`goreleaser release --snapshot --clean`)

## Local workflow

```sh
make build          # build the local binary
make test           # run all tests
make test-coverage  # run tests with the >=80% coverage gate
make lint           # run golangci-lint
```

Before opening a pull request, ensure `make test` and `make lint` both pass.

## Code conventions

- Strict typing throughout. Prefer typed `time.Duration` / `slog.Attr` over
  loose `interface{}`.
- Use `log/slog` for logging. Prefer `*Context` variants
  (`InfoContext`, `LogAttrs`) so request context propagates.
- Validate inputs at system boundaries (MCP tool input, HTTP requests). Do
  not double-validate internally.
- Add JSDoc-style Go comments to exported identifiers; let `revive` and
  `staticcheck` enforce the rest.
- Tests use `stretchr/testify` (`assert` for soft assertions, `require` when
  failures should stop the test).

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(rdw): add recall lookup tool
fix(transport): close response body on retry path
chore(deps): bump go-sdk to v1.7.0
```

Types we use: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`,
`build`, `perf`.

## Releasing

Releases are triggered manually from the GitHub Actions UI:

1. Go to **Actions → Release → Run workflow**.
2. Enter the version in `vX.Y.Z` form (e.g. `v3.1.0`).
3. The workflow creates and pushes the tag, then runs GoReleaser to build
   cross-platform binaries, publish multi-arch container images to GHCR,
   and create a GitHub Release whose notes are generated automatically
   from Conventional Commits since the previous tag.

No `CHANGELOG.md` is maintained in-tree — release notes live on the
GitHub Releases page.

## Reporting issues

When filing a bug, include:

- The `rdw-mcp --version` output (or commit hash if built from source)
- The transport in use (`stdio` or `--http`)
- A minimal reproduction (e.g. the kenteken queried and the observed output)
- Logs at `LogLevel=debug` if relevant
