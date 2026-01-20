package utils

import (
	"regexp"
	"strings"
)

// IsValidMensaEmail validates email format for Mensa Italy
// Accepts format: nome.cognome@mensa.it or nome.cognome123@mensa.it
func IsValidMensaEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))

	if !strings.HasSuffix(email, "@mensa.it") {
		return false
	}

	regex := `^[a-z]+\.[a-z]+[0-9]{0,2}@mensa\.it$`
	re := regexp.MustCompile(regex)
	return re.MatchString(email)
}
