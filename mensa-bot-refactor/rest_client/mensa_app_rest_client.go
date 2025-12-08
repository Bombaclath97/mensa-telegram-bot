package restclient

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/Bombaclath97/bomba-go-utils/env"
	"github.com/Bombaclath97/mensa-telegram-bot/utils"
)

var client = &http.Client{}

func CheckIfIsMemberAndSendCallmeURL(email, membership string, telegramId int64) (bool, string) {
	selfBaseUrl := env.GetEnvOrDefault("SELF_BASE_URL", "localhost:3000")
	userToken := utils.GenerateCallmeUrlEndpoint(telegramId)

	mensaApiEndpoint := env.GetEnvOrDefault("MENSA_APP_ENDPOINT", "localhost:3001")

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/request", mensaApiEndpoint), bytes.NewBufferString(url.Values{
		"email":      {email},
		"member_id":  {membership},
		"callme_url": {fmt.Sprintf("%s/%s", selfBaseUrl, userToken)},
	}.Encode()))
	if err != nil {
		log.Printf("ERROR: Failed to create request: %v", err)
		return false, ""
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", env.GetEnvOrDefault("MENSA_APP_API_TOKEN", "")))

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("ERROR: Failed to send request: %v", err)
		return false, ""
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, userToken
}
