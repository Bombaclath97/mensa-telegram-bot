package utils

type LockedUsers map[int64]string

func (l *LockedUsers) LockUser(userID int64, key string) {
	(*l)[userID] = key
}

func (l *LockedUsers) UnlockUser(userID int64, key string) bool {
	if (*l)[userID] == key {
		delete(*l, userID)
		return true
	}
	return false
}

func (l *LockedUsers) UnconditionalUnlockUser(userID int64) {
	delete(*l, userID)
}

func (l *LockedUsers) IsUserLocked(userID int64) bool {
	_, ok := (*l)[userID]
	return ok
}
