package database

import (
	"time"

	"gorm.io/gorm"
)

// DefaultLanguage is the default language for users
const DefaultLanguage = "it"

// User represents a registered Mensa member in the bot.
// TelegramID is a NATURAL key (the Telegram user ID): never auto-increment it.
type User struct {
	TelegramID      int64  `gorm:"primaryKey;autoIncrement:false" json:"telegram_id"`
	MensaEmail      string `gorm:"not null" json:"mensa_email"`
	FirstName       string `gorm:"not null" json:"first_name"`
	LastName        string `gorm:"not null" json:"last_name"`
	MemberNumber    int64  `gorm:"not null" json:"member_number"`
	IsBotAdmin      bool   `gorm:"default:false" json:"is_bot_admin"`
	IsInternational bool   `gorm:"default:false" json:"is_international"`
	Language        string `gorm:"default:it" json:"language"`

	// GroupsSyncedAt is when the bot last probed every known group for this member and wrote
	// their group_associations. NULL means it has never been done.
	//
	// This exists to disambiguate an empty association list, which otherwise has two
	// indistinguishable meanings:
	//
	//	NULL           -> never probed. We do NOT know which groups they are in.
	//	set, and empty -> probed, and they genuinely belong to no groups.
	//
	// The cleanup sweep must never delete a member of the first kind: kicking them from
	// "no groups" and then dropping the row leaves them sitting in the group forever with
	// no record they exist (cleanup/cleanup.go guards the error branch with exactly this
	// reasoning, but an empty list is not an error). Deleting a member of the second kind
	// is correct and must stay possible — a blanket "never delete on empty" rule would
	// leak rows forever for anyone legitimately in no groups.
	GroupsSyncedAt *time.Time `json:"groups_synced_at"`
}

// BeforeCreate applies the ADMIN_TELEGRAM_IDS seed to users that register after boot.
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if IsSeededBotAdmin(u.TelegramID) {
		u.IsBotAdmin = true
	}
	return nil
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

// Group represents a Telegram group that the bot manages.
// GroupID is a NATURAL key (the Telegram chat ID): never auto-increment it.
//
// The has-many below is what materialises groups_associations.group_id -> groups.group_id.
// It MUST be declared on this side: a belongs-to on GroupAssociation would be ambiguous
// (both structs have a field called GroupID) and GORM would guess has-one, emitting the
// nonsensical reversed FK groups.group_id -> groups_associations.group_id.
type Group struct {
	GroupID int64 `gorm:"primaryKey;autoIncrement:false" json:"group_id"`

	// Status is the bot's own standing in the group, kept current by every my_chat_member
	// update: GroupStatusAdmin, GroupStatusMember or GroupStatusLeft. Empty means unknown
	// (rows from before this column existed) and is treated as active.
	//
	// A group the bot has left is KEPT, not deleted: deleting it would cascade away every
	// member's association with it, which is the wrong answer when an admin removed the bot
	// by mistake and adds it back. Everything that probes groups skips the left ones instead.
	Status    string    `gorm:"not null;default:''" json:"status"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`

	GroupAssociations []GroupAssociation `gorm:"foreignKey:GroupID;references:GroupID;constraint:OnDelete:CASCADE,OnUpdate:CASCADE" json:"-"`
}

func (Group) TableName() string {
	return "groups"
}

// Bot status values for Group.Status.
const (
	GroupStatusAdmin  = "admin"
	GroupStatusMember = "member"
	GroupStatusLeft   = "left"
)

// Active reports whether the bot is (as far as we know) still in the group.
func (g Group) Active() bool { return g.Status != GroupStatusLeft }

// GroupAssociation represents the many-to-many relationship between users and groups.
// Both foreign keys live on THIS table and cascade on delete:
//
//	groups_associations.user_id  -> users.telegram_id  ON DELETE CASCADE (belongs-to below)
//	groups_associations.group_id -> groups.group_id    ON DELETE CASCADE (has-many on Group)
type GroupAssociation struct {
	UserID       int64 `gorm:"primaryKey;autoIncrement:false;not null" json:"user_id"`
	GroupID      int64 `gorm:"primaryKey;autoIncrement:false;not null" json:"group_id"`
	IsGroupAdmin bool  `gorm:"default:false" json:"is_group_admin"`

	User *User `gorm:"foreignKey:UserID;references:TelegramID;constraint:OnDelete:CASCADE,OnUpdate:CASCADE" json:"-"`
}

func (GroupAssociation) TableName() string {
	return "groups_associations"
}

// PendingJoinRequest buffers a chat join request made by a user who is not (yet)
// a registered member. The /approva command drains it once the user registers.
// Deliberately has NO foreign keys: the user does not exist in `users` yet, and
// the group may not have been registered by the bot.
type PendingJoinRequest struct {
	UserID    int64     `gorm:"primaryKey;autoIncrement:false;not null" json:"user_id"`
	GroupID   int64     `gorm:"primaryKey;autoIncrement:false;not null" json:"group_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (PendingJoinRequest) TableName() string {
	return "pending_join_requests"
}

// UserPreference stores settings for users that are NOT registered yet (mainly the
// language picked via /lang before registration). It is carried into the User row on
// registration and deleted afterwards.
type UserPreference struct {
	TelegramID int64  `gorm:"primaryKey;autoIncrement:false" json:"telegram_id"`
	Language   string `gorm:"not null" json:"language"`
}

func (UserPreference) TableName() string {
	return "user_preferences"
}

// PendingRemoval records a group a member still has to be kicked from after their profile
// was deleted (/elimina). The profile goes immediately, because the member asked for that;
// this row keeps only what is needed to finish the job, and membership.DrainPendingRemovals
// retries it in the background.
//
// No foreign keys and no personal data, on purpose: the user row is already gone, and the
// group row may be dropped too without this record vanishing with it.
type PendingRemoval struct {
	UserID    int64     `gorm:"primaryKey;autoIncrement:false;not null" json:"user_id"`
	GroupID   int64     `gorm:"primaryKey;autoIncrement:false;not null" json:"group_id"`
	Attempts  int       `gorm:"not null;default:0" json:"attempts"`
	LastError string    `json:"last_error"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (PendingRemoval) TableName() string {
	return "pending_removals"
}
