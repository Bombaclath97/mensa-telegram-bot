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

## MTProto (group member enumeration)

The Bot API **cannot list a group's members** — no such method exists. MTProto, the protocol
the official clients speak, can. Without these the bot still runs, but a bot added to a group
that already holds people sees **none of them**: they stay invisible to `/elimina`, to
`/profilo`'s group list, and to the cleanup sweep. It logs a warning on every promotion.

| Variable | Default | Description |
| --- | --- | --- |
| `TG_API_ID` | (empty) | From <https://my.telegram.org>. Identifies the *application*, not a user. |
| `TG_API_HASH` | (empty) | Ditto. Prefer `TG_API_HASH_FILE` in Swarm. |

Required **in addition** to `BOT_TOKEN`: MTProto bot auth is
`importBotAuthorization(api_id, api_hash, bot_token)`. Getting them is an interactive login on
a personal Telegram account, but the resulting pair is not a session and grants no access to
that account's messages or contacts.

Enumeration runs when the bot is **promoted to administrator** in a group (`getParticipants`
returns `CHAT_ADMIN_REQUIRED` to a plain member). The MTProto connection is opened on demand
and closed immediately after; it coexists with the Bot API long-poll on the same token.

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

Translations are loaded from the public Tolgee **CDN** (no API key) and cached on
disk, so a translation outage never stops the bot (it degrades to rendering raw
keys). The authenticated export API is intentionally not used — a key pointing at
the wrong project silently serves mismatched keys, rendering every string raw.

| Variable | Default | Description |
| --- | --- | --- |
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

## Group-sync backfill

`SyncMemberGroups` runs once per member, at registration, and stamps
`users.groups_synced_at` only if **every** row in `groups` could be probed. A single
unprobeable row — most often a chat id left stale after Telegram upgraded a basic group
to a supergroup — therefore fails the sweep for every member, and nothing ever retries
them. Unstamped members are deferred by the cleanup sweep indefinitely.

The bot now records upgrades as they happen (`ChatMigrationHandler`) and self-heals on the
`MigrateError` it gets back, so this should not recur. `GROUP_SYNC_BACKFILL` is for draining
the backlog left behind by an occurrence that already happened.

| Variable | Default | Description |
| --- | --- | --- |
| `GROUP_SYNC_BACKFILL` | `false` | When `true`, re-probe every member with `groups_synced_at IS NULL` once at startup and stamp the ones that complete. |

Set it for a single boot, confirm `group sync backfill: finished` reports `failed: 0`, then
unset it. Members that still fail are left unstamped, which is the safe direction. If the
backfill keeps finding work on every boot, a row in `groups` is still unprobeable — fix that
rather than leaving the backfill on.

Find the affected members with:

```sql
SELECT telegram_id, member_number FROM users WHERE groups_synced_at IS NULL;
```
