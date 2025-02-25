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

func GenerateConfirmationCode() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	code := ""

	for range 6 {
		code += fmt.Sprint(r.Intn(10) + 48)
	}

	return code
}
