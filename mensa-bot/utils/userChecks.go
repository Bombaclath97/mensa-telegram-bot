package utils

import (
	"fmt"
	"io"
	"log"
	"net/http"
)

func IsMemberRegistered(userID int64) bool {
	resp, err := http.Get("http://localhost:8080/members/" + fmt.Sprint(userID))
	if err != nil {
		log.Fatalln(err)
	}

	defer resp.Body.Close()

	// If the user is not found, return false
	return resp.StatusCode == http.StatusOK
}

func GetMember(userID int64) ([]byte, error) {
	resp, err := http.Get("http://localhost:8080/members/" + fmt.Sprint(userID))
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("member not found")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}
