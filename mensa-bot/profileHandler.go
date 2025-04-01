package main

import (
	"context"
	"fmt"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func profileHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if conversationStateSaver.GetState(userId) > -1 {
		return
	}
	if !utils.IsMemberRegistered(userId) {
		log.Printf("INFO: Initiating profile registration for user %d", userId)
		conversationStateSaver.SetState(userId, utils.ASKED_EMAIL)
		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.profilecommand.initiateprofileregister", "it"))
	} else {
		log.Printf("INFO: Showing profile for user %d", userId)
		user, err := utils.GetMember(userId)
		if err != nil {
			log.Printf("ERROR: Failed to get member info for user %d: %v", userId, err)

			utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.general.error", "it"))
			return
		}

		paramMap := map[string]string{
			"name":    user.FirstName,
			"surname": user.LastName,
			"email":   user.MensaEmail,
		}

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.profilecommand.showprofile", "it", paramMap))

		groups, err := utils.GetGroupsForUser(userId)
		if err != nil {
			log.Printf("ERROR: Failed to get groups for user %d: %v", userId, err)
			return
		}

		groupsToSend := make([]string, len(groups))

		for _, group := range groups {
			groupName, _ := b.GetChat(ctx, &bot.GetChatParams{
				ChatID: group.GroupID,
			})

			line := fmt.Sprintf("- %s", groupName.Title)

			if group.IsGroupAdmin {
				line += " (sei anche admin)"
			}

			groupsToSend = append(groupsToSend, line)
		}

		paramMap = map[string]string{
			"groups": fmt.Sprintf("\n%s", groupsToSend),
		}

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.profilecommand.showgroups", "it", paramMap))
	}
}
