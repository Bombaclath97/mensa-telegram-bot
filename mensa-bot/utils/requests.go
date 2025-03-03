package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"

	"git.bombaclath.cc/bombaclath97/mensa-bot-telegram/bot/model"
)

func IsMemberRegistered(userID int64) bool {
	resp, err := http.Get("http://localhost:8080/members/" + fmt.Sprint(userID))
	if err != nil {
		log.Fatalln(err)
	}

	defer resp.Body.Close()

	// If the user is not found, return false
	return resp.StatusCode == http.StatusOK
}

func GetMember(userID int64) ([]byte, error) {
	resp, err := http.Get("http://localhost:8080/members/" + fmt.Sprint(userID))
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("member not found")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func RegisterMember(userID int64, firstName, lastName, email string) int {
	member := model.User{
		TelegramID: userID,
		FirstName:  firstName,
		LastName:   lastName,
		MensaEmail: email,
	}

	reqBody, _ := json.Marshal(member)

	resp, err := http.Post("http://localhost:8080/members", "application/json", io.NopCloser(bytes.NewReader(reqBody)))
	if err != nil {
		return http.StatusInternalServerError
	}

	defer resp.Body.Close()

	return resp.StatusCode
}

func LookupEmail(email string) (int, error) {
	api_endpoint := os.Getenv("MENSA_APP_API_URL") + "/members?email=" + url.QueryEscape(email)
	resp, err := http.Get(api_endpoint)

	return resp.StatusCode, err
}
