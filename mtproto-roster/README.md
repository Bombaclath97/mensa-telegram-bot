# mtproto-roster

A spike. It answers one question:

> Can our bot enumerate the **full member list** of a Mensa supergroup?

Everything about the cutover plan hangs on the answer, so it gets proven before it gets
designed around.

## Why the question matters

`group_associations` is only written when a **join request is approved**
(`handlers/join_request_handler.go`) or for **admins** (`handlers/group_handler.go`).
Members who were already sitting in the groups never issue a join request, so the bot has
no record of them. That breaks three things at once:

- the cleanup sweep deletes an expired member's row while kicking them from **nothing**,
  leaving them in the group forever and now untracked;
- `/elimina` removes nobody from any group, despite promising it does;
- `/profilo` shows "no groups" for the entire membership.

## Why the Bot API can't answer it

The bot speaks the **Bot API** (`go-telegram/bot`), which has no way to list members:

| method                  | gives you            |
| ----------------------- | -------------------- |
| `getChatMember`         | one user you already know |
| `getChatAdministrators` | admins only          |
| `getChatMemberCount`    | an integer           |

There is no enumeration endpoint. This is deliberate, for anti-spam reasons.

## Why MTProto might

**MTProto** — the protocol the official clients and TDLib speak — is a *different API*, and
it does have the method: [`channels.getParticipants`](https://core.telegram.org/method/channels.getParticipants).
The docs state plainly:

> "Both users and bots can use this method"

gated on `CHAT_ADMIN_REQUIRED`. Our bot is already an admin everywhere, so on paper this
works. On paper is not good enough — Telegram has tightened member enumeration before.

### The access_hash trap

`channels.getParticipants` wants an `InputChannel{ChannelID, AccessHash}`, and a bot never
learns an `access_hash` — the Bot API doesn't expose one. That looks fatal. It isn't;
[core.telegram.org/api/peers](https://core.telegram.org/api/peers) says:

> "Zero access hash: equal to `0`, **must be used by bots** when only a min access hash (or
> no access hash) is available locally, but a full access hash is required."

So `AccessHash: 0` is the *mandated* value here, not a hack — but **it is not sufficient on
its own**, and that cost a debugging round to discover:

> `access_hash=0` is accepted by `channels.getChannels` (which resolves the channel **and
> hands back its real access_hash**), but **rejected** by `channels.getParticipants`, which
> returns `CHANNEL_INVALID`.

The zero hash is a key that opens exactly one door: the one that gives you the real key. So
every call is a **two-step** — resolve with `0`, then enumerate with the real hash. See
`resolveChannel` in `main.go`.

This matters because `CHANNEL_INVALID` from `getParticipants` looks *identical* to "Telegram
forbids bots from enumerating members" — the false negative that would have sunk the whole
cutover plan. It doesn't. It just means the resolve step was skipped.

Same trap in the id format: the Bot API's `-1001234567890` is MTProto channel `1234567890`
(positive — MTProto keeps users/chats/channels in separate namespaces, so the `-100` prefix
and the sign are pure Bot API packing). Feeding the raw form in produces the same misleading
error. Both are handled; the conversion is unit-tested.

## RESULT (2026-07-16, test supergroup, 4 members)

```
[ok] resolved channel 4345944461 ("Test 1"), got its real access_hash
[ok] this bot IS an administrator of the target chat
     rights: ... BanUsers:true InviteUsers:true ...
server reports Count = 4
  page 0   offset=0      got=4    unique users so far=4
ENUMERATION WORKS
```

4 of 4 enumerated, matching the server's own `Count`. `BanUsers:true` is also confirmed —
that's the right the grace-period kick needs.

**Still unproven, and deliberately so:** a 4-member throwaway cannot tell you whether a real
Mensa group has `participants_hidden` set, nor exercise the ~10k `Recent` ceiling. One
counting run against one real group closes both. Until then this is "works in principle",
not "works on our data".

## Running it

You need three things:

1. **`TG_API_ID` / `TG_API_HASH`** from <https://my.telegram.org>. Interactive login on a
   personal Telegram account — they are **not** derivable from the bot token, and MTProto
   bot auth (`importBotAuthorization`) requires them *in addition* to it.
2. **`BOT_TOKEN`** — the same token the bot uses.
3. **`TARGET_CHAT_ID`** — Bot API format, the `-100…` number already in the bot's logs.

```sh
TG_API_ID=1234567 \
TG_API_HASH=abcdef... \
BOT_TOKEN=123:ABC... \
TARGET_CHAT_ID=-1001234567890 \
    go run .
```

**Point it at a throwaway test supergroup first** (create one, add the bot, promote it to
admin, add a couple of accounts). It's read-only, but there's no reason to pull real
members' IDs and names onto a terminal just to test plumbing.

## Reading the result

| output | means |
| ------ | ----- |
| `ENUMERATION WORKS` | A one-shot cutover tool can snapshot the full membership, seed `group_associations`, and the grace-period kick can reach existing members. |
| `PARTIAL` | Fewer members came back than the server's own `Count`. The list may be capped or partly hidden — **don't** plan a kick policy on this. |
| `ENUMERATION RETURNED NOTHING` | Not proven. Check the bot really is an admin of that chat. |
| `CHAT_ADMIN_REQUIRED` | Bot isn't admin there. Does **not** disprove anything — promote and retry. |
| `CHANNEL_INVALID` | Either the id conversion is wrong, or `access_hash=0` was rejected for this bot — which *would* be a real finding, contradicting the docs above. |

A test supergroup can't tell you whether a **real** group hides its member list
(`participants_hidden`). One counting run against one real group settles that.

## If it works

This module grows into the one-shot cutover tool: enumerate each group once, snapshot the
member IDs, seed the roster and `group_associations`. MTProto stays in the *migration path
only* — the bot itself keeps speaking the Bot API, unchanged, with `chat_member` updates
keeping the roster live afterwards.

It's a sibling module (like `mock-mensa-api`) precisely so `gotd/td` never enters the bot's
own dependency tree.
