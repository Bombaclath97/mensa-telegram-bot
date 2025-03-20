package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	model "git.bombaclath.cc/bombadurelli/mensa-bot-telegram/mensa-shared-models"
	"github.com/Bombaclath97/bomba-go-utils/logger"
)

var crudEndpoint = os.Getenv("CRUD_ENDPOINT")
var log = logger.Configure("mensa-bot")

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
		return model.User{}, fmt.Errorf("member not found, status code: %d", resp.StatusCode)
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

func RegisterMember(user model.User) int {
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

func IsMembershipActive(intMembership int64) bool {
	mensaApiEndpoint := os.Getenv("API_ENDPOINT")

	membership := fmt.Sprintf("%d", intMembership)

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/valid", mensaApiEndpoint), bytes.NewBufferString(url.Values{
		"member_id": {membership},
	}.Encode()))

	if err != nil {
		log.Printf("ERROR: Failed to create request: %v", err)
		return false
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("API_BEARER_TOKEN")))
	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("ERROR: Failed to send request: %v", err)
		return false
	}
	defer resp.Body.Close()

	var checkValidity model.CheckValidity
	err = json.NewDecoder(resp.Body).Decode(&checkValidity)
	if err != nil {
		log.Printf("ERROR: Failed to decode response body: %v", err)
		return false
	}

	return checkValidity.IsMembershipActive
}

func RegisterGroupForUser(userId, chatId int64) int {
	group := model.GroupAssociations{
		GroupID: int(chatId),
		UserID:  int(userId),
	}

	reqBody, err := json.Marshal(group)
	if err != nil {
		log.Printf("ERROR: Failed to marshal group: %v", err)
		return http.StatusInternalServerError
	}

	resp, err := http.Post(fmt.Sprintf("http://%s/groups/associations", crudEndpoint), "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("ERROR: Failed to register group for user: %v", err)
		return http.StatusInternalServerError
	}
	defer resp.Body.Close()

	return resp.StatusCode
}

func GetGroupsForUser(userId int64) ([]model.GroupAssociations, error) {
	resp, err := http.Get(fmt.Sprintf("http://%s/groups/associations/%d", crudEndpoint, userId))
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

	var groups []model.GroupAssociations
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

func GetAllMembers() []model.User {
	var users []model.User

	resp, err := http.Get("http://" + crudEndpoint + "/members")
	if err != nil {
		log.Printf("ERROR: Failed to get all users: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("ERROR: Failed to get all users: %v", err)
		return nil
	}

	err = json.NewDecoder(resp.Body).Decode(&users)

	if err != nil {
		log.Printf("ERROR: Failed to decode response body: %v", err)
		return nil
	}

	return users
}

func IsUserBotAdministrator(userId int64) bool {
	user, _ := GetMember(userId)
	return user.IsBotAdmin
}

func RegisterBotGroup(chatId int64) int {
	group := model.Group{
		GroupID: int(chatId),
	}

	reqBody, err := json.Marshal(group)
	if err != nil {
		log.Printf("ERROR: Failed to marshal group: %v", err)
		return http.StatusInternalServerError
	}

	resp, err := http.Post(fmt.Sprintf("http://%s/groups/", crudEndpoint), "application/json", bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("ERROR: Failed to register group for user: %v", err)
		return http.StatusInternalServerError
	}
	defer resp.Body.Close()

	return resp.StatusCode
}
