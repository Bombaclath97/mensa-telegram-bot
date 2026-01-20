package database

import (
	"errors"

	"gorm.io/gorm"
)

// UserRepository handles all user-related database operations
type UserRepository struct{}

func NewUserRepository() *UserRepository {
	return &UserRepository{}
}

// GetByID retrieves a user by their Telegram ID
func (r *UserRepository) GetByID(telegramID int64) (*User, error) {
	var user User
	result := Get().First(&user, "telegram_id = ?", telegramID)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, result.Error
	}
	return &user, nil
}

// GetByEmail retrieves a user by their Mensa email
func (r *UserRepository) GetByEmail(email string) (*User, error) {
	var user User
	result := Get().First(&user, "mensa_email = ?", email)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, result.Error
	}
	return &user, nil
}

// GetAll retrieves all users
func (r *UserRepository) GetAll() ([]User, error) {
	var users []User
	result := Get().Find(&users)
	return users, result.Error
}

// Create creates a new user
func (r *UserRepository) Create(user *User) error {
	return Get().Create(user).Error
}

// Update updates an existing user
func (r *UserRepository) Update(user *User) error {
	return Get().Save(user).Error
}

// Delete deletes a user by their Telegram ID
func (r *UserRepository) Delete(telegramID int64) error {
	return Get().Delete(&User{}, "telegram_id = ?", telegramID).Error
}

// Exists checks if a user exists by their Telegram ID
func (r *UserRepository) Exists(telegramID int64) (bool, error) {
	var count int64
	result := Get().Model(&User{}).Where("telegram_id = ?", telegramID).Count(&count)
	return count > 0, result.Error
}

// ExistsByEmail checks if a user exists by their email
func (r *UserRepository) ExistsByEmail(email string) (bool, error) {
	var count int64
	result := Get().Model(&User{}).Where("mensa_email = ?", email).Count(&count)
	return count > 0, result.Error
}

// UpdateLanguage updates the user's preferred language
func (r *UserRepository) UpdateLanguage(telegramID int64, language string) error {
	return Get().Model(&User{}).
		Where("telegram_id = ?", telegramID).
		Update("language", language).Error
}

// GetLanguage retrieves a user's language preference, returning default "it" if not found
func (r *UserRepository) GetLanguage(telegramID int64) string {
	user, err := r.GetByID(telegramID)
	if err != nil || user == nil {
		return DefaultLanguage
	}
	return user.GetLanguage()
}

// GroupRepository handles all group-related database operations
type GroupRepository struct{}

func NewGroupRepository() *GroupRepository {
	return &GroupRepository{}
}

// GetByID retrieves a group by its ID
func (r *GroupRepository) GetByID(groupID int64) (*Group, error) {
	var group Group
	result := Get().First(&group, "group_id = ?", groupID)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, result.Error
	}
	return &group, nil
}

// GetAll retrieves all groups
func (r *GroupRepository) GetAll() ([]Group, error) {
	var groups []Group
	result := Get().Find(&groups)
	return groups, result.Error
}

// Create creates a new group
func (r *GroupRepository) Create(group *Group) error {
	return Get().Create(group).Error
}

// Delete deletes a group by its ID
func (r *GroupRepository) Delete(groupID int64) error {
	return Get().Delete(&Group{}, "group_id = ?", groupID).Error
}

// Exists checks if a group exists
func (r *GroupRepository) Exists(groupID int64) (bool, error) {
	var count int64
	result := Get().Model(&Group{}).Where("group_id = ?", groupID).Count(&count)
	return count > 0, result.Error
}

// GroupAssociationRepository handles user-group associations
type GroupAssociationRepository struct{}

func NewGroupAssociationRepository() *GroupAssociationRepository {
	return &GroupAssociationRepository{}
}

// GetUserGroups retrieves all group associations for a user
func (r *GroupAssociationRepository) GetUserGroups(userID int64) ([]GroupAssociation, error) {
	var associations []GroupAssociation
	result := Get().Where("user_id = ?", userID).Find(&associations)
	return associations, result.Error
}

// GetGroupUsers retrieves all user associations for a group
func (r *GroupAssociationRepository) GetGroupUsers(groupID int64) ([]GroupAssociation, error) {
	var associations []GroupAssociation
	result := Get().Where("group_id = ?", groupID).Find(&associations)
	return associations, result.Error
}

// IsUserInGroup checks if a user is in a specific group
func (r *GroupAssociationRepository) IsUserInGroup(userID, groupID int64) (bool, error) {
	var count int64
	result := Get().Model(&GroupAssociation{}).
		Where("user_id = ? AND group_id = ?", userID, groupID).
		Count(&count)
	return count > 0, result.Error
}

// Create creates a new group association
func (r *GroupAssociationRepository) Create(association *GroupAssociation) error {
	return Get().Create(association).Error
}

// Delete deletes a specific user-group association
func (r *GroupAssociationRepository) Delete(userID, groupID int64) error {
	return Get().Delete(&GroupAssociation{}, "user_id = ? AND group_id = ?", userID, groupID).Error
}

// DeleteAllForUser deletes all group associations for a user
func (r *GroupAssociationRepository) DeleteAllForUser(userID int64) error {
	return Get().Delete(&GroupAssociation{}, "user_id = ?", userID).Error
}

// PromoteToAdmin promotes a user to admin in a group
func (r *GroupAssociationRepository) PromoteToAdmin(userID, groupID int64) error {
	return Get().Model(&GroupAssociation{}).
		Where("user_id = ? AND group_id = ?", userID, groupID).
		Update("is_group_admin", true).Error
}

// DemoteFromAdmin demotes a user from admin in a group
func (r *GroupAssociationRepository) DemoteFromAdmin(userID, groupID int64) error {
	return Get().Model(&GroupAssociation{}).
		Where("user_id = ? AND group_id = ?", userID, groupID).
		Update("is_group_admin", false).Error
}
