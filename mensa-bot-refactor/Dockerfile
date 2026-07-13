# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Build stage.
#
# CGO is MANDATORY: gorm.io/driver/sqlite pulls in github.com/mattn/go-sqlite3,
# a cgo package. A CGO_ENABLED=0 build fails to link the sqlite driver, so the
# build stage must ship a C toolchain — the Debian-based golang image already
# bundles gcc, so no extra apt install is needed here.
# ---------------------------------------------------------------------------
FROM golang:1.25-bookworm AS build

WORKDIR /src

# Cache module downloads independently of the source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the single service binary (main package lives at the module root).
RUN CGO_ENABLED=1 GOFLAGS=-trimpath go build -o /out/mensa-bot .

# ---------------------------------------------------------------------------
# Runtime stage.
#
# debian:bookworm-slim is a currently-supported (non-EOL) base that carries the
# same glibc the cgo binary was linked against. ca-certificates is required for
# the outbound HTTPS calls to the Mensa API and Tolgee; curl backs the
# HEALTHCHECK below.
# ---------------------------------------------------------------------------
FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl \
	&& rm -rf /var/lib/apt/lists/*

# Run as a dedicated non-root user. /data is created and handed to that user so
# the SQLite DB and Tolgee cache remain writable when the process is not root.
RUN useradd --system --uid 10001 --user-group --home-dir /app --no-create-home mensa \
	&& mkdir -p /app /data \
	&& chown -R mensa:mensa /app /data

WORKDIR /app

COPY --from=build --chown=mensa:mensa /out/mensa-bot /app/mensa-bot

# The SQLite database lives on a volume so it survives container replacement.
# DB_PATH points inside it; override HTTP_ADDR/DB_PATH via the environment.
ENV DB_PATH=/data/mensa-telegram.db \
	TOLGEE_CACHE_PATH=/data/tolgee-cache.json \
	HTTP_ADDR=:8080
VOLUME ["/data"]

EXPOSE 8080

USER mensa

# Liveness against the inbound HTTP server's GET /health endpoint.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
	CMD curl -fsS http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/mensa-bot"]
