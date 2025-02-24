package utils

type RequestsToApprove map[int64]int64

func (r *RequestsToApprove) AddRequest(chatID, userID int64) {
	(*r)[chatID] = userID
}

func (r *RequestsToApprove) RemoveRequest(chatID int64) {
	delete(*r, chatID)
}

func (r *RequestsToApprove) GetRequest(chatID int64) (int64, bool) {
	userID, ok := (*r)[chatID]
	return userID, ok
}
