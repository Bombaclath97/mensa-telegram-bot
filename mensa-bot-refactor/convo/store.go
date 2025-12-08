package convo

import (
	"gorm.io/gorm"
)

type DBStore struct {
	db *gorm.DB
}

func NewDBStore(db *gorm.DB) Store {
	return &DBStore{db: db}
}

func (s *DBStore) Load(userID int64) (*ConvoContext, error) {
	var ctx ConvoContext
	err := s.db.Where("user_id = ?", userID).First(&ctx).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &ctx, nil
}

func (s *DBStore) Save(ctx *ConvoContext) error {
	return s.db.Save(ctx).Error
}

func (s *DBStore) Clear(userID int64) error {
	return s.db.Where("user_id = ?", userID).Delete(&ConvoContext{}).Error
}
