package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/gin-gonic/gin"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func startHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if conversationStateSaver.GetState(userId) > -1 {
		return
	}
	if !utils.IsMemberRegistered(userId) {
		log.Printf("INFO: User %d is not registered", userId)

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.startcommand.notregistered", "it"))
	} else {
		log.Printf("INFO: User %d is already registered", userId)
		paramMap := map[string]string{}

		chatUser, err := utils.GetMember(userId)
		if err != nil {
			paramMap["name"] = update.Message.From.FirstName
		} else {
			paramMap["name"] = chatUser.FirstName
		}

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.startcommand.alreadyregistered", "it", paramMap))
	}
}

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
	}
}

func onChatJoinRequest(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatId := update.ChatJoinRequest.Chat.ID
	userId := update.ChatJoinRequest.From.ID
	firstName := update.ChatJoinRequest.From.FirstName

	if conversationStateSaver.GetState(userId) > -1 {
		return
	}

	// Check if user has a bot profile registered
	if !utils.IsMemberRegistered(userId) {
		log.Printf("INFO: User %d requested to join chat %d but is not registered", userId, chatId)

		paramMap := map[string]string{
			"name": firstName,
		}

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.welcome.invitetojoin", "it", paramMap))
		requestsToApprove.AddRequest(userId, chatId)
	} else {
		log.Printf("INFO: Approving chat join request for user %d in chat %d", userId, chatId)
		b.ApproveChatJoinRequest(ctx, &bot.ApproveChatJoinRequestParams{
			ChatID: chatId,
			UserID: userId,
		})

		paramMap := map[string]string{
			"groupName": update.ChatJoinRequest.Chat.Title,
		}

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.welcome.approvedtogroup", "it", paramMap))
		utils.RegisterGroupForUser(userId, chatId)
	}
}

func onMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	state := conversationStateSaver.GetState(update.Message.From.ID)

	if update.Message.Text == "/cancel" && state > -1 {
		log.Printf("INFO: User %d cancelled the registration process", update.Message.From.ID)
		conversationStateSaver.SetState(update.Message.From.ID, utils.IDLE)
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
					conversationStateSaver.SetState(update.Message.From.ID, utils.IDLE)

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
			utils.RegisterMember(update.Message.From.ID, user)

			conversationStateSaver.SetState(update.Message.From.ID, utils.IDLE)
			intermediateUserSaver.RemoveUser(update.Message.From.ID)

			utils.SendMessage(b, ctx, update.Message.From.ID, tolgee.GetTranslation("telegrambot.conversation.creationsuccess", "it"))
		case utils.ASKED_DELETE_CONFIRMATION:
			if update.Message.Text == "Sono sicuro di ciò che faccio" {
				conversationStateSaver.SetState(update.Message.From.ID, utils.IDLE)
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

func approveHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if conversationStateSaver.GetState(userId) > -1 {
		return
	}

	if !utils.IsMemberRegistered(update.Message.From.ID) {
		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.startcommand.notregistered", "it"))
	} else {
		requestsToJoin, ok := requestsToApprove.GetRequests(update.Message.From.ID)
		log.Printf("INFO: Approving requests for user %d: %v", update.Message.From.ID, requestsToJoin)
		if !ok {
			utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.approvecommand.norequesttoapprove", "it"))
		} else {
			for _, chatId := range requestsToJoin {
				b.ApproveChatJoinRequest(ctx, &bot.ApproveChatJoinRequestParams{
					ChatID: chatId,
					UserID: update.Message.From.ID,
				})

				utils.RegisterGroupForUser(update.Message.From.ID, chatId)
			}
			requestsToApprove.RemoveRequests(update.Message.From.ID)
			utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.approvecommand.approved", "it"))
		}
	}
}

func postCallmeAPI(ctx *gin.Context, b *bot.Bot) {
	fullToken := ctx.Param("token")
	userId := strings.Split(fullToken, "_")[0]
	userIdInt, err := strconv.ParseInt(userId, 10, 64)
	if err != nil {
		log.Printf("ERROR: Invalid user ID %s: %v", userId, err)
		ctx.JSON(400, gin.H{"message": "Invalid user ID"})
		return
	}

	log.Printf("INFO: Received callme API request for user %s", userId)

	var callmeApi callmeApi

	if err := ctx.ShouldBindJSON(&callmeApi); err != nil {
		log.Printf("ERROR: Couldn't bind body with JSON: %v", err)
		ctx.JSON(400, gin.H{"message": "Received unexpected body!"})
		return
	}

	if callmeApi.Accepted {
		if lockedUsers.UnlockUser(userIdInt, fullToken) {
			log.Printf("INFO: Unlocking user %s", userId)

			conversationStateSaver.SetState(userIdInt, utils.ASKED_NAME)
			utils.SendMessage(b, ctx, userIdInt, tolgee.GetTranslation("telegrambot.conversation.approvedapproval", "it"))
			ctx.JSON(200, gin.H{"message": "Utente sbloccato"})
		} else {
			log.Printf("ERROR: Couldn't unlock user %s. Received full-token %s", userId, fullToken)
			ctx.JSON(400, gin.H{"message": "Errore"})
		}
	} else {
		log.Printf("INFO: User %s rejected the API call!", userId)

		ctx.JSON(200, gin.H{"message": "Richiesta rifiutata"})
		conversationStateSaver.SetState(userIdInt, utils.IDLE)
		lockedUsers.UnconditionalUnlockUser(userIdInt)

		utils.SendMessage(b, ctx, userIdInt, tolgee.GetTranslation("telegrambot.conversation.rejectedapproval", "it"))
	}
}

func appHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if conversationStateSaver.GetState(userId) > -1 {
		return
	}

	utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.appcommand.sendapp", "it"))
}

func deleteHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if conversationStateSaver.GetState(userId) > -1 {
		return
	}

	if !utils.IsMemberRegistered(userId) {
		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.startcommand.notregistered", "it"))
	} else {
		user, _ := utils.GetMember(userId)

		paramMap := map[string]string{
			"name": user.FirstName,
		}

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.deletecommand.deleteprofile", "it", paramMap))
		conversationStateSaver.SetState(userId, utils.ASKED_DELETE_CONFIRMATION)
	}
}

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
