package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/go-telegram/bot/models"
)

// SyncMemberGroups probes every group the bot knows about, asking Telegram whether this
// member is in it, and rewrites their groups_associations rows to match. On a complete
// sweep it stamps users.groups_synced_at.
//
// # Why this exists
//
// groups_associations only ever got written when a JOIN REQUEST was approved
// (join_request_handler.go) or for group ADMINS (group_handler.go's groupSyncAdmins).
// Anyone already sitting in a group before the bot arrived never issues a join request, so
// the bot held no record of them. That silently broke three things: the cleanup sweep
// deleted an expired member's row while kicking them from *nothing* (leaving them in the
// group forever, untracked), /elimina removed nobody, and /profilo showed "no groups" for
// everyone.
//
// The Bot API cannot list a group's members — there is no such method — so the only way to
// rebuild the mapping is to ask about one user at a time, which is exactly what this does
// once we know who they are.
//
// # The completeness contract
//
// The stamp is the whole point, and it is only honest if the sweep really finished. If ANY
// group could not be probed, groups_synced_at stays NULL and an error is returned: a partial
// probe re-creates the very ambiguity the column exists to remove (see User.GroupsSyncedAt),
// and would hand the cleanup sweep permission to delete a member whose groups we never
// actually established.
//
// Registration must NOT fail when this does. The member is registered either way; an
// unstamped row simply means the next sweep defers them, which is the safe direction.
func SyncMemberGroups(ctx context.Context, userID int64) error {
	groupRepo := database.NewGroupRepository()
	assocRepo := database.NewGroupAssociationRepository()
	userRepo := database.NewUserRepository()

	groups, err := groupRepo.GetAll()
	if err != nil {
		return fmt.Errorf("loading known groups: %w", err)
	}

	var (
		complete = true
		inGroups int
	)

	for _, group := range groups {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		member, err := tb.GetChatMember(ctx, group.GroupID, userID)
		if err != nil {
			// Could be anything: the bot was removed from the group, the chat is gone, a
			// transient API failure. We do not know whether the member is in there, so the
			// sweep is not complete and must not be stamped.
			complete = false
			logger.Warn("group sync: could not probe a group, the sweep will not be stamped",
				map[string]interface{}{
					"user_id":  userID,
					"group_id": group.GroupID,
					"error":    err.Error(),
				})
			continue
		}

		if memberIsInGroup(member) {
			// Register is idempotent and creates the group row first, so the FK holds.
			if err := assocRepo.Register(userID, group.GroupID); err != nil {
				complete = false
				logger.Error("group sync: could not write the association", map[string]interface{}{
					"user_id":  userID,
					"group_id": group.GroupID,
					"error":    err.Error(),
				})
				continue
			}
			inGroups++
			continue
		}

		// Not in the group. Drop any stale association so the table stays truthful: this
		// sweep is authoritative, not merely additive.
		if err := assocRepo.Delete(userID, group.GroupID); err != nil {
			complete = false
			logger.Error("group sync: could not drop a stale association", map[string]interface{}{
				"user_id":  userID,
				"group_id": group.GroupID,
				"error":    err.Error(),
			})
		}
	}

	if !complete {
		return fmt.Errorf("group sync for user %d was incomplete: groups_synced_at left NULL", userID)
	}

	if err := userRepo.MarkGroupsSynced(userID, time.Now()); err != nil {
		return fmt.Errorf("stamping groups_synced_at for user %d: %w", userID, err)
	}

	logger.Info("group sync complete", map[string]interface{}{
		"user_id":   userID,
		"probed":    len(groups),
		"in_groups": inGroups,
	})
	return nil
}

// memberIsInGroup reports whether a ChatMember response means the user is actually in the
// chat right now.
//
// NOTE: this is deliberately NOT groupIsInsideStatus (group_handler.go), which answers the
// same question about the BOT and treats "restricted" as inside unconditionally. That is
// close enough for the bot's own membership, but wrong here: a restricted user may have
// LEFT, and is_member is the only thing that says so. Reusing the other predicate would
// invent an association for someone who is not in the group — and an invented association
// means the cleanup sweep tries to kick them from a chat they already left.
func memberIsInGroup(member *models.ChatMember) bool {
	if member == nil {
		return false
	}

	switch member.Type {
	case models.ChatMemberTypeOwner,
		models.ChatMemberTypeAdministrator,
		models.ChatMemberTypeMember:
		return true
	case models.ChatMemberTypeRestricted:
		return member.Restricted != nil && member.Restricted.IsMember
	default:
		// left, kicked/banned, and the zero value.
		return false
	}
}
