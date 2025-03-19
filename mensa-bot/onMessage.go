package main

import (
	"context"
	"strconv"
	"strings"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func onMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	state := conversationStateSaver.GetState(update.Message.From.ID)

	if update.Message.Text == "/cancel" && state > -1 {
		log.Printf("INFO: User %d cancelled the registration process", update.Message.From.ID)
		conversationStateSaver.RemoveState(update.Message.From.ID)
		intermediateUserSaver.RemoveUser(update.Message.From.ID)
		lockedUsers.UnconditionalUnlockUser(update.Message.From.ID)

		utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.cancelcommand.cancelled", "it"))
		return
	}
	if state > -1 && !lockedUsers.IsUserLocked(update.Message.From.ID) {
		// Conversation state machine already started
		switch state {
		// User has just used the /profile command, waiting for email
		case utils.ASKED_EMAIL:
			if strings.Contains(update.Message.Text, "@mensa.it") {
				if exist, _ := utils.EmailExistsInDatabase(update.Message.Text); !exist {
					log.Printf("INFO: User %d provided a valid email: %s", update.Message.From.ID, update.Message.Text)
					intermediateUserSaver.SetEmail(update.Message.From.ID, update.Message.Text)
					conversationStateSaver.SetState(update.Message.From.ID, utils.ASKED_MEMBER_NUMBER)

					utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.conversation.requestmemberid", "it"))
				} else {
					log.Printf("INFO: Email %s already registered for user %d", update.Message.Text, update.Message.From.ID)

					utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.conversation.emailalreadyregistered", "it"))
				}
			} else {
				log.Printf("INFO: Invalid email provided by user %d: %s", update.Message.From.ID, update.Message.Text)

				utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.conversation.emailnotvalid", "it"))
			}
		// User has inserted email, waiting for member number
		case utils.ASKED_MEMBER_NUMBER:
			memberNumber, err := strconv.Atoi(update.Message.Text)

			// If the message is a number
			if err == nil {
				// If an association exists in the App. If it does, wait for approval on app
				if isMember, token := utils.CheckIfIsMemberAndSendCallmeURL(intermediateUserSaver.GetEmail(update.Message.From.ID), update.Message.Text, update.Message.From.ID); isMember {
					log.Printf("INFO: User %d provided a valid member number: %d", update.Message.From.ID, memberNumber)
					intermediateUserSaver.SetMemberNumber(update.Message.From.ID, int64(memberNumber))
					conversationStateSaver.SetState(update.Message.From.ID, utils.AWAITING_APPROVAL)
					lockedUsers.LockUser(update.Message.From.ID, token)

					utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.conversation.awaitappinteraction", "it"))
				} else { // Associazione inesistente, ricomincia da capo
					log.Printf("INFO: Non-existent association for user %d with member number %d", update.Message.From.ID, memberNumber)
					intermediateUserSaver.RemoveUser(update.Message.From.ID)
					conversationStateSaver.RemoveState(update.Message.From.ID)

					utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.conversation.associationnonexistent", "it"))
				}
			} else { // The message is not a number
				log.Printf("INFO: Invalid member number provided by user %d: %s", update.Message.From.ID, update.Message.Text)

				utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.conversation.notvalidmemberid", "it"))
			}
		// User has inserted name, waiting for surname
		case utils.ASKED_NAME:
			intermediateUserSaver.SetFirstName(update.Message.From.ID, update.Message.Text)
			conversationStateSaver.SetState(update.Message.From.ID, utils.ASKED_SURNAME)

			paramMap := map[string]string{
				"name": update.Message.Text,
			}

			utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.conversation.requestlastname", "it", paramMap))
		// User has inserted surname, registration is complete
		case utils.ASKED_SURNAME:
			user := intermediateUserSaver.GetCompleteUserAndCleanup(update.Message.From.ID)
			user.LastName = update.Message.Text
			utils.RegisterMember(user)

			conversationStateSaver.RemoveState(update.Message.From.ID)
			intermediateUserSaver.RemoveUser(update.Message.From.ID)

			utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.conversation.creationsuccess", "it"))
		case utils.ASKED_DELETE_CONFIRMATION:
			if update.Message.Text == "Sono sicuro di ciò che faccio" {
				conversationStateSaver.RemoveState(update.Message.From.ID)
				groupsToKickFrom, err := utils.GetGroupsForUser(update.Message.From.ID)

				if err != nil {
					log.Printf("ERROR: Failed to get groups for user %d: %v", update.Message.From.ID, err)
					utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.general.error", "it"))
				} else {
					for _, group := range groupsToKickFrom {
						userRole, _ := b.GetChatMember(ctx, &bot.GetChatMemberParams{
							ChatID: group.GroupID,
							UserID: update.Message.From.ID,
						})

						if userRole.Owner != nil && userRole.Owner.Status == "creator" {
							log.Printf("INFO: User %d is the owner of group %d, can't demote nor kick. Sending alert to group", update.Message.From.ID, group.GroupID)
							utils.SendMessage(b, ctx, int64(group.GroupID), tolgee.GetTranslation("telegrambot.deletecommand.ownerdeleted", "it"))
							continue
						}

						if userRole.Administrator != nil && userRole.Administrator.Status == "administrator" {
							log.Printf("INFO: User %d is an admin of group %d, demoting him", update.Message.From.ID, group.GroupID)
							utils.DemoteUser(b, ctx, int64(group.GroupID), update.Message.From.ID)
						}

						log.Printf("INFO: Kicking user %d from group %d", update.Message.From.ID, group.GroupID)
						utils.KickUser(b, ctx, int64(group.GroupID), update.Message.From.ID)
					}
				}

				utils.DeleteMember(update.Message.From.ID)
				utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.deletecommand.deleted", "it"))
			} else {
				utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.deletecommand.askagain", "it"))
			}
		case utils.MANUAL_ASK_USER_ID:
			// Check if message is int64
			userId, err := strconv.ParseInt(update.Message.Text, 10, 64)
			if err != nil {
				utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.manualadd.notvaliduserid", "it"))
				return
			}
			intermediateUserSaver.SetUserId(update.Message.From.ID, userId)
			conversationStateSaver.SetState(update.Message.From.ID, utils.MANUAL_ASK_EMAIL)
			utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.manualadd.requestemail", "it"))
		case utils.MANUAL_ASK_EMAIL:
			intermediateUserSaver.SetEmail(update.Message.From.ID, update.Message.Text)
			conversationStateSaver.SetState(update.Message.From.ID, utils.MANUAL_ASK_MEMBER_NUMBER)
			utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.manualadd.requestmemberid", "it"))
		case utils.MANUAL_ASK_MEMBER_NUMBER:
			memberNumber, err := strconv.Atoi(update.Message.Text)
			if err != nil {
				utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.manualadd.notvalidmemberid", "it"))
				return
			}
			intermediateUserSaver.SetMemberNumber(update.Message.From.ID, int64(memberNumber))
			conversationStateSaver.SetState(update.Message.From.ID, utils.MANUAL_ASK_NAME)
			utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.manualadd.requestname", "it"))
		case utils.MANUAL_ASK_NAME:
			intermediateUserSaver.SetFirstName(update.Message.From.ID, update.Message.Text)
			conversationStateSaver.SetState(update.Message.From.ID, utils.MANUAL_ASK_SURNAME)
			utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.manualadd.requestsurname", "it"))
		case utils.MANUAL_ASK_SURNAME:
			conversationStateSaver.RemoveState(update.Message.From.ID)

			user := intermediateUserSaver.GetCompleteUserAndCleanup(update.Message.From.ID)
			user.LastName = update.Message.Text
			user.IsInternational = true
			utils.RegisterMember(user)
			utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.manualadd.success", "it"))
		}
	}
	if update.Message.ForwardOrigin != nil && utils.IsMemberRegistered(update.Message.From.ID) {
		if update.Message.ForwardOrigin.Type == models.MessageOriginTypeHiddenUser {
			utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.general.forwardedhidden", "it"))
			return
		}
		memberToCheck := update.Message.ForwardOrigin.MessageOriginUser.SenderUser.ID
		if utils.IsMemberRegistered(memberToCheck) {
			user, _ := utils.GetMember(memberToCheck)

			paramMap := map[string]string{
				"name":     user.FirstName,
				"lastName": user.LastName,
			}

			utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.general.forwardedregistered", "it", paramMap))
		} else {
			utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.general.forwardednotregistered", "it"))
		}
	}
}
