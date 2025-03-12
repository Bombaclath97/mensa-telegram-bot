package utils

import (
	"fmt"
	"math/rand"
	"regexp"
	"time"
)

func IsValidEmail(email string) bool {
	regex := `^[a-zA-Z]+\.[a-zA-Z]+[0-9]{0,2}@mensa\.it$`
	re := regexp.MustCompile(regex)
	return re.MatchString(email)
}

func GenerateCallmeUrlEndpoint(userId string) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	code := fmt.Sprintf("%s_", userId)

	charSet := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	for range 15 {
		code += string(charSet[r.Intn(len(charSet))])
	}

	return code
}
