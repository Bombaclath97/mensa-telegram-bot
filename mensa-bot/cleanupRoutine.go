package main

import (
	"time"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/gin-gonic/gin"
	"github.com/go-telegram/bot"
)

func cleanupRoutine(ctx *gin.Context, b *bot.Bot) {
	start := time.Now()

	log.Printf("INFO: starting cleanup routine")
	users := utils.GetAllMembers()
	for _, user := range users {
		if !utils.IsMembershipActive(user.TelegramID) {
			go func() {
				log.Printf("INFO: User %d has an expired membership. Kicking him from all the groups", user.TelegramID)
				groupsToKickFrom, err := utils.GetGroupsForUser(user.TelegramID)

				if err != nil {
					log.Printf("ERROR: Failed to get groups for user %d: %v", user.TelegramID, err)
					return
				}

				for _, group := range groupsToKickFrom {
					userRole, _ := b.GetChatMember(ctx, &bot.GetChatMemberParams{
						ChatID: group.GroupID,
						UserID: user.TelegramID,
					})

					if userRole.Owner != nil && userRole.Owner.Status == "creator" {
						log.Printf("INFO: User %d is the owner of group %d, can't demote nor kick. Sending alert to group", user.TelegramID, group.GroupID)
						utils.SendMessage(b, ctx, int64(group.GroupID), tolgee.GetTranslation("telegrambot.cleanup.ownerexpired", "it"))
						continue
					}

					if userRole.Administrator != nil && userRole.Administrator.Status == "administrator" {
						log.Printf("INFO: User %d is an admin of group %d, demoting him", user.TelegramID, group.GroupID)
						utils.DemoteUser(b, ctx, int64(group.GroupID), user.TelegramID)
					}

					log.Printf("INFO: Kicking user %d from group %d", user.TelegramID, group.GroupID)
					utils.KickUser(b, ctx, int64(group.GroupID), user.TelegramID)
				}

				log.Printf("INFO: Done, deleting user %d", user.TelegramID)
				utils.SendMessage(b, ctx, user.TelegramID, tolgee.GetTranslation("telegrambot.cleanup.expiredmembership", "it"))
				utils.DeleteMember(user.TelegramID)
			}()
		}
	}

	timeElapsed := time.Since(start)
	log.Printf("INFO: cleanup routine completed in %s", timeElapsed)
	ctx.JSON(200, gin.H{"message": "Routine completata"})
}
