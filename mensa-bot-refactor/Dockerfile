# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Build stage.
#
# CGO_ENABLED=0 yields a fully static binary. This was NOT possible while the bot
# used SQLite: gorm.io/driver/sqlite pulls in github.com/mattn/go-sqlite3, a cgo
# package, which forced a C toolchain into this stage and a matching glibc into
# the runtime. The Postgres driver (jackc/pgx) is pure Go, so both constraints
# are gone.
# ---------------------------------------------------------------------------
FROM golang:1.25-bookworm AS build

WORKDIR /src

# Cache module downloads independently of the source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# -trimpath keeps local build paths out of the binary; -s -w drop the symbol
# table and DWARF, which are dead weight in a container image.
RUN CGO_ENABLED=0 GOOS=linux go build \
	-trimpath \
	-ldflags='-s -w' \
	-o /out/mensa-bot .

# ---------------------------------------------------------------------------
# Runtime stage.
#
# A static binary needs no distribution underneath it. distroless/static ships
# ca-certificates (required for the outbound HTTPS to the Mensa API and Tolgee)
# and tzdata, and essentially nothing else: no shell, no package manager, no
# libc. Almost nothing to patch, and very little to exploit.
#
# The HEALTHCHECK cannot shell out to curl the way it used to: there is no curl
# and no shell to run it in. Instead the binary probes ITSELF (`-healthcheck`,
# see healthcheck.go), which needs no distro and cannot drift from the endpoint
# it checks, since both are compiled from the same source.
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

# The nonroot user (uid 65532) is baked into the distroless image, so there is no
# useradd step. There is no VOLUME either: with Postgres the bot keeps no state on
# disk. The Tolgee fallback cache is the sole exception and defaults to /tmp —
# writable, and expendable, since losing it only costs a re-fetch.
ENV TOLGEE_CACHE_PATH=/tmp/tolgee-cache.json \
	HTTP_ADDR=:8080

COPY --from=build /out/mensa-bot /mensa-bot

EXPOSE 8080

USER nonroot:nonroot

# Exec form: there is no shell to expand a string form.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
	CMD ["/mensa-bot", "-healthcheck"]

ENTRYPOINT ["/mensa-bot"]
