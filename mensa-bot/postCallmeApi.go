package main

import (
	"strconv"
	"strings"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/gin-gonic/gin"
	"github.com/go-telegram/bot"
)

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
		conversationStateSaver.RemoveState(userIdInt)
		lockedUsers.UnconditionalUnlockUser(userIdInt)

		utils.SendMessage(b, ctx, userIdInt, tolgee.GetTranslation("telegrambot.conversation.rejectedapproval", "it"))
	}
}
