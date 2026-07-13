# Tolgee Translation Keys

Every translation key the finished bot renders, built by grepping every
`tolgee.T(...)` call site (and the two `keyOwnerExpired` / `keyExpiredMembership`
constants in `cleanup/cleanup.go`). The bot uses **62 keys**.

**Status: all 62 keys exist in the Tolgee project, translated into all 10
project languages** (620 translations). Code and Tolgee are in sync — no key the
bot renders is missing, and no key in the project is unused. The bot renders the
raw dotted key for anything missing, so keep it that way: if you add a
`tolgee.T(...)` call, create the key before deploying.

The **Status** column records each key's origin:

- **existing** — was already used by the legacy bot.
- **NEW** — added by this refactor's newly-ported or brand-new flows. The 31 NEW
  keys were created in Tolgee on 2026-07-14 (the `manualadd.*` family was never
  in the project despite the legacy bot referencing it — the legacy `/manual_add`
  sent no prompts, so the gap went unnoticed).

> **Button labels carry no emoji.** The code prepends the icon itself — e.g.
> `"✅ " + tolgee.T("telegrambot.deletecommand.confirmbutton", lang)` — so the
> translations for `general.cancel`, `profile.email`, `profile.membershipnumber`,
> `profile.submit` and `deletecommand.confirmbutton` are plain text. Adding an
> emoji to those in Tolgee produces a double icon.

## General

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.general.error` | existing | many handlers | Generic "something went wrong" message | — |
| `telegrambot.general.cancel` | **NEW** | keyboard_renderer, delete_handler | "Cancel" button label | — |
| `telegrambot.general.forwardedhidden` | existing | main (forwarded lookup) | Sender of a forwarded message has hidden their account | — |
| `telegrambot.general.forwardedregistered` | existing | main (forwarded lookup) | Forwarded sender is a registered member | `{name}`, `{lastName}` |
| `telegrambot.general.forwardednotregistered` | existing | main (forwarded lookup) | Forwarded sender is not registered | — |

## Start (`/start`)

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.startcommand.notregistered` | existing | start_handler | Greeting for an unregistered user | — |
| `telegrambot.startcommand.alreadyregistered` | existing | start_handler | Greeting for a registered user | `{name}` |

## Profile (`/profilo`)

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.profilecommand.initiateprofileregister` | existing | profile_handler | "Let's start registering your profile" | — |
| `telegrambot.profilecommand.showprofile` | existing | profile_handler | The user's profile block | `{name}`, `{surname}`, `{email}` |
| `telegrambot.profilecommand.showgroups` | existing | profile_handler | The user's group list | `{groupList}` |
| `telegrambot.profilecommand.isadmin` | **NEW** | profile_handler | Suffix on groups where the user is admin, e.g. "(admin)" | — |

## Profile registration keyboard

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.profile.email` | **NEW** | keyboard_renderer | "Email" button label | — |
| `telegrambot.profile.membershipnumber` | **NEW** | keyboard_renderer | "Membership number" button label | — |
| `telegrambot.profile.submit` | **NEW** | keyboard_renderer | "Submit" button label | — |

## Registration conversation

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.conversation.alreadyinprogress` | **NEW** | profile_handler | A wizard is already running | — |
| `telegrambot.conversation.sessionexpired` | **NEW** | profile_handler, callme_handler | The registration session expired | — |
| `telegrambot.conversation.requestemail` | **NEW** | profile_handler | Prompt for the @mensa.it email | — |
| `telegrambot.conversation.requestmemberid` | existing | profile_handler | Prompt for the membership number | — |
| `telegrambot.conversation.requestlastname` | existing | (name step after approval) | Prompt for the last name | `{name}` |
| `telegrambot.conversation.fieldsmissing` | **NEW** | profile_handler | Submit attempted with missing fields | — |
| `telegrambot.conversation.notvalidmemberid` | existing | profile_handler | The membership number is not a valid integer | — |
| `telegrambot.conversation.emailnotvalid` | existing | profile_handler | The email format is invalid | — |
| `telegrambot.conversation.emailalreadyregistered` | existing | profile_handler | The email is already registered | — |
| `telegrambot.conversation.creationsuccess` | existing | profile_handler | Profile created successfully | — |
| `telegrambot.conversation.awaitappinteraction` | existing | profile_handler | "Open the Mensa app to confirm" | — |
| `telegrambot.conversation.associationnonexistent` | existing | profile_handler | The (email, number) pair is not a real member | — |
| `telegrambot.conversation.approvedapproval` | existing | callme_handler | The app user ACCEPTED the verification | — |
| `telegrambot.conversation.rejectedapproval` | existing | callme_handler | The app user REJECTED the verification | — |

## Cancel (`/cancel`)

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.cancelcommand.cancelled` | existing | cancel_handler, profile_handler, delete_handler | "Action cancelled" | — |

## Delete (`/elimina`)

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.deletecommand.deleteprofile` | existing | delete_handler | Confirm-before-delete prompt | `{name}` |
| `telegrambot.deletecommand.confirmbutton` | **NEW** | delete_handler | "Confirm deletion" button label | — |
| `telegrambot.deletecommand.deleted` | existing | delete_handler | Profile deleted confirmation | — |
| `telegrambot.deletecommand.ownerdeleted` | existing | delete_handler | Posted in a group whose owner deleted their profile (owners can't be kicked) | — |

## App (`/app`)

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.appcommand.sendapp` | existing | app_handler | Mensa Italia app download links | — |

## Language (`/lang`) — NEW feature

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.lang.prompt` | **NEW** | lang_handler | "Choose your language" | — |
| `telegrambot.lang.changed` | **NEW** | lang_handler | Language changed confirmation | `{language}` |

## Command menu (`SetMyCommands`) — all NEW

Localized descriptions published to the Telegram command menu, once per language.
Legacy hardcoded these in Italian; the refactor sources them from Tolgee, so all
seven are NEW. `/manual_add` is deliberately absent (admin-only, unadvertised).

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.commands.start` | **NEW** | main (publishCommands) | Menu description for `/start` | — |
| `telegrambot.commands.profilo` | **NEW** | main (publishCommands) | Menu description for `/profilo` | — |
| `telegrambot.commands.approva` | **NEW** | main (publishCommands) | Menu description for `/approva` | — |
| `telegrambot.commands.app` | **NEW** | main (publishCommands) | Menu description for `/app` | — |
| `telegrambot.commands.elimina` | **NEW** | main (publishCommands) | Menu description for `/elimina` | — |
| `telegrambot.commands.lang` | **NEW** | main (publishCommands) | Menu description for `/lang` | — |
| `telegrambot.commands.cancel` | **NEW** | main (publishCommands) | Menu description for `/cancel` | — |

## Group management (join requests, `/approva`, bot joining a group)

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.welcome.invitetojoin` | existing | join_request_handler | DM to an UNREGISTERED user who requested to join a group | `{name}` |
| `telegrambot.welcome.approvedtogroup` | existing | join_request_handler | DM confirming a join request was approved | `{groupName}` |
| `telegrambot.welcome.botjoined` | existing | group_handler | Posted in a group when the bot is added | — |
| `telegrambot.approvecommand.approved` | existing | approve_handler | Buffered join requests approved | — |
| `telegrambot.approvecommand.norequesttoapprove` | existing | approve_handler | Nothing pending to approve | — |

## Admin manual add (`/manual_add`, hidden)

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.manualadd.requestuserid` | **NEW** | manual_add_handler | Prompt for the target's Telegram user ID | — |
| `telegrambot.manualadd.notvaliduserid` | existing | manual_add_handler | The typed Telegram user ID is not valid | — |
| `telegrambot.manualadd.useralreadyregistered` | **NEW** | manual_add_handler | That Telegram account is already registered | — |
| `telegrambot.manualadd.requestemail` | existing | manual_add_handler | Prompt for the target's email (no `@mensa.it` required) | — |
| `telegrambot.manualadd.notvalidemail` | **NEW** | manual_add_handler | The typed email is not a valid address | — |
| `telegrambot.manualadd.emailalreadyregistered` | **NEW** | manual_add_handler | That email is already registered | — |
| `telegrambot.manualadd.requestmemberid` | existing | manual_add_handler | Prompt for the membership number | — |
| `telegrambot.manualadd.notvalidmemberid` | existing | manual_add_handler | The typed membership number is not valid | — |
| `telegrambot.manualadd.requestname` | existing | manual_add_handler | Prompt for the first name | — |
| `telegrambot.manualadd.requestsurname` | existing | manual_add_handler | Prompt for the last name | — |
| `telegrambot.manualadd.success` | existing | manual_add_handler | Confirmation to the ADMIN that the member was created | — |
| `telegrambot.manualadd.registerednotice` | **NEW** | manual_add_handler | Courtesy DM to the newly registered international member | — |

## Cleanup sweep (membership expiry)

| Key | Status | Where used | What it should say | Params |
|-----|--------|------------|--------------------|--------|
| `telegrambot.cleanup.expiredmembership` | existing | cleanup (`keyExpiredMembership`) | DM to a member whose membership expired, before removal | — |
| `telegrambot.cleanup.ownerexpired` | existing | cleanup (`keyOwnerExpired`) | Posted in a group whose OWNER expired (owners can't be kicked) | — |

---

## NEW keys created in Tolgee (2026-07-14)

All 31 are live in all 10 languages. No action needed — listed for the record:

```
telegrambot.general.cancel
telegrambot.profilecommand.isadmin
telegrambot.profile.email
telegrambot.profile.membershipnumber
telegrambot.profile.submit
telegrambot.conversation.alreadyinprogress
telegrambot.conversation.sessionexpired
telegrambot.conversation.requestemail
telegrambot.conversation.fieldsmissing
telegrambot.deletecommand.confirmbutton
telegrambot.lang.prompt
telegrambot.lang.changed
telegrambot.commands.start
telegrambot.commands.profilo
telegrambot.commands.approva
telegrambot.commands.app
telegrambot.commands.elimina
telegrambot.commands.lang
telegrambot.commands.cancel
telegrambot.manualadd.requestuserid
telegrambot.manualadd.notvaliduserid
telegrambot.manualadd.useralreadyregistered
telegrambot.manualadd.requestemail
telegrambot.manualadd.notvalidemail
telegrambot.manualadd.emailalreadyregistered
telegrambot.manualadd.requestmemberid
telegrambot.manualadd.notvalidmemberid
telegrambot.manualadd.requestname
telegrambot.manualadd.requestsurname
telegrambot.manualadd.success
telegrambot.manualadd.registerednotice
```

Two orphaned keys were **deleted** from the project: `telegrambot.deletecommand.askagain`
(the refactor uses `deletecommand.confirmbutton`) and `telegrambot.welcome.message`.

`telegrambot.deletecommand.deleteprofile` was **rewritten** in all 10 languages: it
used to tell users to type the phrase `Sono sicuro di ciò che faccio`, which the
refactor's inline Confirm/Cancel buttons made a no-op. It now points at the buttons.

---

## Parameter syntax

Parameters are interpolated with `{parameterName}`. For example
`"Hello {name}, welcome back!"` called as:

```go
tolgee.T("key", "en", map[string]string{"name": "Mario"})
```

renders `Hello Mario, welcome back!`.

## Supported languages

The Tolgee project defines **10** languages, and the bot loads whatever the project
reports at boot — so this list is driven by Tolgee, not by the code. Every key is
translated into all 10.

| Code | Language          |
|------|-------------------|
| `it` | Italian (base)    |
| `en` | English           |
| `es` | Spanish           |
| `de` | German            |
| `fr` | French            |
| `pt` | Portuguese        |
| `ja` | Japanese          |
| `zh` | Chinese           |
| `hi` | Hindi             |
| `bn` | Bangla            |

> Note: without `TOLGEE_API_KEY` the bot cannot fetch the language list and falls
> back to the CDN with a default tag set of `it/en/es/de` only. Set the API key in
> production so all 10 languages load.
