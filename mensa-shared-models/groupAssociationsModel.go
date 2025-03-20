package model

type GroupAssociations struct {
	UserID       int  `json:"user_id"`
	GroupID      int  `json:"group_id"`
	IsGroupAdmin bool `json:"is_group_admin"`
}
