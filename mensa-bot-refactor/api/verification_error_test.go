package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Telling a real member "no such member" because the API was down is both a lie and
// unactionable — they would re-check details that were correct all along. Only a positive
// rejection from the API may surface as ErrMemberNotFound.
func TestRequestVerificationDistinguishesNotFoundFromUnavailable(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		wantNotFound bool
		wantErr      bool
	}{
		{name: "200 means verified", status: http.StatusOK, wantNotFound: false, wantErr: false},

		// Definitive: the API looked and says no.
		{name: "404 is a real negative", status: http.StatusNotFound, wantNotFound: true, wantErr: true},
		{name: "400 is a real negative", status: http.StatusBadRequest, wantNotFound: true, wantErr: true},

		// Not definitive: we never got an answer about the member.
		{name: "500 is not a negative", status: http.StatusInternalServerError, wantNotFound: false, wantErr: true},
		{name: "502 is not a negative", status: http.StatusBadGateway, wantNotFound: false, wantErr: true},
		{name: "429 rate limit is not a negative", status: http.StatusTooManyRequests, wantNotFound: false, wantErr: true},
		{name: "408 timeout is not a negative", status: http.StatusRequestTimeout, wantNotFound: false, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			t.Setenv("API_ENDPOINT", srv.URL)
			t.Setenv("API_BEARER_TOKEN", "test-token")

			err := NewMensaAPIClient().RequestVerification(
				context.Background(), "mario.rossi@mensa.it", MemberNumber(12345), "https://example.test/callme_api/1_abc")

			if tc.wantErr && err == nil {
				t.Fatalf("status %d: expected an error, got nil", tc.status)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("status %d: unexpected error: %v", tc.status, err)
			}
			if got := errors.Is(err, ErrMemberNotFound); got != tc.wantNotFound {
				t.Errorf("status %d: errors.Is(err, ErrMemberNotFound) = %v, want %v (err=%v)",
					tc.status, got, tc.wantNotFound, err)
			}
		})
	}
}

// A DNS failure / connection refused is the case that actually bit us: the endpoint was a
// placeholder that did not resolve, and every user was told they were not a Mensa member.
func TestRequestVerificationTransportFailureIsNotNotFound(t *testing.T) {
	// Point at a closed port on localhost: connection refused, no HTTP response at all.
	t.Setenv("API_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("API_BEARER_TOKEN", "test-token")

	err := NewMensaAPIClient().RequestVerification(
		context.Background(), "mario.rossi@mensa.it", MemberNumber(12345), "https://example.test/callme_api/1_abc")

	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}
	if errors.Is(err, ErrMemberNotFound) {
		t.Fatalf("an unreachable API must NOT report the member as non-existent, got: %v", err)
	}
}

// Same for a completely unset endpoint.
func TestRequestVerificationMissingEndpointIsNotNotFound(t *testing.T) {
	t.Setenv("API_ENDPOINT", "")

	err := NewMensaAPIClient().RequestVerification(
		context.Background(), "mario.rossi@mensa.it", MemberNumber(12345), "https://example.test/callme_api/1_abc")

	if err == nil {
		t.Fatal("expected an error when API_ENDPOINT is unset")
	}
	if errors.Is(err, ErrMemberNotFound) {
		t.Fatalf("a misconfigured endpoint must NOT report the member as non-existent, got: %v", err)
	}
}
