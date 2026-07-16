package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// capturedRequest records everything about an inbound request that the tests assert on.
type capturedRequest struct {
	method      string
	path        string
	authHeader  string
	contentType string
	form        url.Values
	rawBody     string
}

// newCapturingServer returns an httptest server that records the request into `into` and
// replies with the given status and body.
func newCapturingServer(t *testing.T, status int, body string, into *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		into.method = r.Method
		into.path = r.URL.Path
		into.authHeader = r.Header.Get("Authorization")
		into.contentType = r.Header.Get("Content-Type")
		into.rawBody = string(raw)
		if form, err := url.ParseQuery(string(raw)); err == nil {
			into.form = form
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

// TestCheckMembershipValidityDecodesSnakeCase is the G04 regression guard: a realistic
// Mensa API response uses snake_case `is_membership_active`. If the DTO tag ever regresses
// to camelCase this decodes to false and the cleanup sweep would kick the whole membership.
func TestCheckMembershipValidityDecodesSnakeCase(t *testing.T) {
	var cap capturedRequest
	// The exact wire shape the real /valid endpoint emits.
	const realistic = `{"username":"mario.rossi","name":"Mario Rossi","is_membership_active":true}`
	srv := newCapturingServer(t, http.StatusOK, realistic, &cap)
	defer srv.Close()

	t.Setenv("API_ENDPOINT", srv.URL)
	t.Setenv("API_BEARER_TOKEN", "s3cr3t-token")

	client := NewMensaAPIClient()
	resp, err := client.CheckMembershipValidity(context.Background(), MemberNumber(12345))
	if err != nil {
		t.Fatalf("CheckMembershipValidity returned error: %v", err)
	}

	// THE field that would have kicked everyone: it must actually populate true.
	if !resp.IsMembershipActive {
		t.Fatalf("is_membership_active did not decode to true; got %+v — the json tag has regressed", resp)
	}
	if resp.Username != "mario.rossi" || resp.Name != "Mario Rossi" {
		t.Fatalf("username/name did not decode: %+v", resp)
	}

	// Assert the outgoing request is shaped correctly.
	if cap.method != http.MethodPost {
		t.Errorf("method = %q, want POST", cap.method)
	}
	if cap.path != "/valid" {
		t.Errorf("path = %q, want /valid", cap.path)
	}
	if cap.authHeader != "Bearer s3cr3t-token" {
		t.Errorf("Authorization = %q, want %q", cap.authHeader, "Bearer s3cr3t-token")
	}
	if !strings.HasPrefix(cap.contentType, "application/x-www-form-urlencoded") {
		t.Errorf("Content-Type = %q, want form-urlencoded", cap.contentType)
	}
	if got := cap.form.Get("member_id"); got != "12345" {
		t.Errorf("member_id form field = %q, want 12345", got)
	}
}

// TestCheckMembershipValidityCamelCaseWouldDecodeFalse documents WHY the snake_case tag
// matters: the exact same payload with a camelCase key does NOT populate the field. This is
// the bug the production DTO avoids.
func TestCheckMembershipValidityCamelCaseWouldDecodeFalse(t *testing.T) {
	const camel = `{"username":"mario.rossi","name":"Mario Rossi","isMembershipActive":true}`
	var got CheckValidityResponse
	if err := json.Unmarshal([]byte(camel), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.IsMembershipActive {
		t.Fatal("a camelCase payload should NOT populate the snake_case-tagged field; " +
			"if this passes the DTO tag is camelCase and the sweep is a massacre")
	}
}

func TestCheckMembershipValidityInactive(t *testing.T) {
	var cap capturedRequest
	srv := newCapturingServer(t, http.StatusOK,
		`{"username":"gino.verdi","name":"Gino Verdi","is_membership_active":false}`, &cap)
	defer srv.Close()

	t.Setenv("API_ENDPOINT", srv.URL)
	t.Setenv("API_BEARER_TOKEN", "tok")

	resp, err := NewMensaAPIClient().CheckMembershipValidity(context.Background(), MemberNumber(7))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsMembershipActive {
		t.Fatal("expected inactive membership")
	}
}

func TestCheckMembershipValidityNon200IsError(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusNotFound, http.StatusUnauthorized} {
		var cap capturedRequest
		srv := newCapturingServer(t, status, "boom", &cap)
		t.Setenv("API_ENDPOINT", srv.URL)
		t.Setenv("API_BEARER_TOKEN", "tok")

		resp, err := NewMensaAPIClient().CheckMembershipValidity(context.Background(), MemberNumber(1))
		if err == nil {
			t.Errorf("status %d: expected an error so the cleanup sweep fails closed, got nil (resp=%+v)", status, resp)
		}
		if resp != nil {
			t.Errorf("status %d: expected nil response on error, got %+v", status, resp)
		}
		srv.Close()
	}
}

func TestCheckMembershipValidityMalformedJSONIsError(t *testing.T) {
	var cap capturedRequest
	srv := newCapturingServer(t, http.StatusOK, `{not json`, &cap)
	defer srv.Close()
	t.Setenv("API_ENDPOINT", srv.URL)
	t.Setenv("API_BEARER_TOKEN", "tok")

	_, err := NewMensaAPIClient().CheckMembershipValidity(context.Background(), MemberNumber(1))
	if err == nil {
		t.Fatal("expected a decode error on malformed JSON")
	}
}

func TestCheckMembershipValidityNoEndpointIsError(t *testing.T) {
	t.Setenv("API_ENDPOINT", "")
	t.Setenv("API_BEARER_TOKEN", "tok")
	_, err := NewMensaAPIClient().CheckMembershipValidity(context.Background(), MemberNumber(1))
	if err == nil {
		t.Fatal("expected an error when API_ENDPOINT is unset")
	}
}

// TestRequestVerificationBodyAndHeaders asserts the registration-time /request call carries
// the email, member number and callme URL, plus the Bearer token.
func TestRequestVerificationBodyAndHeaders(t *testing.T) {
	var cap capturedRequest
	srv := newCapturingServer(t, http.StatusOK, `{}`, &cap)
	defer srv.Close()

	t.Setenv("API_ENDPOINT", srv.URL+"/") // trailing slash must be trimmed
	t.Setenv("API_BEARER_TOKEN", "bearer-xyz")

	err := NewMensaAPIClient().RequestVerification(context.Background(),
		"mario.rossi@mensa.it", MemberNumber(42), "https://bot.example/callme_api/42_abcdefghijklmno")
	if err != nil {
		t.Fatalf("RequestVerification returned error: %v", err)
	}

	if cap.method != http.MethodPost {
		t.Errorf("method = %q, want POST", cap.method)
	}
	if cap.path != "/request" {
		t.Errorf("path = %q, want /request", cap.path)
	}
	if cap.authHeader != "Bearer bearer-xyz" {
		t.Errorf("Authorization = %q, want Bearer bearer-xyz", cap.authHeader)
	}
	if got := cap.form.Get("email"); got != "mario.rossi@mensa.it" {
		t.Errorf("email = %q", got)
	}
	if got := cap.form.Get("member_id"); got != "42" {
		t.Errorf("member_id = %q, want 42", got)
	}
	if got := cap.form.Get("callme_url"); got != "https://bot.example/callme_api/42_abcdefghijklmno" {
		t.Errorf("callme_url = %q", got)
	}
}

func TestRequestVerificationNon200IsError(t *testing.T) {
	var cap capturedRequest
	srv := newCapturingServer(t, http.StatusForbidden, "nope", &cap)
	defer srv.Close()
	t.Setenv("API_ENDPOINT", srv.URL)
	t.Setenv("API_BEARER_TOKEN", "tok")

	err := NewMensaAPIClient().RequestVerification(context.Background(), "a.b@mensa.it", MemberNumber(1), "u")
	if err == nil {
		t.Fatal("expected an error on non-200 from /request")
	}
}

func TestMemberNumberString(t *testing.T) {
	if MemberNumber(42).String() != "42" {
		t.Fatalf("MemberNumber.String() = %q", MemberNumber(42).String())
	}
}
