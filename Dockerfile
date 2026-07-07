# ============================================================
#  sqlite-server — Multi-stage Dockerfile
#  Produces a minimal scratch image (< 10 MB).
#
#  Build:
#    docker build -t sqlite-server:latest .
#
#  Run:
#    docker run -p 5432:5432 -v $(pwd)/data:/data \
#               sqlite-server:latest /data/myapp.db
#
#  With TLS:
#    docker run -p 5432:5432 \
#               -v $(pwd)/data:/data \
#               -v $(pwd)/certs:/certs \
#               sqlite-server:latest \
#               --ssl-cert /certs/server.crt \
#               --ssl-key  /certs/server.key \
#               /data/myapp.db
# ============================================================

# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

# Build arguments (injected by Makefile / CI).
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Cache dependency download layer separately for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source.
COPY . .

# Build a completely static binary (no CGO, no dynamic linking).
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags="-s -w \
        -X main.Version=${VERSION} \
        -X main.Commit=${COMMIT} \
        -X main.BuildDate=${BUILD_DATE}" \
      -o /sqlite-server \
      ./cmd/sqlite-server

# Verify the binary.
RUN /sqlite-server version

# ── Stage 2: Minimal runtime ──────────────────────────────────────────────────
FROM scratch

# Copy only what we need.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo                 /usr/share/zoneinfo
COPY --from=builder /sqlite-server                      /sqlite-server

# Persistent storage for the SQLite database file.
VOLUME ["/data"]

# PostgreSQL wire protocol default port.
EXPOSE 5432

# Default entrypoint — database path is the first positional argument.
ENTRYPOINT ["/sqlite-server"]
CMD ["/data/database.db"]

# ── Labels (OCI) ──────────────────────────────────────────────────────────────
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title       = "sqlite-server"
LABEL org.opencontainers.image.description = "SQLite over the PostgreSQL wire protocol"
LABEL org.opencontainers.image.version     = "${VERSION}"
LABEL org.opencontainers.image.revision    = "${COMMIT}"
LABEL org.opencontainers.image.created     = "${BUILD_DATE}"
LABEL org.opencontainers.image.source      = "https://github.com/sqlite-server/sqlite-server"
LABEL org.opencontainers.image.licenses    = "MIT"
