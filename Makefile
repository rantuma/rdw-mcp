.PHONY: build build-all clean test test-coverage test-integration lint fmt verify vuln licenses help

# Version info — overridable from CI / GoReleaser. For local dev they default to
# git-derived values when available so `rdw-mcp --version` works out of the box.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

GO_BUILD := go build -trimpath -ldflags '$(LDFLAGS)'

help: ## Show this help.
	@awk 'BEGIN{FS=":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the local binary.
	$(GO_BUILD) -o rdw-mcp .

build-all: ## Cross-compile for linux/darwin/windows × amd64/arm64.
	GOOS=linux   GOARCH=amd64 $(GO_BUILD) -o rdw-mcp-linux-amd64 .
	GOOS=linux   GOARCH=arm64 $(GO_BUILD) -o rdw-mcp-linux-arm64 .
	GOOS=darwin  GOARCH=amd64 $(GO_BUILD) -o rdw-mcp-darwin-amd64 .
	GOOS=darwin  GOARCH=arm64 $(GO_BUILD) -o rdw-mcp-darwin-arm64 .
	GOOS=windows GOARCH=amd64 $(GO_BUILD) -o rdw-mcp-windows-amd64.exe .

clean: ## Remove build artefacts and coverage files.
	rm -f rdw-mcp rdw-mcp-* coverage.out coverage.html

test: ## Run all tests.
	go test ./...

test-coverage: ## Run tests with coverage gate (>=80%).
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out
	@awk '/^total:/ { gsub(/%/, "", $$3); if ($$3 + 0 < 80) { print "Coverage below 80%: " $$3 "%"; exit 1 } }' coverage.out

test-integration: ## Run integration tests against the real RDW API.
	go test -tags=integration -count=1 -timeout=60s ./...

lint: ## Run golangci-lint.
	golangci-lint run ./...

fmt: ## Format Go sources with gofumpt (falls back to gofmt).
	@if command -v gofumpt >/dev/null 2>&1; then \
		gofumpt -w .; \
	else \
		echo "gofumpt not installed; falling back to gofmt"; \
		gofmt -s -w .; \
	fi

verify: ## Download and verify Go modules.
	go mod download
	go mod verify

vuln: ## Run govulncheck (installs it on demand).
	@command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

# Packages whose licenses go-licenses cannot detect (e.g. MIT-0). Verified
# manually — see the upstream LICENSE files referenced in licenses/.
LICENSE_IGNORE := github.com/segmentio/asm

licenses: ## Verify and refresh third-party license texts in ./licenses (installs go-licenses on demand).
	@command -v go-licenses >/dev/null 2>&1 || go install github.com/google/go-licenses@latest
	go-licenses check . --ignore=$(LICENSE_IGNORE)
	rm -rf licenses
	go-licenses save . --save_path=./licenses --ignore=$(LICENSE_IGNORE)
