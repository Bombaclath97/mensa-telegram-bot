package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	"github.com/Bombaclath97/mensa-telegram-bot/membership"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/go-telegram/bot"
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

	// Only the groups the bot is still in. A group it has left can never be probed, and one
	// unprobeable group would leave this member unstamped forever.
	groups, err := groupRepo.GetActive()
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
			// A chat upgraded to a supergroup is RESOLVED, not unknown: Telegram hands us the
			// new id in a typed field, so we re-point the row and probe again below. Without
			// this, the dead row is unprobeable forever and — because one bad row fails the
			// whole sweep — no user ever gets groups_synced_at stamped again.
			var migrate *bot.MigrateError
			if errors.As(err, &migrate) && migrate.MigrateToChatID != 0 {
				newID := int64(migrate.MigrateToChatID)
				logger.Info("group sync: chat was upgraded to a supergroup, migrating the group row",
					map[string]interface{}{
						"user_id": userID,
						"old_id":  group.GroupID,
						"new_id":  newID,
					})

				if err := groupRepo.MigrateGroupID(group.GroupID, newID); err != nil {
					complete = false
					logger.Error("group sync: could not migrate the upgraded group",
						map[string]interface{}{
							"user_id": userID,
							"old_id":  group.GroupID,
							"new_id":  newID,
							"error":   err.Error(),
						})
					continue
				}

				// Re-probe under the new id so THIS sweep still completes honestly. The
				// migrated row is already in `groups`; if it was also in this loop's snapshot
				// it gets probed twice, which is idempotent and harmless.
				member, err = tb.GetChatMember(ctx, newID, userID)
				if err != nil {
					complete = false
					logger.Warn("group sync: could not probe the upgraded group, the sweep will not be stamped",
						map[string]interface{}{
							"user_id":  userID,
							"group_id": newID,
							"error":    err.Error(),
						})
					continue
				}
				group.GroupID = newID
			} else if membership.IsBotGone(err) {
				// The bot is no longer in this group (removed, or the chat was deleted) and we
				// missed the my_chat_member update, e.g. it happened while the bot was down.
				// That is a known answer, not an unknown one: record it, stop probing the
				// group, and keep the sweep complete.
				membership.RecordBotGone(ctx, group.GroupID, err)
				continue
			} else {
				// A transient API failure, or something we cannot classify. We do not know
				// whether the member is in there, so the sweep is not complete and must not be
				// stamped.
				complete = false
				logger.Warn("group sync: could not probe a group, the sweep will not be stamped",
					map[string]interface{}{
						"user_id":  userID,
						"group_id": group.GroupID,
						"error":    err.Error(),
					})
				continue
			}
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

// BackfillUnsyncedMembers re-probes every member whose groups were never successfully synced
// and stamps the ones that now complete.
//
// # Why this is needed at all
//
// SyncMemberGroups runs exactly once per member, at registration. If it fails there, nothing
// ever retries it — and it fails for EVERY member whenever a single row in `groups` cannot be
// probed, which is what a chat id left stale by a supergroup upgrade does. The result is a
// population of members stuck at groups_synced_at = NULL that the cleanup sweep defers
// forever, long after the underlying group row is repaired.
//
// This is opt-in (GROUP_SYNC_BACKFILL) and one-shot rather than periodic: it exists to drain
// a backlog after a repair, not to paper over a recurring failure. If it keeps finding work
// on every boot, something in `groups` is still unprobeable and that is the thing to fix.
//
// Members that still cannot be synced are left NULL, which is the safe direction: unstamped
// means "deferred", never "deletable".
func BackfillUnsyncedMembers(ctx context.Context) {
	userRepo := database.NewUserRepository()

	ids, err := userRepo.GetUnsyncedGroupIDs()
	if err != nil {
		logger.Error("group sync backfill: could not list unsynced members",
			map[string]interface{}{"error": err.Error()})
		return
	}
	if len(ids) == 0 {
		logger.Info("group sync backfill: nothing to do, every member is stamped", nil)
		return
	}

	logger.Info("group sync backfill: starting", map[string]interface{}{"members": len(ids)})

	var synced, failed int
	for _, userID := range ids {
		if ctx.Err() != nil {
			logger.Warn("group sync backfill: cancelled before finishing", map[string]interface{}{
				"synced":    synced,
				"failed":    failed,
				"remaining": len(ids) - synced - failed,
			})
			return
		}

		if err := SyncMemberGroups(ctx, userID); err != nil {
			failed++
			logger.Warn("group sync backfill: member still could not be synced",
				map[string]interface{}{"user_id": userID, "error": err.Error()})
			continue
		}
		synced++
	}

	logger.Info("group sync backfill: finished", map[string]interface{}{
		"members": len(ids),
		"synced":  synced,
		"failed":  failed,
	})
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
