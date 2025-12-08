package convo

import "sync"

var (
	globalStore Store
	storeOnce   sync.Once
)

func InitGlobalStore(store Store) {
	storeOnce.Do(func() {
		globalStore = store
	})
}

func GetConvoContext(userID int64) (*ConvoContext, error) {
	if globalStore == nil {
		return nil, nil
	}
	return globalStore.Load(userID)
}

func SaveConvoContext(ctx *ConvoContext) error {
	if globalStore == nil {
		return nil
	}
	return globalStore.Save(ctx)
}

func ClearConvoContext(userID int64) error {
	if globalStore == nil {
		return nil
	}
	return globalStore.Clear(userID)
}

func UpdateConvoContext(userID int64, updateFunc func(*ConvoContext) error) error {
	ctx, err := GetConvoContext(userID)
	if err != nil {
		return err
	}
	if ctx == nil {
		return nil
	}
	if err := updateFunc(ctx); err != nil {
		return err
	}
	return SaveConvoContext(ctx)
}
