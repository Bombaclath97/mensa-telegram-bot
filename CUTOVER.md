# Onboarding a populated group

How the bot takes over a Mensa group that is **already full of people**, and removes the ones
who never verify.

> **This document previously called it a "one-time cutover". That was wrong.** A bot being
> added to a populated group is not a migration that happens once — it is a **recurring
> product event**. Add the bot to a group with 500 people in it and all 500 are invisible,
> today, tomorrow, and every time it happens again. So enumeration is core runtime behaviour
> and lives **in the bot** (`mtproto/`), triggered by the promotion event. It is not a
> migration tool, and MTProto is not confined to a migration path.
>
> Everything about the *member-facing* flow below (the timeline, the deep link, the
> exemptions, the kick) is unchanged. Only the framing and the trigger were wrong.

Everything below is a design contract, agreed before implementation. If the code and this
document disagree, one of them is a bug.

## The problem

`group_associations` — the table that records who is in which group — is only ever written
in two places:

| writer | covers |
| ------ | ------ |
| `handlers/join_request_handler.go` | someone whose **join request** was approved |
| `handlers/group_handler.go` (`groupSyncAdmins`) | group **admins** only |

People who were already sitting in the group before the bot arrived never issue a join
request. So the bot has no record of them, and three things silently break:

- the cleanup sweep deletes an expired member's row while kicking them from **nothing**,
  leaving them in the group forever and now untracked (`cleanup/cleanup.go:508` guards the
  *error* branch with exactly this reasoning — but an *empty* list is not an error);
- `/elimina` removes nobody from any group, despite promising it does;
- `/profilo` shows "no groups" for the entire membership.

## The two populations

This is the load-bearing distinction. **They need different mechanisms, and both live side by
side permanently** — it is not one-then-the-other.

| | pending join request? | can the bot DM them? | handled by |
| --- | --- | --- | --- |
| **Newcomer** (at the door) | yes | **yes** — `ChatJoinRequest.user_chat_id` grants a 5-minute window | the existing join-request gate |
| **Already inside** when the bot arrives | no | **no** — no request, no window | enumeration + this document |

A bot cannot normally start a conversation; the user must press Start first. The join-request
window is the *only* exception, and it is granted by the act of requesting to join. Legacy
members did that years ago, if ever — so for them there is no window and no DM.

**The only channel that reaches a legacy member is a message in the group itself.** Every
decision below follows from that one fact.

## Timeline

One cohort per group. The clock starts at that group's snapshot, so everyone in the group
shares a single deadline and a single announcement.

### Day 0 — snapshot, triggered by the bot being PROMOTED

Enumerate the group over MTProto (`mtproto/`, in the bot), classify every member, write roster
rows with a shared deadline of +7 days.

**The trigger is `promoted`, not `joined`** (`handlers/group_handler.go`).
`channels.getParticipants` returns `CHAT_ADMIN_REQUIRED` to a plain member, and a bot is very
often added as a member and promoted minutes later. Enumerating on `joined` would fail for the
common case and leave everyone invisible forever. Telegram tells us the exact moment the right
is granted, so we wait for it rather than retry blindly. A bot added *directly* as an admin
satisfies both branches and is covered.

Verified by experiment: an MTProto session and the Bot API's `getUpdates` long-poll run
**concurrently on the same bot token** with no conflict — 12/12 polls returned `ok=True`
during a full enumeration. No 409, no session eviction. The connection is opened on demand and
closed straight after; enumeration happens a handful of times ever.

**Pre-flight, and abort loudly on failure:**

- `can_view_participants == false` → **stop**. A hidden member list means a partial snapshot,
  and a partial snapshot means innocent people get classified unprofiled and kicked. Never
  seed from an incomplete enumeration.
- enumerated count != `participants_count` → **stop**, same reasoning.

A loud abort is fine. A silent under-count evicts real members.

### Day 0 — announcement (names nobody)

Posted and pinned in the group:

> 🔒 **This group is now restricted to verified Mensa Italia members.**
> To stay, verify your membership by **{date}**. It takes a minute, and verifying once
> covers every Mensa group you're in.
> **[Verify now →]({deeplink})**
> Anyone unverified by then will be removed — you can rejoin any time once you verify.

### Day 3 — reminder

The same message, still naming nobody. Everyone gets a quiet chance to comply before anyone
is named.

### Days 5 and 6 — straggler mentions

Now with names, **batched into one message** (per-person messages would hit group rate limits
and read as spam):

> ⏳ **{n} days left.** Still unverified: @alice, @bob, Carla, Dario
> **[Verify now →]({deeplink})**

Members with no username are mentioned inline via `tg://user?id=<id>`, which still fires their
notification. That is what reaches people the Bot API otherwise cannot touch at all.

### Day 7 — kick

`telegram_bot.KickUser` — ban then immediately unban, so re-entry works. A plain ban would
mean *"the user will not be able to return to the chat on their own using invite links, unless
unbanned first"*; the unban leaves them free to come back the moment they verify.

**The first real kick is gated:** day 7 produces an audit log of exactly who *would* be
removed, and stops. A human reads it, checks the names against what they know, and only then
is it flipped live. A classification bug that survives seven days of warnings still gets
caught here, before it evicts anyone.

## The deep link is the whole design

`t.me/<bot>?start=verify_<groupID>`

This is the one mechanism that turns an unreachable person into a reachable one:

1. they tap it in the group announcement;
2. Telegram opens a private chat with a **Start** button;
3. they press it — **the bot can now DM them, permanently**, and knows which group sent them;
4. it runs the registration wizard and can send a personal countdown from then on.

Everything good happens after that one tap. The announcement's only real job is to earn it —
which is why it leads with the link and not with the threat.

On successful registration: probe `getChatMember` across the known groups, backfill
`group_associations`, mark the roster row, cancel the kick. That is the same seam as the
missing `/approva` auto-drain — one function, both fixes.

## Exemptions

The kicker never touches:

| class | why |
| ----- | --- |
| group **owner** | cannot be kicked at all; `cleanupHandleGroup` already warns the group instead |
| group **admins** | demoting-then-kicking a group admin is never the intent |
| other **bots** | no Mensa profile by definition; all would be evicted on day 7 |
| **bot admins** (`ADMIN_TELEGRAM_IDS`) | the expiry sweep already has `CLEANUP_SKIP_BOT_ADMINS=true` for exactly this — don't let a misconfiguration evict the operators |
| **anonymous admins** | post as the group identity, cannot be resolved to a profile, would look unprofiled |
| **deleted accounts** | no profile by definition; drop silently, never "warn" then kick |

## Safety

Reuse the expiry sweep's rails wholesale — they are written, tested, and were built for
exactly this class of mistake:

- `DRY_RUN` defaults **true**;
- circuit breakers on absolute count **and** fraction-of-checked;
- Telegram throttle between destructive calls;
- skip bot admins.

**Roll out per-group, smallest group first.** Independent cohorts mean a mistake costs one
group, not the whole membership. One synchronised deadline across every group would expose
all of them to the same bug on the same day.

## Unreachable members

Someone with notifications off, who never opens the group, and never taps the link **will be
kicked on day 7**. This is deliberate. The alternative — only kicking people the bot can prove
it reached — means the people who ignore everything stay forever, which defeats the policy.

The kick is recoverable by design: they verify, request to join, and the existing gate
auto-approves them. If they ever contact the bot afterwards, it explains what happened and how
to return.

## Still to build

- roster table (`group_id`, `user_id`, `first_seen_at`, `deadline`, `state`, `notified_at`)
- `groups_synced_at` on `User` — distinguishes *genuinely in no groups* (safe to delete) from
  *never probed* (deleting orphans them). Plain fail-closed-on-empty would leak rows forever
  for the legitimately groupless.
- `/start` deep-link payload handling (`verify_<groupID>`)
- registration-time association backfill + `/approva` auto-drain (same seam)
- new Tolgee keys for every string above, in it / en / es / de — the bot renders a raw dotted
  key for anything missing, so these must exist in the project before the first announcement.

## Open

- The enumeration is proven on a 4-member test supergroup only. One counting run against one
  real Mensa group is needed to confirm size handling. See `mtproto-roster/README.md`.

## Not open

- **Hidden member lists.** Mensa group rules do not permit hiding the participant list, so
  `participants_hidden` is treated as an invariant (always false). The
  `can_view_participants` pre-flight above stays anyway — not as an investigation, but as a
  cheap guard against an admin flipping the setting by accident. It costs one field read and
  turns a silent partial snapshot into a loud abort.
