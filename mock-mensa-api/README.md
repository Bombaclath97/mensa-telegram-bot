# mock-mensa-api

A stand-in for the Mensa Italia membership API, so the bot's registration flow can be run
end to end without the real service (and without touching production data).

It plays **both halves** of the real interaction:

1. The bot asks it to verify someone — `POST /v1/request`
2. It calls the bot back, **impersonating the official Mensa app** — `POST {callme_url}`

That second half is the point. The app callback is the only thing that proves a member's
identity, so without a server that performs it, registration can never complete.

```
mensa-bot  ──POST /v1/request (callme_url=http://mensa-bot:8080/callme_api/<token>)──▶  mock-mensa-api
mensa-bot  ◀─POST /callme_api/<token>  {"accepted": true}────────────────────────────  mock-mensa-api
```

## Run it

```sh
docker compose -f compose.yaml -f compose.dev.yaml up --build
```

The dev overlay points the bot at the mock (`API_ENDPOINT`) and points the mock's callback
back at the bot (`CALLME_BASE_URL`). Both use **compose service names**, not `localhost` —
inside the mock's container, `localhost` is the mock.

> **Never run this overlay against the production bot token.**

## Seeded members

| Email | Number | Membership |
|---|---|---|
| `mario.rossi@mensa.it` | `12345` | active |
| `anna.bianchi@mensa.it` | `67890` | **expired** (exercises the cleanup sweep) |

Any other pair returns `404`, which the bot reports as "you are not a member" — so that
path stays testable. Set `MOCK_ACCEPT_ANY=true` to accept anything instead.

## End-to-end: register in Telegram

1. `/profilo` in a private chat with the bot.
2. Tap **✉️ Email** → send `mario.rossi@mensa.it`
3. Tap **🎫 Numero di tessera** → send `12345`
4. Tap **✅ Invia**. The bot calls the mock; the mock calls the bot back with
   `{"accepted": true}` after `MOCK_CALLBACK_DELAY` (2s).
5. The bot DMs you that the request was approved and asks for your first name, then your
   last name. Only then is the user row written.

Watch it happen:

```sh
docker compose -f compose.yaml -f compose.dev.yaml logs -f mock-mensa-api mensa-bot
```

## Approval modes

`MOCK_APPROVE_MODE` (or flip it at runtime):

| Mode | Behaviour |
|---|---|
| `auto` (default) | Approves every request after the delay — the happy path. |
| `reject` | Rejects every request, as if the member tapped "no" in the app. |
| `manual` | Fires nothing. You approve by hand — use this to test the approval timeout. |

```sh
curl -X POST localhost:8081/_control/mode/reject     # next registration gets rejected
curl -X POST localhost:8081/_control/mode/manual     # nothing fires automatically
curl localhost:8081/_control/state                   # see members + pending tokens
curl -X POST localhost:8081/_control/approve/<token> # fire the callback yourself
curl -X POST localhost:8081/_control/reject/<token>
```

In `manual` mode, take the token from `/_control/state` (it is `<telegramID>_<15 chars>`).

## Testing the cleanup sweep

`anna.bianchi@mensa.it` is seeded as **expired**, so `POST /v1/valid` reports
`is_membership_active: false` for member `67890`. Register her, then run the sweep with
`CLEANUP_DRY_RUN=false` to see the removal path. The sweep is dry-run by default and has a
circuit breaker — see `mensa-bot-refactor/ENVIRONMENT.md`.

Change membership state at runtime:

```sh
curl -X POST localhost:8081/_control/members -H 'Content-Type: application/json' -d '[
  {"email":"mario.rossi@mensa.it","member_number":12345,"name":"Mario Rossi","active":false}
]'
```

## Config

| Variable | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:8081` | Bind address |
| `MOCK_APPROVE_MODE` | `auto` | `auto` / `reject` / `manual` |
| `MOCK_CALLBACK_DELAY` | `2s` | Delay before the fake app answers |
| `MOCK_MEMBERS` | see above | `email:number:name:active\|expired`, comma separated |
| `MOCK_ACCEPT_ANY` | `false` | Treat any pair as a real member |

## Endpoints

**Mensa API (what the bot calls)**
- `POST /v1/request` — form: `email`, `member_id`, `callme_url`. `200` = real member, push sent. `404` = no such member.
- `POST /v1/valid` — form: `member_id`. Returns `{"username","name","is_membership_active"}`.

**Control plane (for tests)**
- `GET /_control/state`, `POST /_control/mode/{auto|reject|manual}`,
  `POST /_control/approve/{token}`, `POST /_control/reject/{token}`, `POST /_control/members`
- `GET /health`
