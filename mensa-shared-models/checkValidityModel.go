package model

type CheckValidity struct {
	Username           string `json:"username"`
	Name               string `json:"name"`
	IsMembershipActive bool   `json:"is_membership_active"`
}
