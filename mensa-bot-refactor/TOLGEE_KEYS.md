# Tolgee Translation Keys

This document lists all translation keys used in the Mensa Telegram Bot refactor. Each key includes a description and any parameters that need to be interpolated.

## General Keys

| Key                                          | Description                                                               | Parameters             |
|----------------------------------------------|---------------------------------------------------------------------------|------------------------|
| `telegrambot.general.error`                  | Generic error message shown when something goes wrong                     | None                   |
| `telegrambot.general.cancel`                 | "Cancel" button text used in various keyboards                            | None                   |
| `telegrambot.general.forwardedhidden`        | Message shown when a forwarded message's sender has hidden their identity | None                   |
| `telegrambot.general.forwardedregistered`    | Message shown when a forwarded message's sender is registered             | `{name}`, `{lastName}` |
| `telegrambot.general.forwardednotregistered` | Message shown when a forwarded message's sender is not registered         | None                   |

## Start Command (`/start`)

| Key                                          | Description                               | Parameters |
|----------------------------------------------|-------------------------------------------|------------|
| `telegrambot.startcommand.notregistered`     | Welcome message for unregistered users    | None       |
| `telegrambot.startcommand.alreadyregistered` | Welcome back message for registered users | `{name}`   |

## Profile Command (`/profilo`)

| Key                                                  | Description                                                       | Parameters                       |
|------------------------------------------------------|-------------------------------------------------------------------|----------------------------------|
| `telegrambot.profilecommand.initiateprofileregister` | Message shown when starting the registration wizard               | None                             |
| `telegrambot.profilecommand.showprofile`             | Message displaying user's profile information                     | `{name}`, `{surname}`, `{email}` |
| `telegrambot.profilecommand.showgroups`              | Message displaying user's group memberships                       | `{groupList}`                    |
| `telegrambot.profilecommand.isadmin`                 | Suffix shown next to groups where user is admin (e.g., "(admin)") | None                             |

## Profile Keyboard Buttons

| Key                                    | Description                                                      | Parameters |
|----------------------------------------|------------------------------------------------------------------|------------|
| `telegrambot.profile.email`            | "Email" button text in profile registration keyboard             | None       |
| `telegrambot.profile.membershipnumber` | "Membership Number" button text in profile registration keyboard | None       |
| `telegrambot.profile.submit`           | "Submit" button text in profile registration keyboard            | None       |

## Conversation / Registration Flow

| Key                                               | Description                                                                          | Parameters |
|---------------------------------------------------|--------------------------------------------------------------------------------------|------------|
| `telegrambot.conversation.alreadyinprogress`      | Message shown when user tries to start registration while one is already in progress | None       |
| `telegrambot.conversation.sessionexpired`         | Message shown when user's registration session has expired                           | None       |
| `telegrambot.conversation.requestemail`           | Prompt asking user to enter their Mensa email                                        | None       |
| `telegrambot.conversation.requestmemberid`        | Prompt asking user to enter their membership number                                  | None       |
| `telegrambot.conversation.fieldsmissing`          | Error shown when user tries to submit with incomplete fields                         | None       |
| `telegrambot.conversation.notvalidmemberid`       | Error shown when membership number is not a valid number                             | None       |
| `telegrambot.conversation.emailnotvalid`          | Error shown when email format is invalid                                             | None       |
| `telegrambot.conversation.emailalreadyregistered` | Error shown when email is already registered to another user                         | None       |
| `telegrambot.conversation.valuesaved`             | Confirmation shown after a field value is saved                                      | None       |
| `telegrambot.conversation.creationsuccess`        | Success message shown after profile is created                                       | None       |

## Cancel Command

| Key                                   | Description                                      | Parameters |
|---------------------------------------|--------------------------------------------------|------------|
| `telegrambot.cancelcommand.cancelled` | Confirmation message when an action is cancelled | None       |

## Delete Command (`/elimina`)

| Key                                       | Description                                                               | Parameters |
|-------------------------------------------|---------------------------------------------------------------------------|------------|
| `telegrambot.deletecommand.deleteprofile` | Confirmation prompt before deleting profile                               | `{name}`   |
| `telegrambot.deletecommand.confirmbutton` | "Confirm deletion" button text                                            | None       |
| `telegrambot.deletecommand.deleted`       | Confirmation message after profile is deleted                             | None       |
| `telegrambot.deletecommand.ownerdeleted`  | Message sent to group when owner deletes their profile (cannot be kicked) | None       |

## App Command (`/app`)

| Key                              | Description                                  | Parameters |
|----------------------------------|----------------------------------------------|------------|
| `telegrambot.appcommand.sendapp` | Message with Mensa Italia app download links | None       |

## Language Command (`/lang`)

| Key                        | Description                                           | Parameters   |
|----------------------------|-------------------------------------------------------|--------------|
| `telegrambot.lang.prompt`  | Prompt asking user to choose their preferred language | None         |
| `telegrambot.lang.changed` | Confirmation message after language is changed        | `{language}` |

---

## Parameter Syntax

Parameters are interpolated using the `{parameterName}` syntax. For example:

```
"Hello {name}, welcome back!"
```

When the translation is retrieved with:
```go
tolgee.T("key", "en", map[string]string{"name": "Mario"})
```

The result will be:
```
"Hello Mario, welcome back!"
```

---

## Supported Languages

The bot currently supports the following languages:

| Code | Language          | Flag |
|------|-------------------|------|
| `it` | Italian (default) | 🇮🇹 |
| `en` | English           | 🇬🇧 |
| `es` | Spanish           | 🇪🇸 |
| `de` | German            | 🇩🇪 |
