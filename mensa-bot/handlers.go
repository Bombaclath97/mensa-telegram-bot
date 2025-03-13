package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	model "git.bombaclath.cc/bombadurelli/mensa-bot-telegram/mensa-shared-models"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func startHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if !utils.IsMemberRegistered(userId) {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    userId,
			Text:      model.NOT_REGISTERED_MESSAGE,
		})
	} else {
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
		conversationStateSaver.SetState(userId, utils.ASKED_EMAIL)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    userId,
			Text:      model.INITIATE_PROFILE_REGISTRATION_MESSAGE,
		})
	} else {
		bBody, _ := utils.GetMember(userId)

		var user model.User
		json.Unmarshal(bBody, &user)
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

	//Check if user has a bot profile registered
	if !utils.IsMemberRegistered(userId) {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    userId,
			Text:      fmt.Sprintf(model.INVITE_TO_JOIN_MESSAGE, firstName),
		})
		requestsToApprove.AddRequest(userId, chatId)
	} else {
		b.ApproveChatJoinRequest(ctx, &bot.ApproveChatJoinRequestParams{
			ChatID: chatId,
			UserID: userId,
		})
	}
}

func onMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	state := conversationStateSaver.GetState(update.Message.From.ID)
	if state > -1 && !lockedUsers.IsUserLocked(update.Message.From.ID) {
		if update.Message.Text == "/cancel" {
			conversationStateSaver.RemoveState(update.Message.From.ID)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ParseMode: "Markdown",
				ChatID:    update.Message.From.ID,
				Text:      model.CANCEL_REGISTRATION_MESSAGE,
			})
		} else {
			// Conversation state machine already started
			switch state {
			// User has just used the /profile command, waiting for email
			case utils.ASKED_EMAIL:
				if strings.Contains(update.Message.Text, "@mensa.it") {
					if exist, _ := utils.EmailExistsInDatabase(update.Message.Text); !exist {
						intermediateUserSaver.SetEmail(update.Message.From.ID, update.Message.Text)
						conversationStateSaver.SetState(update.Message.From.ID, utils.ASKED_MEMBER_NUMBER)
						b.SendMessage(ctx, &bot.SendMessageParams{
							ParseMode: "Markdown",
							ChatID:    update.Message.From.ID,
							Text:      model.ASK_MEMBER_NUMBER_MESSAGE,
						})
					} else {
						b.SendMessage(ctx, &bot.SendMessageParams{
							ParseMode: "Markdown",
							ChatID:    update.Message.From.ID,
							Text:      model.EMAIL_ALREADY_REGISTERED,
						})
					}
				} else {
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
						intermediateUserSaver.SetMemberNumber(update.Message.From.ID, int64(memberNumber))
						conversationStateSaver.SetState(update.Message.From.ID, utils.AWAITING_APPROVAL)
						lockedUsers.LockUser(update.Message.From.ID, token)

						b.SendMessage(ctx, &bot.SendMessageParams{
							ParseMode: "Markdown",
							ChatID:    update.Message.From.ID,
							Text:      fmt.Sprintf(model.AWAIT_APPROVAL_ON_APP),
						})
					} else { // Associazione inesistente, ricomincia da capo
						b.SendMessage(ctx, &bot.SendMessageParams{
							ParseMode: "Markdown",
							ChatID:    update.Message.From.ID,
							Text:      fmt.Sprintf(model.NON_EXISTENT_ASSOCIATION_MESSAGE, intermediateUserSaver.GetEmail(update.Message.From.ID), memberNumber),
						})
						intermediateUserSaver.RemoveUser(update.Message.From.ID)
						conversationStateSaver.RemoveState(update.Message.From.ID)
					}
				} else { // The message is not a number
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
}

func approveHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !utils.IsMemberRegistered(update.Message.From.ID) {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    update.Message.From.ID,
			Text:      model.NOT_REGISTERED_MESSAGE,
		})
	} else {
		requestsToJoin, ok := requestsToApprove.GetRequests(update.Message.From.ID)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ParseMode: "Markdown",
			ChatID:    update.Message.From.ID,
			Text:      "Approving requests..." + fmt.Sprint(requestsToJoin),
		})
		if !ok {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ParseMode: "Markdown",
				ChatID:    update.Message.From.ID,
				Text:      model.NO_REQUESTS_TO_APPROVE,
			})
		} else {
			for _, chatId := range requestsToJoin {
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

func appHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	b.SendMessage(ctx, &bot.SendMessageParams{
		ParseMode: "Markdown",
		ChatID:    update.Message.From.ID,
		Text:      model.APP_DOWNLOAD_MESSAGE,
	})
}
