package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
)

const callmeTokenCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
const callmeTokenSuffixLen = 15

// GenerateCallmeUrlEndpoint builds the callme token for a user: '<telegramID>_<15 random chars>'.
// The random suffix is the shared secret that the Mensa app must present to unlock the
// registration, so it is drawn from a cryptographic source (the legacy used math/rand
// seeded with the clock, which is guessable).
func GenerateCallmeUrlEndpoint(userID int64) string {
	var code strings.Builder
	code.WriteString(fmt.Sprintf("%d_", userID))

	max := big.NewInt(int64(len(callmeTokenCharset)))
	for range callmeTokenSuffixLen {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			// crypto/rand failing is unrecoverable; there is no safe fallback for a secret.
			panic(fmt.Sprintf("failed to generate callme token: %v", err))
		}
		code.WriteByte(callmeTokenCharset[n.Int64()])
	}

	return code.String()
}

// ParseCallmeToken extracts the Telegram user ID from a callme token.
// It only validates the shape; the caller MUST still token-match the full string against
// the draft (model.Drafts.UnlockUser) — the ID prefix is user-controlled, not a secret.
func ParseCallmeToken(token string) (int64, error) {
	idPart, suffix, found := strings.Cut(token, "_")
	if !found || len(suffix) != callmeTokenSuffixLen {
		return 0, fmt.Errorf("malformed callme token")
	}
	userID, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("malformed callme token: %w", err)
	}
	return userID, nil
}

// BuildCallmeURL returns the absolute URL the Mensa app must call back, i.e.
// {CALLME_BASE_URL}/{token}.
func BuildCallmeURL(token string) string {
	base := strings.TrimRight(os.Getenv("CALLME_BASE_URL"), "/")
	return fmt.Sprintf("%s/%s", base, token)
}
