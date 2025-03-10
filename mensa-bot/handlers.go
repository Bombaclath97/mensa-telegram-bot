package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"git.bombaclath.cc/bombaclath97/mensa-bot-telegram/bot/model"
	"git.bombaclath.cc/bombaclath97/mensa-bot-telegram/bot/utils"
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
	if state > -1 {
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
							Text:      model.EMAIL_NOT_VALID_MESSAGE,
						})
					}
				} else {
					b.SendMessage(ctx, &bot.SendMessageParams{
						ParseMode: "Markdown",
						ChatID:    update.Message.From.ID,
						Text:      model.EMAIL_ALREADY_REGISTERED,
					})
				}
			// User has inserted email, waiting for member number
			case utils.ASKED_MEMBER_NUMBER:
				memberNumber, err := strconv.Atoi(update.Message.Text)

				// If the message is a number
				if err == nil {
					// If an association exists in the App
					if utils.IsMember(intermediateUserSaver.GetEmail(update.Message.From.ID), update.Message.Text) {
						intermediateUserSaver.SetMemberNumber(update.Message.From.ID, int64(memberNumber))
						conversationStateSaver.SetState(update.Message.From.ID, utils.ASKED_NAME)
						b.SendMessage(ctx, &bot.SendMessageParams{
							ParseMode: "Markdown",
							ChatID:    update.Message.From.ID,
							Text:      fmt.Sprintf(model.ASK_NAME_MESSAGE),
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
			// User has inserted surname, generate confirmation code and send email, then ask for confirmation code
			case utils.ASKED_SURNAME:
				intermediateUserSaver.SetLastName(update.Message.From.ID, update.Message.Text)
				conversationStateSaver.SetState(update.Message.From.ID, utils.ASKED_CONFIRMATION_CODE)

				confirmationCode := utils.GenerateConfirmationCode()
				intermediateUserSaver.SetConfirmationCode(update.Message.From.ID, confirmationCode)

				utils.SendConfirmationEmail(intermediateUserSaver.GetEmail(update.Message.From.ID), intermediateUserSaver.GetFirstName(update.Message.From.ID), confirmationCode)
				b.SendMessage(ctx, &bot.SendMessageParams{
					ParseMode: "Markdown",
					ChatID:    update.Message.From.ID,
					Text:      fmt.Sprintf(model.ASK_CONFIRMATION_CODE_MESSAGE, intermediateUserSaver.GetEmail(update.Message.From.ID)),
				})
			// User has inserted confirmation code, check if it's correct
			case utils.ASKED_CONFIRMATION_CODE:
				if update.Message.Text == intermediateUserSaver.GetConfirmationCode(update.Message.From.ID) {
					user := intermediateUserSaver.GetCompleteUserAndCleanup(update.Message.From.ID)
					utils.RegisterMember(update.Message.From.ID, user)

					conversationStateSaver.RemoveState(update.Message.From.ID)
					intermediateUserSaver.RemoveUser(update.Message.From.ID)
					b.SendMessage(ctx, &bot.SendMessageParams{
						ParseMode: "Markdown",
						ChatID:    update.Message.From.ID,
						Text:      model.REGISTRATION_SUCCESS_MESSAGE,
					})
				} else {
					b.SendMessage(ctx, &bot.SendMessageParams{
						ParseMode: "Markdown",
						ChatID:    update.Message.From.ID,
						Text:      model.INVALID_CONFIRMATION_CODE_MESSAGE,
					})
				}
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
