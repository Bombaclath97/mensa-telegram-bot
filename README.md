# Mensa Bot Telegram

A Telegram bot that gates the Mensa Italia Telegram groups. It lets a Mensa
member prove their membership once, then auto-approves their join requests to
every group the bot administers, keeps a member's profile, and periodically
removes members whose membership has lapsed.

The bot supports Italian (default), English, Spanish and German; user-facing
strings come from [Tolgee](https://tolgee.io), with a CDN and on-disk cache
fallback so a translation outage never takes the bot down.

## Architecture

This is a **single Go service** (module `github.com/Bombaclath97/mensa-telegram-bot`,
one binary). It replaces the former three-part deployment (`mensa-bot` +
`mensa-telegram-crud` + `mensa-shared-models`), all of which have been retired.

The one process runs two things side by side under a shared, signal-cancelled
context (SIGINT/SIGTERM → graceful shutdown):

- **Telegram long-polling** — commands, callbacks, chat-join-request gating and
  group membership events.
- **An inbound HTTP server** (`HTTP_ADDR`, default `:8080`) exposing:
  - `GET /health` — liveness probe (used by the Docker/compose healthcheck).
  - `POST /callme_api/{token}` — the callback the official Mensa app calls to
    approve or reject an identity-verification request.

Persistence is **PostgreSQL accessed directly via GORM** (`gorm.io/driver/postgres`,
pure Go). There is no separate CRUD service and no HTTP/DB layer between the bot
and its data. The schema is auto-migrated on boot and foreign keys are enforced
natively.

The bot itself is **stateless** — it keeps nothing on disk. That is deliberate:
it was SQLite, which pinned the process to one machine's filesystem and made
Docker Swarm actively dangerous (a named volume is local to its node and does not
follow a rescheduled task, so the bot would come up against an empty database).
Postgres also drops the cgo dependency, which is why the image is now a 27 MB
distroless static binary instead of a Debian base with a C toolchain behind it.

Note this does **not** make the bot scalable: `replicas` must be exactly 1.
Telegram permits one `getUpdates` poller per token, and both the draft store and
the cleanup ticker are in-process. See [`DEPLOY.md`](DEPLOY.md).

TLS is **not** handled by the service: it serves plain HTTP and is meant to sit
behind a reverse proxy (nginx, Caddy, Traefik, …) that terminates TLS and
forwards both the webhook path and the health check to `HTTP_ADDR`.

```
                 ┌────────────────────────────────────┐
   Telegram  ◄──►│  mensa-bot (single Go binary)      │
  (long poll)    │    • bot handlers  ─┐              │       ┌────────────┐
                 │    • http server   ─┼─► GORM ──────┼──────►│  Postgres  │
 reverse proxy   │    • cleanup ticker ┘              │       │  (volume)  │
  ──HTTP──►:8080 │                                    │       └────────────┘
 (/callme_api,   │    outbound ─► Mensa API, Tolgee   │        the only state,
  /health)       └────────────────────────────────────┘        pinned to a node
                          stateless: no volume
```

## Registration & verification flow

Membership is proven by a round-trip through the official Mensa app; the bot
never trusts a self-declared email/number on its own.

1. A user opens a private chat and runs `/profilo` (or `/start`) to begin the
   registration wizard.
2. The wizard collects the user's **@mensa.it email** and **membership number**.
3. On submit the bot calls the Mensa API (`POST {API_ENDPOINT}/request`) with the
   email, membership number and a one-time callback URL
   (`{CALLME_BASE_URL}/{token}`). A 200 means the pair is a real member and a
   push notification was sent to that member's Mensa app. The draft is locked in
   an `AWAITING_APPROVAL` state (TTL `APPROVAL_TIMEOUT_MINUTES`).
4. The member opens the Mensa app and approves (or rejects). The app calls back
   `POST /callme_api/{token}` with `{"accepted": <bool>}`. The token must match
   the locked draft on **both** branches, so no third party can approve or abort
   someone else's registration.
5. On approval the bot collects the member's real first and last name, writes the
   verified user row, and the account is now a recognized member.
6. From then on, any **join request** to a group the bot administers is
   auto-approved for that member and the group association is recorded. If an
   unregistered user requests to join, the bot DMs them to register first and
   buffers the request; **registration itself drains the buffer**, so those
   requests are approved the moment they finish. `/approva` re-runs the drain by
   hand if it is ever needed.
7. Registration also probes every known group for that member and records the
   groups they were *already* in. Nothing else can discover those: a member who
   was in the group before the bot arrived never issues a join request, and the
   Bot API cannot list a group's members. Until this ran, such members were
   invisible to `/elimina`, to `/profilo`'s group list, and to the cleanup
   sweep — which would delete their row while kicking them from nothing.

International Mensa members without an `@mensa.it` address are added by a bot
admin via the hidden `/manual_add` command, which bypasses the email/app check.

A ticker-driven **cleanup sweep** (interval `CLEANUP_INTERVAL_HOURS`) checks each
member against `POST {API_ENDPOINT}/valid`; expired members are warned, demoted /
kicked from groups and deleted. The sweep ships **dry-run by default**
(`CLEANUP_DRY_RUN=true`) and has circuit breakers (`CLEANUP_MAX_DELETIONS`,
`CLEANUP_MAX_FRACTION`) so a misconfiguration cannot mass-remove the membership.

## Commands

Published to the Telegram command menu (per language) via `SetMyCommands`:

| Command    | What it does                                                        |
| ---------- | ------------------------------------------------------------------- |
| `/start`   | Greeting; tells you whether you are registered.                     |
| `/profilo` | Show your profile / groups, or start the registration wizard.       |
| `/approva` | Approve your buffered join requests once you are registered.        |
| `/app`     | Send the Mensa Italia app download links.                           |
| `/elimina` | Delete your profile and remove you from every Mensa group.          |
| `/lang`    | Choose your language (it / en / es / de).                           |
| `/cancel`  | Abort an in-progress wizard.                                        |

Four more commands are **admin-only** and deliberately not advertised in the menu. A
non-admin gets no reply at all. Admins are seeded from `ADMIN_TELEGRAM_IDS`.

| Command          | What it does                                                          |
| ---------------- | --------------------------------------------------------------------- |
| `/manual_add`    | Register an international member without an `@mensa.it` address.     |
| `/sweep_preview` | Run a dry-run cleanup sweep now and report who would be removed.      |
| `/pending`       | List group removals `/elimina` could not finish yet.                  |
| `/groups`        | List every group, whether the bot is admin there, and its members.    |

The same admins receive Telegram alerts when something needs a human: a tripped circuit
breaker, a failed sweep, an unhealthy Mensa API, a removal the bot gave up on, or the bot
being removed from a group or losing its admin rights there. See
[`DEPLOY.md`](DEPLOY.md#admin-alerts).

## Configuration

All configuration is via environment variables. See
[`mensa-bot-refactor/ENVIRONMENT.md`](mensa-bot-refactor/ENVIRONMENT.md) for the
exhaustive list and [`mensa-bot-refactor/.env.example`](mensa-bot-refactor/.env.example)
for a template to copy. The essentials:

| Variable             | Required | Description                                            |
| -------------------- | -------- | ------------------------------------------------------ |
| `BOT_TOKEN`          | yes      | Telegram bot token from [@BotFather](https://t.me/BotFather). |
| `API_ENDPOINT`       | yes      | Base URL of the Mensa Italia membership API.           |
| `API_BEARER_TOKEN`   | yes      | Bearer token for that API.                             |
| `CALLME_BASE_URL`    | yes      | Public origin of this service + `/callme_api`.         |
| `ADMIN_TELEGRAM_IDS` | no       | Comma-separated Telegram IDs flagged as bot admins.    |
| `DATABASE_URL`       | yes*     | Postgres DSN. *Or the discrete `DB_HOST`/`DB_USER`/`DB_PASSWORD`/`DB_NAME` form. |
| `HTTP_ADDR`          | no       | Bind address (default `:8080`).                        |

## Running

### Docker Compose (recommended)

1. Copy the env template and fill in the required values:

   ```sh
   cp mensa-bot-refactor/.env.example mensa-bot-refactor/.env
   $EDITOR mensa-bot-refactor/.env
   ```

2. Build and start the single service:

   ```sh
   docker compose up -d --build
   ```

Postgres data lives on the named `mensa-pgdata` volume, so it survives container
replacement. The bot has no volume — it is stateless. The `mensa-bot` service
publishes port `8080`; point your reverse proxy at it and forward `/callme_api/…`.

For **Docker Swarm / Portainer**, use [`docker-stack.yml`](docker-stack.yml) and read
[`DEPLOY.md`](DEPLOY.md) first — `docker stack deploy` ignores `build:`, so it needs
a published image, and the data pins the stack to one labelled node.

### Locally (development)

Requires Go 1.25+ and a reachable Postgres. No C toolchain is needed any more —
the build is `CGO_ENABLED=0` and fully static.

```sh
docker compose up -d postgres        # or point DATABASE_URL at your own
cd mensa-bot-refactor
cp .env.example .env                 # then fill it in
go run .
```

The test suite starts its own throwaway Postgres via testcontainers, so it needs a
running Docker daemon; it skips rather than fails without one.

```sh
go test ./...
```

## Translations

Translation keys are documented in
[`mensa-bot-refactor/TOLGEE_KEYS.md`](mensa-bot-refactor/TOLGEE_KEYS.md), which
marks the keys that are new in this refactor and must exist in the Tolgee project
(in it / en / es / de) — the bot renders a raw dotted key for anything missing.
