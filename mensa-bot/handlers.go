package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	model "git.bombaclath.cc/bombadurelli/mensa-bot-telegram/mensa-shared-models"
	"github.com/gin-gonic/gin"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func startHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if !utils.IsMemberRegistered(userId) {
		log.Printf("INFO: User %d is not registered", userId)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    userId,
			Text:      model.NOT_REGISTERED_MESSAGE,
		})
	} else {
		log.Printf("INFO: User %d is already registered", userId)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    userId,
			Text:      fmt.Sprintf(model.ALREADY_REGISTERED_MESSAGE, update.Message.From.FirstName),
		})
	}
}

func profileHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if !utils.IsMemberRegistered(userId) {
		log.Printf("INFO: Initiating profile registration for user %d", userId)
		conversationStateSaver.SetState(userId, utils.ASKED_EMAIL)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    userId,
			Text:      model.INITIATE_PROFILE_REGISTRATION_MESSAGE,
		})
	} else {
		log.Printf("INFO: Showing profile for user %d", userId)
		bBody, err := utils.GetMember(userId)
		if err != nil {
			log.Printf("ERROR: Failed to get member info for user %d: %v", userId, err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ParseMode: "Markdown",
				ChatID:    userId,
				Text:      "Errore nel recupero delle informazioni dell'utente.",
			})
			return
		}

		var user model.User
		if err := json.Unmarshal(bBody, &user); err != nil {
			log.Printf("ERROR: Failed to unmarshal member info for user %d: %v", userId, err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ParseMode: "Markdown",
				ChatID:    userId,
				Text:      "Errore nel parsing delle informazioni dell'utente.",
			})
			return
		}

		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    userId,
			Text:      fmt.Sprintf(model.PROFILE_SHOW_MESSAGE, user.FirstName, user.LastName, user.MensaEmail),
		})
	}
}

func onChatJoinRequest(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatId := update.ChatJoinRequest.Chat.ID
	userId := update.ChatJoinRequest.From.ID
	firstName := update.ChatJoinRequest.From.FirstName

	// Check if user has a bot profile registered
	if !utils.IsMemberRegistered(userId) {
		log.Printf("INFO: User %d requested to join chat %d but is not registered", userId, chatId)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    userId,
			Text:      fmt.Sprintf(model.INVITE_TO_JOIN_MESSAGE, firstName),
		})
		requestsToApprove.AddRequest(userId, chatId)
	} else {
		log.Printf("INFO: Approving chat join request for user %d in chat %d", userId, chatId)
		b.ApproveChatJoinRequest(ctx, &bot.ApproveChatJoinRequestParams{
			ChatID: chatId,
			UserID: userId,
		})
	}
}

func onMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	state := conversationStateSaver.GetState(update.Message.From.ID)
	if update.Message.Text == "/cancel" {
		log.Printf("INFO: User %d cancelled the registration process", update.Message.From.ID)
		conversationStateSaver.RemoveState(update.Message.From.ID)
		intermediateUserSaver.RemoveUser(update.Message.From.ID)
		lockedUsers.UnconditionalUnlockUser(update.Message.From.ID)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    update.Message.From.ID,
			Text:      model.CANCEL_REGISTRATION_MESSAGE,
		})
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
					b.SendMessage(ctx, &bot.SendMessageParams{
						ParseMode: "Markdown",
						ChatID:    update.Message.From.ID,
						Text:      model.ASK_MEMBER_NUMBER_MESSAGE,
					})
				} else {
					log.Printf("ERROR: Email %s already registered for user %d", update.Message.Text, update.Message.From.ID)
					b.SendMessage(ctx, &bot.SendMessageParams{
						ParseMode: "Markdown",
						ChatID:    update.Message.From.ID,
						Text:      model.EMAIL_ALREADY_REGISTERED,
					})
				}
			} else {
				log.Printf("ERROR: Invalid email provided by user %d: %s", update.Message.From.ID, update.Message.Text)
				b.SendMessage(ctx, &bot.SendMessageParams{
					ParseMode: "Markdown",
					ChatID:    update.Message.From.ID,
					Text:      model.EMAIL_NOT_VALID_MESSAGE,
				})
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

					b.SendMessage(ctx, &bot.SendMessageParams{
						ParseMode: "Markdown",
						ChatID:    update.Message.From.ID,
						Text:      model.AWAIT_APPROVAL_ON_APP,
					})
				} else { // Associazione inesistente, ricomincia da capo
					log.Printf("ERROR: Non-existent association for user %d with member number %d", update.Message.From.ID, memberNumber)
					b.SendMessage(ctx, &bot.SendMessageParams{
						ParseMode: "Markdown",
						ChatID:    update.Message.From.ID,
						Text:      fmt.Sprintf(model.NON_EXISTENT_ASSOCIATION_MESSAGE, intermediateUserSaver.GetEmail(update.Message.From.ID), memberNumber),
					})
					intermediateUserSaver.RemoveUser(update.Message.From.ID)
					conversationStateSaver.RemoveState(update.Message.From.ID)
				}
			} else { // The message is not a number
				log.Printf("ERROR: Invalid member number provided by user %d: %s", update.Message.From.ID, update.Message.Text)
				b.SendMessage(ctx, &bot.SendMessageParams{
					ParseMode: "Markdown",
					ChatID:    update.Message.From.ID,
					Text:      model.MEMBER_NUMBER_IS_NOT_VALID_MESSAGE,
				})
			}
		// User has inserted name, waiting for surname
		case utils.ASKED_NAME:
			intermediateUserSaver.SetFirstName(update.Message.From.ID, update.Message.Text)
			conversationStateSaver.SetState(update.Message.From.ID, utils.ASKED_SURNAME)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ParseMode: "Markdown",
				ChatID:    update.Message.From.ID,
				Text:      fmt.Sprintf(model.ASK_SURNAME_MESSAGE, update.Message.Text),
			})
		// User has inserted surname, registration is complete
		case utils.ASKED_SURNAME:
			user := intermediateUserSaver.GetCompleteUserAndCleanup(update.Message.From.ID)
			user.LastName = update.Message.Text
			utils.RegisterMember(update.Message.From.ID, user)

			conversationStateSaver.RemoveState(update.Message.From.ID)
			intermediateUserSaver.RemoveUser(update.Message.From.ID)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ParseMode: "Markdown",
				ChatID:    update.Message.From.ID,
				Text:      model.REGISTRATION_SUCCESS_MESSAGE,
			})

		}
	}
}

func approveHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !utils.IsMemberRegistered(update.Message.From.ID) {
		log.Printf("ERROR: User %d is not registered and cannot approve requests", update.Message.From.ID)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    update.Message.From.ID,
			Text:      model.NOT_REGISTERED_MESSAGE,
		})
	} else {
		requestsToJoin, ok := requestsToApprove.GetRequests(update.Message.From.ID)
		log.Printf("INFO: Approving requests for user %d: %v", update.Message.From.ID, requestsToJoin)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    update.Message.From.ID,
			Text:      "Approving requests..." + fmt.Sprint(requestsToJoin),
		})
		if !ok {
			log.Printf("INFO: No requests to approve for user %d", update.Message.From.ID)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ParseMode: "Markdown",
				ChatID:    update.Message.From.ID,
				Text:      model.NO_REQUESTS_TO_APPROVE,
			})
		} else {
			for _, chatId := range requestsToJoin {
				log.Printf("INFO: Approving chat join request for user %d in chat %d", update.Message.From.ID, chatId)
				b.ApproveChatJoinRequest(ctx, &bot.ApproveChatJoinRequestParams{
					ChatID: chatId,
					UserID: update.Message.From.ID,
				})
			}
			requestsToApprove.RemoveRequests(update.Message.From.ID)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ParseMode: "Markdown",
				ChatID:    update.Message.From.ID,
				Text:      model.REQUESTS_APPROVED_MESSAGE,
			})
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
			b.SendMessage(ctx, &bot.SendMessageParams{
				ParseMode: "Markdown",
				ChatID:    userIdInt,
				Text:      "Richiesta approvata, grazie! Puoi per favore scrivermi il tuo nome ora?",
			})
			ctx.JSON(200, gin.H{"message": "Utente sbloccato"})
		} else {
			log.Printf("ERROR: Couldn't unlock user %s", userId)
			ctx.JSON(400, gin.H{"message": "Errore"})
		}
	} else {
		log.Printf("INFO: Refusing callme API request for user %s", userId)

		ctx.JSON(200, gin.H{"message": "Richiesta rifiutata"})
		conversationStateSaver.RemoveState(userIdInt)
		lockedUsers.UnconditionalUnlockUser(userIdInt)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    userIdInt,
			Text:      "Richiesta rifiutata, mi dispiace. Se hai bisogno di aiuto, contatta il @Bombaclath97.",
		})
	}
}

func appHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	log.Printf("INFO: Sending app download message to user %d", update.Message.From.ID)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ParseMode: "Markdown",
		ChatID:    update.Message.From.ID,
		Text:      model.APP_DOWNLOAD_MESSAGE,
	})
}
