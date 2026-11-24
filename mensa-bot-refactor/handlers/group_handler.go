package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Mensa-Italia/mensa-telegram-bot/alert"
	"github.com/Mensa-Italia/mensa-telegram-bot/database"
	"github.com/Mensa-Italia/mensa-telegram-bot/logger"
	"github.com/Mensa-Italia/mensa-telegram-bot/membership"
	"github.com/Mensa-Italia/mensa-telegram-bot/mtproto"
	tb "github.com/Mensa-Italia/mensa-telegram-bot/telegram_bot"
	"github.com/Mensa-Italia/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// MatchMyChatMember is the match predicate for MyChatMemberHandler.
//
// NOTE FOR THE INTEGRATOR: go-telegram dispatches to the FIRST matching handler only
// (see bot@v1.17.0/process_update.go), so "bot joined" and "bot promoted" MUST NOT be
// registered as two separate match funcs — a bot added straight to a group AS an admin
// matches both and only one would ever run. Register this single handler:
//
//	b.RegisterHandlerMatchFunc(handlers.MatchMyChatMember, handlers.MyChatMemberHandler)
//
// and it dispatches internally to the join / promotion branches.
func MatchMyChatMember(update *models.Update) bool {
	if update == nil || update.MyChatMember == nil {
		return false
	}
	chatType := update.MyChatMember.Chat.Type
	return chatType == models.ChatTypeGroup || chatType == models.ChatTypeSupergroup
}

// MatchChatMigration is the match predicate for ChatMigrationHandler.
//
// Telegram announces a basic-group -> supergroup upgrade with a service message on BOTH
// sides: migrate_to_chat_id in the dying basic group, migrate_from_chat_id in the new
// supergroup. Either one is enough, and matching both makes the handler robust to whichever
// update the bot actually receives (it is often only in one of the two chats by then).
//
// REGISTRATION ORDER: this must be registered BEFORE the command handlers. go-telegram
// dispatches to the first matching handler only, and a service message carries no text, so
// nothing else would claim it — but keeping it with the other update-type handlers makes
// that independent of future command registrations.
func MatchChatMigration(update *models.Update) bool {
	if update == nil || update.Message == nil {
		return false
	}
	return update.Message.MigrateToChatID != 0 || update.Message.MigrateFromChatID != 0
}

// ChatMigrationHandler records a basic-group -> supergroup upgrade the moment it happens,
// instead of letting it be discovered later as a permanently failing probe.
//
// # Why this matters more than it looks
//
// The chat id changes on upgrade. Until the groups row is re-pointed, SyncMemberGroups keeps
// probing the dead id and fails; because a single unprobeable row fails the WHOLE sweep,
// users.groups_synced_at is then left NULL for every member on every registration, and the
// cleanup sweep defers all of them indefinitely. SyncMemberGroups also self-heals from the
// MigrateError it gets back (see group_sync.go), but that only fires when someone happens to
// register; this catches it immediately.
func ChatMigrationHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !MatchChatMigration(update) {
		return
	}

	msg := update.Message

	// Normalise both directions into (old, new).
	oldID, newID := msg.Chat.ID, msg.MigrateToChatID
	if msg.MigrateFromChatID != 0 {
		oldID, newID = msg.MigrateFromChatID, msg.Chat.ID
	}
	if oldID == 0 || newID == 0 || oldID == newID {
		return
	}

	groupRepo := database.NewGroupRepository()

	// Only act on groups the bot actually tracks: an upgrade of some unrelated chat we were
	// merely present in must not create rows.
	known, err := groupRepo.Exists(oldID)
	if err != nil {
		logger.Error("Chat migration: could not check the old group", map[string]interface{}{
			"old_id": oldID,
			"new_id": newID,
			"error":  err.Error(),
		})
		return
	}
	if !known {
		logger.Debug("Chat migration ignored: the old chat was not a tracked group",
			map[string]interface{}{"old_id": oldID, "new_id": newID})
		return
	}

	if err := groupRepo.MigrateGroupID(oldID, newID); err != nil {
		logger.Error("Chat migration FAILED: the stale group row will keep failing every group "+
			"sync, leaving groups_synced_at NULL for every member until it is fixed",
			map[string]interface{}{
				"old_id": oldID,
				"new_id": newID,
				"error":  err.Error(),
			})
		return
	}

	logger.Info("Chat migrated to a supergroup, group row re-pointed", map[string]interface{}{
		"old_id": oldID,
		"new_id": newID,
	})

	// The upgraded chat is a real supergroup, so the member list is now readable — which it
	// was not while it was a basic group (see ErrLegacyBasicGroup).
	groupEnumerateExistingMembers(ctx, newID)
}

// MyChatMemberHandler reacts to changes of the BOT's own membership in a group.
//
// MyChatMember updates always concern the bot itself, so no bot-ID check is needed.
// Unlike the legacy onBotJoinsGroup (which read update.Message.Chat.ID on a MyChatMember
// update and nil-panicked), this reads update.MyChatMember.Chat.ID, and unlike the legacy
// matchBotBecomesAdmin (which dereferenced the nillable NewChatMember.Administrator) it
// gates on the ChatMember.Type discriminator, which is always populated.
func MyChatMemberHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !MatchMyChatMember(update) {
		return
	}

	mcm := update.MyChatMember
	groupID := mcm.Chat.ID
	oldStatus := mcm.OldChatMember.Type
	newStatus := mcm.NewChatMember.Type

	logger.Debug("Bot chat member status changed", map[string]interface{}{
		"group_id":   groupID,
		"old_status": string(oldStatus),
		"new_status": string(newStatus),
	})

	joined := groupIsInsideStatus(newStatus) && !groupIsInsideStatus(oldStatus)
	promoted := newStatus == models.ChatMemberTypeAdministrator && oldStatus != models.ChatMemberTypeAdministrator
	removed := !groupIsInsideStatus(newStatus) && groupIsInsideStatus(oldStatus)
	demoted := groupIsInsideStatus(newStatus) && newStatus != models.ChatMemberTypeAdministrator &&
		oldStatus == models.ChatMemberTypeAdministrator

	// Record the bot's standing on EVERY change, leaving included. Before this, leaving was
	// never recorded: the row stayed "active", every member's group sync kept probing a chat
	// the bot could no longer see, failed, and left groups_synced_at NULL for every new
	// member, which makes the cleanup sweep defer all of them forever.
	groupRepo := database.NewGroupRepository()
	if err := groupRepo.SetStatus(groupID, membership.BotStatus(newStatus), mcm.Chat.Title); err != nil {
		logger.Error("Failed to record the bot's status in the group", map[string]interface{}{
			"group_id": groupID,
			"status":   string(newStatus),
			"error":    err.Error(),
		})
		return
	}

	if removed {
		logger.Warn("Bot removed from group", map[string]interface{}{"group_id": groupID, "by": mcm.From.ID})
		alert.Admins(ctx, fmt.Sprintf("🚪 <b>The bot was removed from %s</b> by %s.\n\n"+
			"Members of that group are no longer checked or removed by the bot. If this was a mistake, add the bot back and make it an admin.",
			groupLabel(groupID, mcm.Chat.Title), userLabel(mcm.From)))
		return
	}
	if demoted {
		logger.Warn("Bot lost administrator rights in group", map[string]interface{}{"group_id": groupID, "by": mcm.From.ID})
		alert.Admins(ctx, fmt.Sprintf("⚠️ <b>The bot is no longer an admin in %s</b> (changed by %s).\n\n"+
			"It cannot approve join requests or remove expired members there until it is made admin again.",
			groupLabel(groupID, mcm.Chat.Title), userLabel(mcm.From)))
		return
	}

	if !joined && !promoted {
		// Restriction tweaks and the like: the status is recorded above, nothing else to do.
		return
	}

	if joined {
		// Greet the group in the language of whoever added the bot.
		userRepo := database.NewUserRepository()
		lang := userRepo.GetLanguage(mcm.From.ID)

		if _, err := tb.Send(ctx, groupID, tolgee.T("telegrambot.welcome.botjoined", lang)); err != nil {
			logger.Warn("Failed to greet group", map[string]interface{}{
				"group_id": groupID,
				"error":    err.Error(),
			})
		}

		logger.Info("Bot joined group", map[string]interface{}{"group_id": groupID})
	}

	if promoted {
		logger.Info("Bot promoted to administrator", map[string]interface{}{"group_id": groupID})
	}

	// Sync the group's human admins in both cases. When the bot is only a plain member it
	// usually cannot call getChatAdministrators; the call simply fails and is logged.
	groupSyncAdmins(ctx, groupID)

	// Enumerate the people who were ALREADY in the group, but only once we are an admin.
	//
	// WHY GATED ON `promoted` AND NOT `joined`: channels.getParticipants returns
	// CHAT_ADMIN_REQUIRED to a plain member, and a bot is very often added as a member first
	// and promoted minutes later. Enumerating on `joined` would therefore fail for the common
	// case and leave everyone in the group invisible forever. Telegram tells us the moment we
	// gain the right — this is that moment, so we use it instead of retrying blindly.
	//
	// A bot added DIRECTLY as an administrator satisfies both branches (see the note on
	// MyChatMemberHandler), so that case is covered too and runs exactly once.
	if promoted {
		groupEnumerateExistingMembers(ctx, groupID)
	}
}

// groupEnumerateExistingMembers reads the group's full member list over MTProto and records an
// association for everyone in it who is already a registered member.
//
// # Why this cannot be done any other way
//
// groups_associations is only ever written when a JOIN REQUEST is approved. Someone already
// sitting in the group when the bot arrives never issues one, so the bot holds no record of
// them: the cleanup sweep cannot kick them, /elimina cannot remove them, and /profilo shows
// them no groups. Add the bot to a 500-member group and all 500 are invisible.
//
// The Bot API cannot list members — there is no such method. MTProto can (see the mtproto
// package), and this is the only moment we learn who is in the room.
//
// Best-effort by design: a failure here must never stop the bot joining a group. The members
// stay invisible, which is exactly the status quo before this ran.
func groupEnumerateExistingMembers(ctx context.Context, groupID int64) {
	if !mtproto.Configured() {
		// Not an error: the bot works without MTProto, it just cannot see members who never
		// issued a join request. Warn, because that is a silent and surprising degradation.
		logger.Warn("Group enumeration skipped: MTProto is not configured (TG_API_ID/TG_API_HASH). "+
			"Members who were already in this group will stay invisible to /elimina, /profilo and the cleanup sweep",
			map[string]interface{}{"group_id": groupID})
		return
	}

	members, err := mtproto.Enumerate(ctx, groupID)
	if err != nil {
		switch {
		case errors.Is(err, mtproto.ErrAdminRequired):
			// Should not happen on the `promoted` branch, but harmless if it does: the next
			// promotion event retries.
			logger.Warn("Group enumeration skipped: not an administrator yet", map[string]interface{}{
				"group_id": groupID,
			})
		case errors.Is(err, mtproto.ErrParticipantsHidden):
			// A hard stop, deliberately. A hidden list means any enumeration is PARTIAL, and a
			// partial list would later classify verified members as unprofiled.
			logger.Error("Group enumeration REFUSED: the participant list is hidden, so any read "+
				"would be incomplete. Ask the group admins to unhide members, then re-promote the bot",
				map[string]interface{}{"group_id": groupID})
		case errors.Is(err, mtproto.ErrLegacyBasicGroup):
			// Expected and permanent, so WARN rather than ERROR: no retry will ever help and
			// there is nothing the bot can do. The fix is human — upgrade the chat — and
			// Telegram re-runs this handler against the new supergroup id when that happens.
			logger.Warn("Group enumeration skipped: this is a legacy basic group, which has no "+
				"member-list API. Ask the group admins to upgrade it to a supergroup, then "+
				"re-promote the bot; members already inside stay invisible until then",
				map[string]interface{}{"group_id": groupID})
		default:
			logger.Error("Group enumeration failed", map[string]interface{}{
				"group_id": groupID,
				"error":    err.Error(),
			})
		}
		return
	}

	userRepo := database.NewUserRepository()
	assocRepo := database.NewGroupAssociationRepository()

	var (
		associated   int
		unregistered int
	)

	for _, member := range members {
		// Bots and deleted accounts have no Mensa profile by definition.
		if member.IsBot || member.Deleted {
			continue
		}

		registered, err := userRepo.Exists(member.UserID)
		if err != nil {
			logger.Error("Group enumeration: could not check registration", map[string]interface{}{
				"group_id": groupID,
				"user_id":  member.UserID,
				"error":    err.Error(),
			})
			continue
		}
		if !registered {
			// These are the people the grace-period kicker will eventually target. Counting
			// them is all this does for now — the roster and the 7-day clock are a separate
			// piece of work (see CUTOVER.md).
			unregistered++
			continue
		}

		if err := assocRepo.Register(member.UserID, groupID); err != nil {
			logger.Error("Group enumeration: could not write the association", map[string]interface{}{
				"group_id": groupID,
				"user_id":  member.UserID,
				"error":    err.Error(),
			})
			continue
		}
		associated++
	}

	logger.Info("Group enumeration complete", map[string]interface{}{
		"group_id":     groupID,
		"members":      len(members),
		"associated":   associated,
		"unregistered": unregistered,
	})
}

// groupLabel renders a group for an admin message (HTML).
func groupLabel(groupID int64, title string) string {
	if title == "" {
		return fmt.Sprintf("group <code>%d</code>", groupID)
	}
	return fmt.Sprintf("<b>%s</b> (<code>%d</code>)", tolgee.EscapeHTML(title), groupID)
}

// userLabel renders a Telegram user for an admin message (HTML).
func userLabel(u models.User) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if u.Username != "" {
		name += " (@" + u.Username + ")"
	}
	if name == "" {
		name = "someone"
	}
	return fmt.Sprintf("%s [<code>%d</code>]", tolgee.EscapeHTML(name), u.ID)
}

// groupIsInsideStatus reports whether the given status means the bot is present in the chat.
func groupIsInsideStatus(status models.ChatMemberType) bool {
	switch status {
	case models.ChatMemberTypeMember,
		models.ChatMemberTypeAdministrator,
		models.ChatMemberTypeOwner,
		models.ChatMemberTypeRestricted:
		return true
	default:
		// "left", "kicked" and the zero value (no previous membership) all mean "outside".
		return false
	}
}

// groupSyncAdmins flags every registered member who administers the group with
// is_group_admin = true. This is the only writer of that column: /profilo's "(admin)"
// suffix and /elimina's demote branch both depend on it.
func groupSyncAdmins(ctx context.Context, groupID int64) {
	admins, err := tb.GetChatAdministrators(ctx, groupID)
	if err != nil {
		logger.Error("Failed to get chat administrators", map[string]interface{}{
			"group_id": groupID,
			"error":    err.Error(),
		})
		return
	}

	userRepo := database.NewUserRepository()
	assocRepo := database.NewGroupAssociationRepository()

	for _, admin := range admins {
		adminUser := groupAdminUser(admin)
		if adminUser == nil || adminUser.IsBot {
			continue
		}

		registered, err := userRepo.Exists(adminUser.ID)
		if err != nil {
			logger.Error("Failed to check admin registration", map[string]interface{}{
				"user_id":  adminUser.ID,
				"group_id": groupID,
				"error":    err.Error(),
			})
			continue
		}
		if !registered {
			continue
		}

		// PromoteToAdmin creates the association if it is missing, then sets the flag.
		if err := assocRepo.PromoteToAdmin(adminUser.ID, groupID); err != nil {
			logger.Error("Failed to flag group admin", map[string]interface{}{
				"user_id":  adminUser.ID,
				"group_id": groupID,
				"error":    err.Error(),
			})
			continue
		}

		logger.Info("Flagged registered member as group admin", map[string]interface{}{
			"user_id":  adminUser.ID,
			"group_id": groupID,
		})
	}
}

// groupAdminUser extracts the Telegram user out of a ChatMember without dereferencing a
// nil variant pointer (the legacy code's crash).
func groupAdminUser(member models.ChatMember) *models.User {
	switch member.Type {
	case models.ChatMemberTypeOwner:
		if member.Owner == nil {
			return nil
		}
		return member.Owner.User
	case models.ChatMemberTypeAdministrator:
		if member.Administrator == nil {
			return nil
		}
		return &member.Administrator.User
	default:
		return nil
	}
}
