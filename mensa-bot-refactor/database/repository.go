package database

import (
	"errors"
	"time"

	"github.com/Bombaclath97/mensa-telegram-bot/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserRepository handles all user-related database operations.
//
// NOT-FOUND CONVENTION: lookups return (nil, nil) when the row does not exist and
// (nil, err) when the query itself failed. Callers MUST check err before concluding
// "not registered".
type UserRepository struct{}

func NewUserRepository() *UserRepository {
	return &UserRepository{}
}

// GetByID retrieves a user by their Telegram ID. Returns (nil, nil) if not registered.
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

// GetByEmail retrieves a user by their Mensa email. Returns (nil, nil) if not found.
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

// GetAll retrieves all registered users (used by the membership-expiry cleanup sweep).
func (r *UserRepository) GetAll() ([]User, error) {
	var users []User
	result := Get().Find(&users)
	return users, result.Error
}

// Create creates a new user.
//
// Two side effects, both intentional:
//   - if user.Language is empty, any language preference the user expressed via /lang
//     BEFORE registering is carried into the row;
//   - the pending preference row is then dropped;
//   - IsBotAdmin is forced true when the Telegram ID is in ADMIN_TELEGRAM_IDS
//     (User.BeforeCreate hook).
func (r *UserRepository) Create(user *User) error {
	if user.Language == "" {
		user.Language = r.GetLanguage(user.TelegramID)
	}

	if err := Get().Create(user).Error; err != nil {
		return err
	}

	if err := Get().Delete(&UserPreference{}, "telegram_id = ?", user.TelegramID).Error; err != nil {
		// Not fatal: the User row is authoritative from now on.
		logger.Warn("Failed to drop pending user preference after registration", map[string]interface{}{
			"error":   err.Error(),
			"user_id": user.TelegramID,
		})
	}

	return nil
}

// Update updates an existing user (full save).
func (r *UserRepository) Update(user *User) error {
	return Get().Save(user).Error
}

// Delete deletes a user by their Telegram ID. groups_associations rows cascade.
func (r *UserRepository) Delete(telegramID int64) error {
	return Get().Delete(&User{}, "telegram_id = ?", telegramID).Error
}

// Exists checks if a user exists by their Telegram ID.
func (r *UserRepository) Exists(telegramID int64) (bool, error) {
	var count int64
	result := Get().Model(&User{}).Where("telegram_id = ?", telegramID).Count(&count)
	return count > 0, result.Error
}

// ExistsByEmail checks if a user exists by their email.
func (r *UserRepository) ExistsByEmail(email string) (bool, error) {
	var count int64
	result := Get().Model(&User{}).Where("mensa_email = ?", email).Count(&count)
	return count > 0, result.Error
}

// IsBotAdmin reports whether the user is a bot administrator. A user in
// ADMIN_TELEGRAM_IDS is an admin even if the row has not been flagged yet.
func (r *UserRepository) IsBotAdmin(telegramID int64) (bool, error) {
	if IsSeededBotAdmin(telegramID) {
		return true, nil
	}
	user, err := r.GetByID(telegramID)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, nil
	}
	return user.IsBotAdmin, nil
}

// UpdateLanguage persists the user's preferred language. If the user is not registered
// yet the preference is stored in user_preferences so the registration wizard (and every
// later message) renders in the chosen language; it is carried into the User row by Create.
func (r *UserRepository) UpdateLanguage(telegramID int64, language string) error {
	result := Get().Model(&User{}).
		Where("telegram_id = ?", telegramID).
		Update("language", language)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	// Not registered (yet): remember the choice out-of-band.
	pref := UserPreference{TelegramID: telegramID, Language: language}
	return Get().Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "telegram_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"language"}),
	}).Create(&pref).Error
}

// UpdateName persists the member's name as read from the Mensa API.
//
// The name is Mensa's to own — it is never typed by the user — so this is the only way it
// changes: at registration, and whenever the profile card is synced.
func (r *UserRepository) UpdateName(telegramID int64, firstName, lastName string) error {
	return Get().Model(&User{}).
		Where("telegram_id = ?", telegramID).
		Updates(map[string]interface{}{
			"first_name": firstName,
			"last_name":  lastName,
		}).Error
}

// MarkGroupsSynced stamps the member as having had every known group probed for their
// membership. Call it ONLY after a probe that actually completed: the whole value of the
// column is that a set timestamp means "we looked, and what is in groups_associations is the
// truth". Stamping it after a partial or failed sweep re-creates the ambiguity it exists to
// remove, and hands the cleanup sweep permission to delete someone it should have deferred.
func (r *UserRepository) MarkGroupsSynced(telegramID int64, at time.Time) error {
	return Get().Model(&User{}).
		Where("telegram_id = ?", telegramID).
		Update("groups_synced_at", at).Error
}

// GetLanguage retrieves the effective language for a Telegram ID: the registered user's
// language, else a pending /lang preference, else DefaultLanguage. Never fails loudly —
// a DB error degrades to the default language and is logged.
func (r *UserRepository) GetLanguage(telegramID int64) string {
	user, err := r.GetByID(telegramID)
	if err != nil {
		logger.Error("Failed to read user language", map[string]interface{}{
			"error":   err.Error(),
			"user_id": telegramID,
		})
		return DefaultLanguage
	}
	if user != nil {
		return user.GetLanguage()
	}

	var pref UserPreference
	result := Get().First(&pref, "telegram_id = ?", telegramID)
	if result.Error != nil {
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			logger.Error("Failed to read pending language preference", map[string]interface{}{
				"error":   result.Error.Error(),
				"user_id": telegramID,
			})
		}
		return DefaultLanguage
	}
	if pref.Language == "" {
		return DefaultLanguage
	}
	return pref.Language
}

// GroupRepository handles all group-related database operations
type GroupRepository struct{}

func NewGroupRepository() *GroupRepository {
	return &GroupRepository{}
}

// GetByID retrieves a group by its ID. Returns (nil, nil) if unknown.
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

// GetAll retrieves all groups the bot administers.
func (r *GroupRepository) GetAll() ([]Group, error) {
	var groups []Group
	result := Get().Find(&groups)
	return groups, result.Error
}

// Create creates a new group row. Fails on a duplicate group ID; prefer Register.
func (r *GroupRepository) Create(group *Group) error {
	return Get().Create(group).Error
}

// Register idempotently registers a group the bot has been added to.
func (r *GroupRepository) Register(groupID int64) error {
	return Get().Clauses(clause.OnConflict{DoNothing: true}).
		Create(&Group{GroupID: groupID}).Error
}

// Delete deletes a group by its ID. groups_associations rows cascade.
func (r *GroupRepository) Delete(groupID int64) error {
	return Get().Delete(&Group{}, "group_id = ?", groupID).Error
}

// Exists checks if a group is registered.
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

// GetUserGroups retrieves all group associations for a user.
func (r *GroupAssociationRepository) GetUserGroups(userID int64) ([]GroupAssociation, error) {
	var associations []GroupAssociation
	result := Get().Where("user_id = ?", userID).Find(&associations)
	return associations, result.Error
}

// GetGroupUsers retrieves all user associations for a group.
func (r *GroupAssociationRepository) GetGroupUsers(groupID int64) ([]GroupAssociation, error) {
	var associations []GroupAssociation
	result := Get().Where("group_id = ?", groupID).Find(&associations)
	return associations, result.Error
}

// Get retrieves a single association. Returns (nil, nil) if the user is not in the group.
func (r *GroupAssociationRepository) Get(userID, groupID int64) (*GroupAssociation, error) {
	var assoc GroupAssociation
	result := Get().First(&assoc, "user_id = ? AND group_id = ?", userID, groupID)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, result.Error
	}
	return &assoc, nil
}

// IsUserInGroup checks if a user is associated with a specific group.
func (r *GroupAssociationRepository) IsUserInGroup(userID, groupID int64) (bool, error) {
	var count int64
	result := Get().Model(&GroupAssociation{}).
		Where("user_id = ? AND group_id = ?", userID, groupID).
		Count(&count)
	return count > 0, result.Error
}

// Create creates a new group association. Fails on duplicates; prefer Register.
// NOTE: both the user and the group must exist (foreign keys are enforced).
func (r *GroupAssociationRepository) Create(association *GroupAssociation) error {
	return Get().Create(association).Error
}

// Register idempotently associates a registered user with a registered group.
// The group row is created first if missing, so that the FK holds even when the bot
// was added to the group before this code ran.
func (r *GroupAssociationRepository) Register(userID, groupID int64) error {
	groupRepo := NewGroupRepository()
	if err := groupRepo.Register(groupID); err != nil {
		return err
	}
	return Get().Clauses(clause.OnConflict{DoNothing: true}).
		Create(&GroupAssociation{UserID: userID, GroupID: groupID}).Error
}

// Delete deletes a specific user-group association.
func (r *GroupAssociationRepository) Delete(userID, groupID int64) error {
	return Get().Delete(&GroupAssociation{}, "user_id = ? AND group_id = ?", userID, groupID).Error
}

// DeleteAllForUser deletes all group associations for a user.
func (r *GroupAssociationRepository) DeleteAllForUser(userID int64) error {
	return Get().Delete(&GroupAssociation{}, "user_id = ?", userID).Error
}

// PromoteToAdmin flags a user as group admin. The association is created if missing.
func (r *GroupAssociationRepository) PromoteToAdmin(userID, groupID int64) error {
	if err := r.Register(userID, groupID); err != nil {
		return err
	}
	return Get().Model(&GroupAssociation{}).
		Where("user_id = ? AND group_id = ?", userID, groupID).
		Update("is_group_admin", true).Error
}

// DemoteFromAdmin clears the group-admin flag. No-op if the association is missing.
func (r *GroupAssociationRepository) DemoteFromAdmin(userID, groupID int64) error {
	return Get().Model(&GroupAssociation{}).
		Where("user_id = ? AND group_id = ?", userID, groupID).
		Update("is_group_admin", false).Error
}

// PendingJoinRequestRepository buffers join requests made by users who were not
// registered at the time. Replaces the legacy in-memory map, which lost every pending
// request on restart.
type PendingJoinRequestRepository struct{}

func NewPendingJoinRequestRepository() *PendingJoinRequestRepository {
	return &PendingJoinRequestRepository{}
}

// Add buffers a (user, group) join request. Idempotent.
func (r *PendingJoinRequestRepository) Add(userID, groupID int64) error {
	return Get().Clauses(clause.OnConflict{DoNothing: true}).
		Create(&PendingJoinRequest{UserID: userID, GroupID: groupID}).Error
}

// GetForUser returns every group the user has a buffered join request for
// (oldest first). Returns an empty slice when there is nothing pending.
func (r *PendingJoinRequestRepository) GetForUser(userID int64) ([]PendingJoinRequest, error) {
	var requests []PendingJoinRequest
	result := Get().Where("user_id = ?", userID).Order("created_at ASC").Find(&requests)
	return requests, result.Error
}

// DeleteForUser clears every buffered join request for a user.
func (r *PendingJoinRequestRepository) DeleteForUser(userID int64) error {
	return Get().Delete(&PendingJoinRequest{}, "user_id = ?", userID).Error
}

// Delete clears a single buffered join request.
func (r *PendingJoinRequestRepository) Delete(userID, groupID int64) error {
	return Get().Delete(&PendingJoinRequest{}, "user_id = ? AND group_id = ?", userID, groupID).Error
}
