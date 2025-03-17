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

	model "git.bombaclath.cc/bombadurelli/mensa-bot-telegram/mensa-shared-models"
)

var crudEndpoint = os.Getenv("CRUD_ENDPOINT")

func IsMemberRegistered(userID int64) bool {
	resp, err := http.Get("http://" + crudEndpoint + "/members/" + fmt.Sprint(userID))
	if err != nil {
		log.Printf("ERROR: Failed to check if member is registered: %v", err)
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func GetMember(userID int64) (model.User, error) {
	resp, err := http.Get("http://" + crudEndpoint + "/members/" + fmt.Sprint(userID))
	if err != nil {
		return model.User{}, fmt.Errorf("failed to get member: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return model.User{}, fmt.Errorf("member not found")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.User{}, fmt.Errorf("failed to read response body: %w", err)
	}

	var user model.User
	err = json.Unmarshal(body, &user)
	if err != nil {
		return model.User{}, fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	return user, nil
}

func RegisterMember(userID int64, user model.User) int {
	reqBody, err := json.Marshal(user)
	if err != nil {
		log.Printf("ERROR: Failed to marshal user: %v", err)
		return http.StatusInternalServerError
	}

	resp, err := http.Post("http://"+crudEndpoint+"/members", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("ERROR: Failed to register member: %v", err)
		return http.StatusInternalServerError
	}
	defer resp.Body.Close()

	return resp.StatusCode
}

func EmailExistsInDatabase(email string) (bool, error) {
	resp, err := http.Get("http://" + crudEndpoint + "/members/email/" + url.QueryEscape(email))
	if err != nil {
		return false, fmt.Errorf("failed to check if email exists: %w", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

func CheckIfIsMemberAndSendCallmeURL(email, membership string, telegramId int64) (bool, string) {
	callmeBaseUrl := os.Getenv("CALLME_BASE_URL")
	userToken := GenerateCallmeUrlEndpoint(telegramId)

	mensaApiEndpoint := os.Getenv("API_ENDPOINT")

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/request", mensaApiEndpoint), bytes.NewBufferString(url.Values{
		"email":      {email},
		"member_id":  {membership},
		"callme_url": {fmt.Sprintf("%s/%s", callmeBaseUrl, userToken)},
	}.Encode()))
	if err != nil {
		log.Printf("ERROR: Failed to create request: %v", err)
		return false, ""
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("API_BEARER_TOKEN")))
	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("ERROR: Failed to send request: %v", err)
		return false, ""
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, userToken
}

func RegisterGroupForUser(userId, chatId int64) int {
	group := model.Group{
		GroupID: int(chatId),
		UserID:  int(userId),
	}

	reqBody, err := json.Marshal(group)
	if err != nil {
		log.Printf("ERROR: Failed to marshal group: %v", err)
		return http.StatusInternalServerError
	}

	resp, err := http.Post(fmt.Sprintf("http://%s/groups", crudEndpoint), "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("ERROR: Failed to register group for user: %v", err)
		return http.StatusInternalServerError
	}
	defer resp.Body.Close()

	return resp.StatusCode
}

func GetGroupsForUser(userId int64) ([]model.Group, error) {
	resp, err := http.Get(fmt.Sprintf("http://%s/groups/%d", crudEndpoint, userId))
	if err != nil {
		return nil, fmt.Errorf("failed to get groups for user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("no groups found for user")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var groups []model.Group
	err = json.Unmarshal(body, &groups)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	return groups, nil
}

func DeleteMember(userId int64) int {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("http://%s/members/%d", crudEndpoint, userId), nil)
	if err != nil {
		log.Printf("ERROR: Failed to create request: %v", err)
		return http.StatusInternalServerError
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("ERROR: Failed to delete member: %v", err)
		return http.StatusInternalServerError
	}
	defer resp.Body.Close()

	return resp.StatusCode
}
