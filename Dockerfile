# syntax=docker/dockerfile:1.7

# ---------- Build stage ----------
FROM golang:1.25-alpine AS build

ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

WORKDIR /src

# Cache modules.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build \
        -trimpath \
        -ldflags "-s -w \
            -X main.version=${VERSION} \
            -X main.commit=${COMMIT} \
            -X main.date=${DATE}" \
        -o /out/rdw-mcp \
        .

# ---------- Runtime stage ----------
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="rdw-mcp" \
      org.opencontainers.image.description="MCP server for Dutch RDW vehicle registration data" \
      org.opencontainers.image.source="https://github.com/rantuma/rdw-mcp" \
      org.opencontainers.image.licenses="MIT"

COPY --from=build /out/rdw-mcp /usr/local/bin/rdw-mcp

USER nonroot:nonroot
EXPOSE 3000

ENTRYPOINT ["/usr/local/bin/rdw-mcp"]
CMD ["--http", "--port=3000"]
