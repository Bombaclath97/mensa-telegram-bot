package database

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mensa-Italia/mensa-telegram-bot/logger"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Connection pool bounds. The bot is a single replica handling one Telegram poller plus a
// webhook and a cleanup ticker, so it needs very few connections — but the Go default is
// UNLIMITED, which would let a burst open more than Postgres accepts.
const (
	maxOpenConns    = 10
	maxIdleConns    = 5
	connMaxLifetime = 30 * time.Minute
)

var (
	db   *gorm.DB
	once sync.Once

	botAdminsMu  sync.RWMutex
	botAdminsSet = map[int64]struct{}{}
)

// Init connects to PostgreSQL and runs migrations.
//
// # Why not SQLite
//
// SQLite is a local file, which pinned the whole service to one machine. Under Docker Swarm
// that is worse than it sounds: a named volume is LOCAL TO ITS NODE, so a task rescheduled
// elsewhere (node drain, failure, rebalance) comes up against an EMPTY database — and an
// empty database means every member looks unregistered. Postgres makes the bot stateless, so
// the task can move freely, and drops the cgo dependency (mattn/go-sqlite3) that forced a C
// toolchain into the build and glibc into the runtime image.
//
// Note this does NOT let the bot scale: replicas must still be exactly 1, because Telegram
// allows a single getUpdates poller per token, and both the draft store and the cleanup
// ticker are in-process.
//
// # Foreign keys
//
// Postgres enforces them natively and always, so the old `_foreign_keys=on` DSN hack is gone.
// It existed because SQLite defaults them OFF per-connection, and a `PRAGMA` on a pooled
// *sql.DB only ever reaches whichever connection happens to serve it.
func Init() error {
	var initErr error

	once.Do(func() {
		dsn, err := resolveDSN()
		if err != nil {
			initErr = err
			return
		}

		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		})
		if err != nil {
			initErr = err
			return
		}

		// Bound the pool. The default is unlimited, which lets a burst of Telegram updates
		// open more connections than Postgres will accept (default max_connections=100) and
		// turns a traffic spike into "FATAL: sorry, too many clients already".
		sqlDB, err := db.DB()
		if err != nil {
			initErr = err
			return
		}
		sqlDB.SetMaxOpenConns(maxOpenConns)
		sqlDB.SetMaxIdleConns(maxIdleConns)
		sqlDB.SetConnMaxLifetime(connMaxLifetime)

		// Auto-migrate the schema
		err = db.AutoMigrate(&User{}, &Group{}, &GroupAssociation{}, &PendingJoinRequest{}, &UserPreference{}, &PendingRemoval{})
		if err != nil {
			initErr = err
			return
		}

		logger.Info("Database initialized successfully", map[string]interface{}{
			"target": redactDSN(dsn),
		})
	})

	return initErr
}

// resolveDSN builds the Postgres connection string from the environment.
//
// Two forms are accepted, and the second exists for a specific reason:
//
//	DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable   (wins if set)
//	DB_HOST / DB_PORT / DB_USER / DB_PASSWORD / DB_NAME / DB_SSLMODE (assembled)
//
// The discrete form is what makes Docker secrets work WITHOUT duplicating the password. The
// official postgres image takes POSTGRES_PASSWORD_FILE; if the bot could only read a whole
// DATABASE_URL, the same password would have to exist twice — once as the postgres secret and
// again embedded in the bot's URL secret — and the two would drift the first time anyone
// rotated one of them. With DB_PASSWORD_FILE both services can point at the SAME secret.
//
// Both forms compose with config.ExpandFileSecrets, so DATABASE_URL_FILE and DB_PASSWORD_FILE
// work exactly as you would expect.
func resolveDSN() (string, error) {
	if dsn := strings.TrimSpace(os.Getenv("DATABASE_URL")); dsn != "" {
		return dsn, nil
	}

	host := strings.TrimSpace(os.Getenv("DB_HOST"))
	if host == "" {
		return "", errors.New("no database configured: set DATABASE_URL " +
			"(postgres://user:pass@host:5432/dbname?sslmode=disable), or DB_HOST/DB_USER/DB_PASSWORD/DB_NAME. " +
			"In Swarm prefer the DB_* form with DB_PASSWORD_FILE, so the bot and postgres can share one secret")
	}

	port := strings.TrimSpace(os.Getenv("DB_PORT"))
	if port == "" {
		port = "5432"
	}
	sslMode := strings.TrimSpace(os.Getenv("DB_SSLMODE"))
	if sslMode == "" {
		// The bot talks to Postgres over a private overlay network, not the internet.
		sslMode = "disable"
	}

	user := strings.TrimSpace(os.Getenv("DB_USER"))
	name := strings.TrimSpace(os.Getenv("DB_NAME"))
	password := os.Getenv("DB_PASSWORD") // NOT trimmed: whitespace can be meaningful here
	if user == "" || name == "" {
		return "", errors.New("DB_USER and DB_NAME are required when using the DB_* form")
	}

	// url.UserPassword escapes credentials, so a password containing '@', '/' or ':' cannot
	// silently corrupt the DSN — which is exactly what naive fmt.Sprintf assembly does.
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     net.JoinHostPort(host, port),
		Path:     "/" + name,
		RawQuery: url.Values{"sslmode": []string{sslMode}}.Encode(),
	}
	return u.String(), nil
}

// redactDSN strips the password out of a connection string so it can be logged.
// DATABASE_URL is a secret; logging it whole would put the Postgres password into whatever
// ships the logs.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "postgres (unparseable DSN)"
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
		}
	}
	return u.Redacted()
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

// SeedBotAdmins makes ADMIN_TELEGRAM_IDS authoritative: every user in the list is flagged
// is_bot_admin, and every user NOT in it has the flag taken away. Users in the list that
// register LATER are flagged by the User.BeforeCreate hook (see IsSeededBotAdmin). Call
// once on boot, after Init().
func SeedBotAdmins(ids []int64) error {
	botAdminsMu.Lock()
	botAdminsSet = make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		botAdminsSet[id] = struct{}{}
	}
	botAdminsMu.Unlock()

	// REVOKE FIRST. ADMIN_TELEGRAM_IDS is the only source of truth for who may run
	// /manual_add, so removing an ID from it MUST take the right away.
	//
	// Without this, is_bot_admin is a one-way latch: the promotion below was its only
	// writer, so a demoted admin kept the flag on their row forever and IsBotAdmin's DB
	// fallback (see repository.go) went on answering true — env change or not.
	//
	// An empty list therefore demotes EVERYONE: .env.example promises "Leave blank to
	// disable /manual_add entirely", and that has to mean it.
	demote := Get().Model(&User{}).Where("is_bot_admin = ?", true)
	if len(ids) > 0 {
		demote = demote.Where("telegram_id NOT IN ?", ids)
	}
	revoked := demote.Update("is_bot_admin", false)
	if revoked.Error != nil {
		return fmt.Errorf("failed to revoke bot admins: %w", revoked.Error)
	}
	if revoked.RowsAffected > 0 {
		logger.Info("Bot admin rights revoked (no longer in ADMIN_TELEGRAM_IDS)",
			map[string]interface{}{"count": revoked.RowsAffected})
	}

	if len(ids) == 0 {
		logger.Warn("No bot admins configured (ADMIN_TELEGRAM_IDS is empty): /manual_add is unavailable to everyone")
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

// SeededBotAdmins returns the IDs passed to SeedBotAdmins (ADMIN_TELEGRAM_IDS), sorted.
// They are who operational alerts are sent to.
func SeededBotAdmins() []int64 {
	botAdminsMu.RLock()
	defer botAdminsMu.RUnlock()
	ids := make([]int64, 0, len(botAdminsSet))
	for id := range botAdminsSet {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
