# Deploying to Docker Swarm / Portainer

Read this once before the first deploy. `docker-stack.yml` assumes an image in GHCR, four
external secrets, an external Traefik network, and one labelled node — none of which it can
create for you.

## The one thing to understand first

**This service cannot scale. `replicas: 1` is a correctness constraint, not a capacity
setting.** Three independent reasons, any one of which is fatal on its own:

1. **Telegram allows one `getUpdates` poller per bot token.** A second replica gets
   `409 Conflict: terminated by other getUpdates request`, and the two instances kick each
   other out in a loop, forever.
2. **`model.Drafts` is an in-process map.** A registration started on replica A is invisible
   to replica B, so the Mensa app's callback 403s at random.
3. **`cleanup.Start` is an in-process ticker.** Two replicas means two independent
   *destructive* sweeps racing over the same rows.

Moving to Postgres did not change any of this. Do not raise `replicas`.

What Swarm *does* buy here: rolling updates, real secrets, restart policy, and Portainer
management. It does **not** buy high availability. See "The data pins the stack" below.

## If you only have the Portainer UI

Everything below is doable from the web interface — no SSH, no docker CLI. The UI paths:

| step | where in Portainer |
| ---- | ------------------ |
| 1. Label the data node | **Swarm → Nodes → _(pick the node)_ → Labels → Add label** — name `mensa-data`, value `true` |
| 2. Create the five secrets | **Secrets → Add secret** (×5, names exactly as below) |
| 3. Check the Traefik network | **Networks** — `traefik-public` must already be listed |
| 4. Deploy | **Stacks → Add stack → Repository** |

Notes for the UI route specifically:

- **Do step 1 first.** The stack constrains Postgres to `node.labels.mensa-data == true`. If no
  node carries that label the service never schedules — it sits `Pending` forever with "no
  suitable node", which reads like a broken stack rather than a missing label. Set it even on a
  single-node swarm.
- **Secret names must match exactly** (`mensa_bot_token`, `mensa_api_bearer_token`,
  `mensa_db_password`, `mensa_tg_api_hash`); the stack declares them `external`.
- **Paste secret values with no trailing newline.** Portainer sends the textarea contents
  verbatim, and a stray Enter at the end of `BOT_TOKEN` gives a 404 from Telegram that looks
  exactly like a wrong token. The bot trims whitespace defensively
  (`config.ExpandFileSecrets`), so this is covered — but don't rely on it.
- The image is **public**, so Portainer needs no registry credentials.

## Prerequisites

### 1. An image in GHCR

`docker stack deploy` **ignores `build:`**. Swarm can pull an image; it cannot build one.
`.github/workflows/ci.yml` publishes to `ghcr.io/bombaclath97/mensa-telegram-bot` on every
push to `main` and every `v*` tag.

The package is currently **public**, so no registry credentials are needed — Portainer and
Swarm can both pull it anonymously. Verify any time with:

```sh
docker manifest inspect ghcr.io/bombaclath97/mensa-telegram-bot:latest
```

If it is ever made private, add a registry credential to the swarm instead:

```sh
docker login ghcr.io -u <user>   # then deploy with --with-registry-auth
docker stack deploy -c docker-stack.yml --with-registry-auth mensa
```

Pin to a `sha-` tag rather than `latest` if you want a redeploy to be reproducible:

```sh
export MENSA_BOT_IMAGE=ghcr.io/bombaclath97/mensa-telegram-bot:sha-<commit>
```

### 2. Label the node that holds the data

```sh
docker node update --label-add mensa-data=true <node>
```

**This is not optional.** See below.

### 3. Create the secrets

```sh
printf '%s' '<bot token>'          | docker secret create mensa_bot_token -
printf '%s' '<mensa api bearer>'   | docker secret create mensa_api_bearer_token -
printf '%s' '<telegram api_hash>'  | docker secret create mensa_tg_api_hash -
openssl rand -base64 32 | tr -d '\n' | docker secret create mensa_db_password -
```

`TG_API_ID` (plain env var) and `mensa_tg_api_hash` come from <https://my.telegram.org>. They
are what let the bot SEE who is already inside a group: the Bot API has no method to list
members, so without them a bot added to a 500-member group sees none of them. They identify
the *application*, not a user — they grant no access to the account that registered them.

**Use `printf`, not `echo`.** `echo` appends a newline, and a `BOT_TOKEN` ending in `\n`
produces a 404 from Telegram that looks exactly like a wrong token. The bot trims whitespace
defensively (`config.ExpandFileSecrets`), but do not rely on that.

The bot and Postgres read **the same** `mensa_db_password` secret — the bot via
`DB_PASSWORD_FILE`, Postgres via its own `POSTGRES_PASSWORD_FILE`. That is why the bot takes
discrete `DB_*` variables rather than one `DATABASE_URL`: a URL would embed the password, so
the same value would exist in two secrets and drift the first time anyone rotated one.

Docker secrets are **immutable**. Rotating one means creating `mensa_bot_token_v2`, pointing
the stack at it, redeploying, then removing the old one.

### 4. The Traefik network

`traefik-public` must already exist and Traefik must be running with a cert resolver. The
stack only routes `Host(...) && PathPrefix("/callme_api")` — `/health` is deliberately not
public.

> If your `traefik-public` is an `internal` network, the bot will start, reach Postgres, and
> then silently fail every outbound call (Telegram, Mensa API, Tolgee). It gets its egress
> from that network, because `mensa-internal` has none by design.

### 5. Environment

Set these where you deploy (Portainer's stack env, or your shell):

| variable | required | notes |
| -------- | -------- | ----- |
| `MENSA_BOT_HOST` | yes | public hostname Traefik routes |
| `API_ENDPOINT` | yes | Mensa membership API base URL |
| `CALLME_BASE_URL` | yes | **public** origin + `/callme_api`; anything else 404s every approval |
| `ADMIN_TELEGRAM_IDS` | no | comma-separated; blank disables `/manual_add` for everyone |
| `MENSA_BOT_IMAGE` | no | defaults to `:latest` |
| `TRAEFIK_CERT_RESOLVER` | no | defaults to `letsencrypt` |
| `CLEANUP_DRY_RUN` | no | **defaults to `true`** — read below before changing |

## Deploy

```sh
docker stack deploy -c docker-stack.yml --with-registry-auth mensa
```

In Portainer: **Stacks → Add stack → Repository**, point it at this repo, set
`docker-stack.yml` as the compose path, and add the environment variables above.

## The data pins the stack

A Swarm named volume is **local to its node** and does **not** follow a rescheduled task.
Without the `node.labels.mensa-data == true` constraint, a drain or node failure moves
Postgres to another machine, where it finds an empty directory and cheerfully initialises a
**brand new empty database**. Nothing errors.

An empty database means the bot sees zero registered members — and **every member looks
unverified to the cutover kicker**. The circuit breakers would abort that sweep, which is
exactly what they are for, but the situation should never arise.

The honest trade: **that node dying is downtime, not failover.** Real HA needs managed
Postgres, not a cleverer constraint.

Back it up:

```sh
docker exec $(docker ps -qf name=mensa_postgres) pg_dump -U mensa mensa | gzip > mensa-$(date +%F).sql.gz
```

## Before you flip `CLEANUP_DRY_RUN`

The expiry sweep ships dry. It is destructive — it kicks members out of groups and deletes
rows. Do not flip it until:

- the destructive-path tests exist (`cleanupHandleGroup`, `DeleteMemberEverywhere`);
- `CLEANUP_MAX_DELETIONS` is set low (single digits) for the first live sweep;
- you have read one dry-run's audit log and recognise the names in it.

See `CUTOVER.md` for the group-takeover process, which is a separate and even more destructive
thing.

## Verifying a deploy

```sh
docker service ls
docker service ps mensa_mensa-bot --no-trunc     # why a task failed
docker service logs -f mensa_mensa-bot
```

A healthy boot logs `Database initialized successfully` with a **redacted** DSN
(`postgres://mensa:xxxxx@postgres:5432/mensa`), then `Bot started successfully`.

If you see `409 Conflict` in the logs, something else is polling the same token — another
deploy, a stale task, or someone's laptop.
