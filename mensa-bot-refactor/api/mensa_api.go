package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// CheckValidityResponse represents the response from the Mensa API validity check
type CheckValidityResponse struct {
	Username           string `json:"username"`
	Name               string `json:"name"`
	IsMembershipActive bool   `json:"isMembershipActive"`
}

// MensaAPIClient handles communication with the Mensa Italia API
type MensaAPIClient struct {
	endpoint    string
	bearerToken string
	httpClient  *http.Client
}

// NewMensaAPIClient creates a new Mensa API client
func NewMensaAPIClient() *MensaAPIClient {
	return &MensaAPIClient{
		endpoint:    os.Getenv("API_ENDPOINT"),
		bearerToken: os.Getenv("API_BEARER_TOKEN"),
		httpClient:  &http.Client{},
	}
}

// CheckMembershipValidity checks if a membership number is valid and active
func (c *MensaAPIClient) CheckMembershipValidity(memberID string) (*CheckValidityResponse, error) {
	data := url.Values{
		"member_id": {memberID},
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/valid", c.endpoint), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.bearerToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result CheckValidityResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// RequestVerification sends a verification request to the Mensa API
// This triggers a push notification in the Mensa app for the user to confirm
func (c *MensaAPIClient) RequestVerification(email, memberID, callmeURL string) error {
	data := url.Values{
		"email":      {email},
		"member_id":  {memberID},
		"callme_url": {callmeURL},
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/request", c.endpoint), strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.bearerToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	return nil
}
