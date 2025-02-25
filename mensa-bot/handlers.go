package main

import (
	"context"
	"encoding/json"
	"fmt"

	"git.bombaclath.cc/bombaclath97/mensa-bot-telegram/bot/model"
	"git.bombaclath.cc/bombaclath97/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func startHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if !utils.IsMemberRegistered(userId) {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: userId,
			Text:   model.NOT_REGISTERED_MESSAGE,
		})
	} else {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: userId,
			Text:   fmt.Sprintf(model.ALREADY_REGISTERED_MESSAGE, update.Message.From.FirstName),
		})
	}
}

func profileHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if !utils.IsMemberRegistered(userId) {
		conversationStateSaver.SetState(userId, utils.ASKED_NAME)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: userId,
			Text:   model.INITIATE_PROFILE_REGISTRATION_MESSAGE,
		})
	} else {
		bBody, _ := utils.GetMember(userId)

		var user model.User
		json.Unmarshal(bBody, &user)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: userId,
			Text:   fmt.Sprintf(model.PROFILE_SHOW_MESSAGE, user.FirstName, user.LastName, user.MensaEmail),
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
			ChatID: userId,
			Text:   fmt.Sprintf(model.INVITE_TO_JOIN_MESSAGE, firstName),
		})
		requestsToApprove.AddRequest(userId, chatId)
	}
}

func onMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	state := conversationStateSaver.GetState(update.Message.From.ID)
	if state > -1 {
		switch state {
		case utils.ASKED_NAME:
			intermediateUserSaver.SetFirstName(update.Message.From.ID, update.Message.Text)
			conversationStateSaver.SetState(update.Message.From.ID, utils.ASKED_SURNAME)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: update.Message.From.ID,
				Text:   fmt.Sprintf(model.ASK_SURNAME_MESSAGE, update.Message.Text),
			})
		case utils.ASKED_SURNAME:
			intermediateUserSaver.SetLastName(update.Message.From.ID, update.Message.Text)
			conversationStateSaver.SetState(update.Message.From.ID, utils.ASKED_EMAIL)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: update.Message.From.ID,
				Text:   fmt.Sprintf(model.ASK_EMAIL_MESSAGE, intermediateUserSaver.GetFirstName(update.Message.From.ID), update.Message.Text),
			})
		case utils.ASKED_EMAIL:
			intermediateUserSaver.SetEmail(update.Message.From.ID, update.Message.Text)
			conversationStateSaver.SetState(update.Message.From.ID, utils.ASKED_CONFIRMATION_CODE)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: update.Message.From.ID,
				Text:   model.ASK_CONFIRMATION_CODE_MESSAGE,
			})
		case utils.ASKED_CONFIRMATION_CODE:
			panic("Not implemented")
		default:
			// NO OP
		}
	}
}
