# syntax=docker/dockerfile:1.7
#
# Synapto — Telegram News Digest Assistant
# Multi-stage build:
#   1. spa   : build the SvelteKit SPA (frontend/) into static assets
#   2. go    : build a single static Go binary that embeds the SPA via //go:embed
#   3. run   : minimal Alpine runtime with ca-certificates + tzdata
#
# The build context is the repository root. The image is reproducible
# (no bind mounts, no host network) and runs as a non-root user.

ARG GO_VERSION=1.23
ARG NODE_VERSION=20

# ---- 1. SPA ----------------------------------------------------------------
FROM node:${NODE_VERSION}-alpine AS spa
WORKDIR /src/frontend

# Install dependencies in a cached layer.
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci

# Build the SPA. Output lands in /src/frontend/build.
COPY frontend/ ./
RUN npm run build

# ---- 2. Go binary ----------------------------------------------------------
FROM golang:${GO_VERSION}-alpine AS gobuild
WORKDIR /src/backend

# Pure-Go SQLite (modernc.org/sqlite) needs no CGo, so we can run with
# CGO_ENABLED=0 and a static binary. Caching the module download layer
# separately keeps rebuilds fast when only source files change.
RUN apk add --no-cache git ca-certificates
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy the rest of the backend source.
COPY backend/ ./

# Stage the built SPA into the //go:embed directory before compiling.
# The .gitkeep in the empty repo dir keeps the embed directive valid
# even when the SPA hasn't been built yet (e.g. during go vet in CI).
COPY --from=spa /src/frontend/build ./internal/adminapi/spa/

ARG VERSION=0.1.0-dev
ENV CGO_ENABLED=0 GOOS=linux
RUN go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/assistant \
    ./cmd/assistant

# ---- 3. Runtime ------------------------------------------------------------
FROM alpine:3.20
LABEL org.opencontainers.image.title="synapto-assistant" \
      org.opencontainers.image.description="Telegram News Digest Assistant" \
      org.opencontainers.image.source="https://github.com/synapto/assistant"

RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -S -g 10001 assistant && \
    adduser  -S -u 10001 -G assistant assistant && \
    mkdir -p /data && chown -R assistant:assistant /data

WORKDIR /app
COPY --from=gobuild /out/assistant /app/assistant
RUN chown assistant:assistant /app/assistant

USER assistant

# The default listen address is localhost-only. For Docker, the operator
# usually exposes the port via the host, in which case binding to all
# interfaces is appropriate. Override with ADMIN_LISTEN_ADDR.
ENV ADMIN_LISTEN_ADDR=0.0.0.0:8080 \
    DB_PATH=/data/assistant.db \
    LOG_LEVEL=info \
    TZ=UTC

EXPOSE 8080
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/api/health | grep -q '"status":"ok"' || exit 1

ENTRYPOINT ["/app/assistant"]
