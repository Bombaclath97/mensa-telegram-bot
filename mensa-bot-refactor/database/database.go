package database

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/Bombaclath97/mensa-telegram-bot/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	db   *gorm.DB
	once sync.Once

	botAdminsMu  sync.RWMutex
	botAdminsSet = map[int64]struct{}{}
)

// Init initializes the database connection and runs migrations.
// Foreign key enforcement is requested in the DSN (`_foreign_keys=on`) so that it
// applies to EVERY pooled connection: `PRAGMA foreign_keys = ON` on the *sql.DB pool
// only ever reaches one connection and is therefore useless.
func Init() error {
	var initErr error

	once.Do(func() {
		dbPath := os.Getenv("DB_PATH")
		if dbPath == "" {
			dbPath = "./mensa-telegram.db"
		}

		dsn := dbPath
		if strings.Contains(dsn, "?") {
			dsn += "&_foreign_keys=on"
		} else {
			dsn += "?_foreign_keys=on"
		}

		var err error
		db, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
			Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		})
		if err != nil {
			initErr = err
			return
		}

		// Auto-migrate the schema
		err = db.AutoMigrate(&User{}, &Group{}, &GroupAssociation{}, &PendingJoinRequest{}, &UserPreference{})
		if err != nil {
			initErr = err
			return
		}

		logger.Info("Database initialized successfully", map[string]interface{}{"path": dbPath})
	})

	return initErr
}

// Get returns the database instance
func Get() *gorm.DB {
	if db == nil {
		logger.Fatal("Database not initialized. Call Init() first.")
	}
	return db
}

// Close closes the database connection. Safe to call even if Init() was never called
// or already failed.
func Close() error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// ParseAdminIDs parses a comma-separated list of Telegram IDs (the ADMIN_TELEGRAM_IDS
// env var format). Blank entries are ignored; a malformed entry is an error.
func ParseAdminIDs(raw string) ([]int64, error) {
	var ids []int64
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram id %q: %w", part, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// SeedBotAdmins records the configured bot administrators and flags every matching
// user that already exists. Users in the list that register LATER are flagged by the
// User.BeforeCreate hook (see IsSeededBotAdmin). Call once on boot, after Init().
func SeedBotAdmins(ids []int64) error {
	botAdminsMu.Lock()
	botAdminsSet = make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		botAdminsSet[id] = struct{}{}
	}
	botAdminsMu.Unlock()

	if len(ids) == 0 {
		logger.Warn("No bot admins configured (ADMIN_TELEGRAM_IDS is empty)")
		return nil
	}

	result := Get().Model(&User{}).Where("telegram_id IN ?", ids).Update("is_bot_admin", true)
	if result.Error != nil {
		return fmt.Errorf("failed to seed bot admins: %w", result.Error)
	}

	logger.Info("Bot admins seeded", map[string]interface{}{
		"configured": len(ids),
		"updated":    result.RowsAffected,
	})
	return nil
}

// IsSeededBotAdmin reports whether the given Telegram ID was passed to SeedBotAdmins.
func IsSeededBotAdmin(telegramID int64) bool {
	botAdminsMu.RLock()
	defer botAdminsMu.RUnlock()
	_, ok := botAdminsSet[telegramID]
	return ok
}
