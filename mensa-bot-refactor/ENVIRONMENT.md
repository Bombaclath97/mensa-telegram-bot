# Environment variables

Every variable the service reads, in one place. Copy `.env.example` to `.env`
and fill in the required ones. The service loads `.env` on start (via `godotenv`)
and otherwise falls back to the process environment.

TLS is intentionally **not** configured here: the service serves plain HTTP and
is meant to sit behind a reverse proxy that terminates TLS.

## Required

| Variable | Default | Description |
| --- | --- | --- |
| `BOT_TOKEN` | — | Telegram bot token from @BotFather. The process exits if unset. |
| `API_ENDPOINT` | — | Base URL of the Mensa Italia API used for membership verification and validity checks. |
| `API_BEARER_TOKEN` | — | Bearer token for the Mensa API. |
| `CALLME_BASE_URL` | — | Public origin of this service **plus** the callback path `/callme_api`. The Mensa app receives `{CALLME_BASE_URL}/{token}` and the service serves `POST /callme_api/{token}`; a mismatch makes every approval callback 404. |
| `ADMIN_TELEGRAM_IDS` | (empty) | Comma-separated Telegram user IDs flagged `is_bot_admin` on boot; the only users allowed to run `/manual_add`. Empty disables `/manual_add` for everyone. |

## HTTP server

| Variable | Default | Description |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Bind address for the inbound HTTP server: `GET /health` (liveness) and `POST /callme_api/{token}` (Mensa app approval webhook). |

## Storage (PostgreSQL)

The bot keeps **no state on disk** — it is stateless, and has no volume. Everything lives in
Postgres. The schema is auto-migrated on boot; foreign keys are enforced natively (the old
SQLite `_foreign_keys=on` DSN hack is gone).

Configure it **either** with one URL **or** with discrete parts. The URL wins if both are set.

| Variable | Default | Description |
| --- | --- | --- |
| `DATABASE_URL` | (empty) | Full DSN, e.g. `postgres://mensa:pw@postgres:5432/mensa?sslmode=disable`. Simplest for local dev. |
| `DB_HOST` | (empty) | Hostname. Required if `DATABASE_URL` is unset. |
| `DB_PORT` | `5432` | |
| `DB_USER` | (empty) | Required with `DB_HOST`. |
| `DB_PASSWORD` | (empty) | Escaped correctly even if it contains `@`, `/` or `:`. |
| `DB_NAME` | (empty) | Required with `DB_HOST`. |
| `DB_SSLMODE` | `disable` | The bot reaches Postgres over a private overlay network, not the internet. |

**In Swarm, prefer the discrete form with `DB_PASSWORD_FILE`.** A `DATABASE_URL` embeds the
password, so it would have to be duplicated into a second secret alongside the one Postgres
itself reads (`POSTGRES_PASSWORD_FILE`) — and the two would drift on the first rotation. With
`DB_PASSWORD_FILE`, both services read the *same* secret. See `DEPLOY.md`.

## Secrets (`*_FILE`)

**Any** variable can be supplied indirectly using the conventional Docker suffix: set
`X_FILE=/path` and the contents of that file become `X`. This is how Docker Swarm delivers
secrets, and it is resolved once at startup (`config.ExpandFileSecrets`) before anything reads
its configuration.

```
BOT_TOKEN_FILE=/run/secrets/mensa_bot_token
DB_PASSWORD_FILE=/run/secrets/mensa_db_password
API_BEARER_TOKEN_FILE=/run/secrets/mensa_api_bearer_token
```

Rules:

- Surrounding whitespace is trimmed. `echo` appends a newline, and a `BOT_TOKEN` ending in
  `\n` produces a 404 from Telegram that looks exactly like a wrong token.
- Setting both `X` and `X_FILE` is a **fatal error**, not a silent preference — guessing a
  winner is how a stale token overrides a rotated one.
- An unreadable or empty secret file is fatal. Continuing would fall back to an empty token
  and fail later, further away, with a worse message.

## Translations (Tolgee)

All optional — the CDN and on-disk cache tiers work with no API key, so a
translation outage never stops the bot (it degrades to rendering raw keys).

| Variable | Default | Description |
| --- | --- | --- |
| `TOLGEE_API_KEY` | (empty) | Tolgee project API key. |
| `TOLGEE_CACHE_PATH` | `./tolgee-cache.json` | On-disk fallback cache. Docker default: `/tmp/tolgee-cache.json` — writable and expendable, since losing it only costs a re-fetch from the CDN. It no longer defaults next to the database: there is no volume any more. |
| `TOLGEE_CDN_PROJECT_ID` | (built-in) | Overrides the CDN project ID for the CDN fallback tier. |
| `TOLGEE_CDN_URL` | (built-in) | Overrides the CDN base URL. |

## Timeouts

| Variable | Default | Description |
| --- | --- | --- |
| `CONVERSATION_TIMEOUT_MINUTES` | `5` | TTL of an in-progress registration / `/manual_add` wizard, re-armed on each step. |
| `APPROVAL_TIMEOUT_MINUTES` | `30` | TTL of a draft parked `AWAITING_APPROVAL` from the Mensa app. Approving happens in a separate app, so this is deliberately much longer than the wizard TTL and is **not** re-armed while locked — it must cover the whole receive-push → open-app → tap-approve round-trip on its own. |

## Logging

| Variable | Default | Description |
| --- | --- | --- |
| `LOG_LEVEL` | `info` | One of `debug`, `info`, `warn`, `error`. |

## Membership-expiry cleanup sweep

Ticker-driven and destructive (kicks + deletes expired members). It is **not**
exposed over HTTP. `CLEANUP_DRY_RUN` defaults to `true`, so out of the box the
sweep only logs what it would do — read those logs before setting it to `false`.

| Variable | Default | Description |
| --- | --- | --- |
| `CLEANUP_DRY_RUN` | `true` | When true, log intended removals without touching anyone. |
| `CLEANUP_INTERVAL_HOURS` | `24` | Interval between sweeps. |
| `CLEANUP_MAX_DELETIONS` | `10` | Circuit breaker: a sweep that would remove more than this aborts, touching nobody. |
| `CLEANUP_MAX_FRACTION` | `0.25` | Circuit breaker: abort if the sweep would remove more than this fraction of members. |
| `CLEANUP_FRACTION_MIN_MEMBERS` | `20` | Only apply the fraction breaker once the membership is at least this large. |
| `CLEANUP_API_CONCURRENCY` | `4` | Parallel Mensa API validity checks during a sweep. |
| `CLEANUP_TELEGRAM_DELAY_MS` | `100` | Delay between Telegram actions to stay within rate limits. |
| `CLEANUP_SKIP_BOT_ADMINS` | `true` | Never sweep users flagged `is_bot_admin`. |
