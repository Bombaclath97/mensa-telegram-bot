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

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/model"
)

var crudEndpoint = os.Getenv("CRUD_ENDPOINT")

func IsMemberRegistered(userID int64) bool {
	resp, err := http.Get("http://" + crudEndpoint + "/members/" + fmt.Sprint(userID))
	if err != nil {
		log.Fatalln(err)
	}

	defer resp.Body.Close()

	// If the user is not found, return false
	return resp.StatusCode == http.StatusOK
}

func GetMember(userID int64) ([]byte, error) {
	resp, err := http.Get("http://" + crudEndpoint + "/members/" + fmt.Sprint(userID))
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

func RegisterMember(userID int64, user model.User) int {
	reqBody, _ := json.Marshal(user)

	resp, err := http.Post("http://"+crudEndpoint+"/members", "application/json", io.NopCloser(bytes.NewReader(reqBody)))
	if err != nil {
		return http.StatusInternalServerError
	}

	defer resp.Body.Close()

	return resp.StatusCode
}

func EmailExistsInDatabase(email string) (bool, error) {

	resp, err := http.Get("http://" + crudEndpoint + "/members/email/" + url.QueryEscape(email))
	if err != nil {
		log.Fatalln(err)
	}

	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

func IsMember(email, membership string) bool {
	req, err := http.NewRequest("POST", os.Getenv("API_ENDPOINT"), bytes.NewBufferString(url.Values{
		"email":     {email},
		"member_id": {membership},
	}.Encode()))
	if err != nil {
		log.Fatalln(err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("API_BEARER_TOKEN")))
	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatalln(err)
	}

	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
