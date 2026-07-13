package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout bounds every call to the Mensa Italia API.
const DefaultTimeout = 15 * time.Second

// MemberNumber is a Mensa membership number.
//
// It is a DISTINCT type on purpose: the legacy cleanup routine passed a Telegram ID
// where a member number was expected, which made the API report every member as
// inactive and would have kicked + deleted the entire membership. Passing a Telegram
// ID here is now a compile error.
type MemberNumber int64

func (m MemberNumber) String() string {
	return strconv.FormatInt(int64(m), 10)
}

// CheckValidityResponse is the response of POST {API_ENDPOINT}/valid.
// The field names are snake_case: this is what the real API emits. A camelCase tag
// here would silently decode IsMembershipActive as false for EVERY member, which the
// cleanup sweep would read as "everyone expired" — so the tag must stay snake_case.
type CheckValidityResponse struct {
	Username           string `json:"username"`
	Name               string `json:"name"`
	IsMembershipActive bool   `json:"is_membership_active"`
}

// MensaAPIClient handles communication with the Mensa Italia API
type MensaAPIClient struct {
	endpoint    string
	bearerToken string
	httpClient  *http.Client
}

// NewMensaAPIClient creates a new Mensa API client from API_ENDPOINT and API_BEARER_TOKEN.
func NewMensaAPIClient() *MensaAPIClient {
	return &MensaAPIClient{
		endpoint:    strings.TrimRight(os.Getenv("API_ENDPOINT"), "/"),
		bearerToken: os.Getenv("API_BEARER_TOKEN"),
		httpClient:  &http.Client{Timeout: DefaultTimeout},
	}
}

func (c *MensaAPIClient) post(ctx context.Context, path string, form url.Values) (*http.Response, error) {
	if c.endpoint == "" {
		return nil, fmt.Errorf("mensa api: API_ENDPOINT is not set")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/%s", c.endpoint, path), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("mensa api: failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.bearerToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mensa api: request to /%s failed: %w", path, err)
	}
	return resp, nil
}

// CheckMembershipValidity asks the Mensa API whether the given MEMBERSHIP NUMBER
// (never a Telegram ID) corresponds to an active membership.
// It returns an error rather than a false negative when the API cannot be reached: the
// caller (the cleanup sweep) MUST NOT treat an error as "membership expired".
func (c *MensaAPIClient) CheckMembershipValidity(ctx context.Context, memberNumber MemberNumber) (*CheckValidityResponse, error) {
	resp, err := c.post(ctx, "valid", url.Values{"member_id": {memberNumber.String()}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mensa api: /valid returned status %d", resp.StatusCode)
	}

	var result CheckValidityResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("mensa api: failed to decode /valid response: %w", err)
	}

	return &result, nil
}

// IsMembershipActive is a convenience wrapper over CheckMembershipValidity.
// The bool is only meaningful when err == nil.
func (c *MensaAPIClient) IsMembershipActive(ctx context.Context, memberNumber MemberNumber) (bool, error) {
	result, err := c.CheckMembershipValidity(ctx, memberNumber)
	if err != nil {
		return false, err
	}
	return result.IsMembershipActive, nil
}

// ErrMemberNotFound means the Mensa API gave a DEFINITIVE answer: no member exists with
// that (email, membership number) pair.
//
// It exists to separate "we know you are not a member" from "we could not ask". Collapsing
// the two — as the code did before — tells a real member their email and membership number
// are wrong whenever the API is merely unreachable, which is both untrue and unactionable.
// Only this error may be reported to the user as "no such member".
var ErrMemberNotFound = errors.New("mensa api: no member matches that email and membership number")

// RequestVerification asks the Mensa API to push an approval request to the member's
// Mensa app. HTTP 200 means the (email, member number) pair exists and the push was sent;
// the app then calls callmeURL back with {"accepted": bool}.
//
// Returns ErrMemberNotFound only when the API positively rejects the pair (a 4xx). A
// transport failure or a 5xx returns a plain error: the caller must treat that as "try
// again later", never as "you are not a member".
func (c *MensaAPIClient) RequestVerification(ctx context.Context, email string, memberNumber MemberNumber, callmeURL string) error {
	resp, err := c.post(ctx, "request", url.Values{
		"email":      {email},
		"member_id":  {memberNumber.String()},
		"callme_url": {callmeURL},
	})
	if err != nil {
		return err // transport: DNS, timeout, connection refused — NOT a negative answer
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// 4xx is the API telling us the pair is not a member. 408/429 are about the request
	// itself, not the member, so they are retryable rather than definitive.
	if resp.StatusCode >= 400 && resp.StatusCode < 500 &&
		resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests {
		return fmt.Errorf("%w (status %d)", ErrMemberNotFound, resp.StatusCode)
	}

	return fmt.Errorf("mensa api: /request returned status %d", resp.StatusCode)
}
