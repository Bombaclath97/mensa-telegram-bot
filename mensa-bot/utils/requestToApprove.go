package utils

type RequestsToApprove map[int64][]int64

func (r *RequestsToApprove) AddRequest(userID, chatID int64) {
	(*r)[userID] = append((*r)[userID], chatID)
}

func (r *RequestsToApprove) RemoveRequests(userID int64) {
	delete(*r, userID)
}

func (r *RequestsToApprove) GetRequests(userID int64) ([]int64, bool) {
	chatID, ok := (*r)[userID]
	return chatID, ok
}
