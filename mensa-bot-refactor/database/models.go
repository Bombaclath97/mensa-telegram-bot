package database

// DefaultLanguage is the default language for users
const DefaultLanguage = "it"

// User represents a registered Mensa member in the bot
type User struct {
	TelegramID      int64  `gorm:"primaryKey" json:"telegram_id"`
	MensaEmail      string `gorm:"not null" json:"mensa_email"`
	FirstName       string `gorm:"not null" json:"first_name"`
	LastName        string `gorm:"not null" json:"last_name"`
	MemberNumber    int64  `gorm:"not null" json:"member_number"`
	IsBotAdmin      bool   `gorm:"default:false" json:"is_bot_admin"`
	IsInternational bool   `gorm:"default:false" json:"is_international"`
	Language        string `gorm:"default:it" json:"language"`

	// Associations
	GroupAssociations []GroupAssociation `gorm:"foreignKey:UserID;references:TelegramID"`
}

// GetLanguage returns the user's language, defaulting to "it" if not set
func (u *User) GetLanguage() string {
	if u.Language == "" {
		return DefaultLanguage
	}
	return u.Language
}

func (User) TableName() string {
	return "users"
}

// Group represents a Telegram group that the bot manages
type Group struct {
	GroupID int64 `gorm:"primaryKey" json:"group_id"`

	// Associations
	GroupAssociations []GroupAssociation `gorm:"foreignKey:GroupID;references:GroupID"`
}

func (Group) TableName() string {
	return "groups"
}

// GroupAssociation represents the many-to-many relationship between users and groups
type GroupAssociation struct {
	UserID       int64 `gorm:"primaryKey;not null" json:"user_id"`
	GroupID      int64 `gorm:"primaryKey;not null" json:"group_id"`
	IsGroupAdmin bool  `gorm:"default:false" json:"is_group_admin"`

	// Foreign key references
	User  User  `gorm:"foreignKey:UserID;references:TelegramID;constraint:OnDelete:CASCADE,OnUpdate:CASCADE"`
	Group Group `gorm:"foreignKey:GroupID;references:GroupID;constraint:OnDelete:CASCADE,OnUpdate:CASCADE"`
}

func (GroupAssociation) TableName() string {
	return "groups_associations"
}
