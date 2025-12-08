package utils

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

func GenerateCallmeUrlEndpoint(userId int64) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	code := strings.Builder{}
	code.WriteString(fmt.Sprintf("%d_", userId))

	charSet := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	for range 15 {
		code.WriteByte(charSet[r.Intn(len(charSet))])
	}

	return code.String()
}
