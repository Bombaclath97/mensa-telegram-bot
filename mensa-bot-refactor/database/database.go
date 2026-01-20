package database

import (
	"os"
	"sync"

	"github.com/Bombaclath97/mensa-telegram-bot/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	db   *gorm.DB
	once sync.Once
)

// Init initializes the database connection and runs migrations
func Init() error {
	var initErr error

	once.Do(func() {
		dbPath := os.Getenv("DB_PATH")
		if dbPath == "" {
			dbPath = "./mensa-telegram.db"
		}

		var err error
		db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
			Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		})
		if err != nil {
			initErr = err
			return
		}

		// Enable foreign keys for SQLite
		sqlDB, err := db.DB()
		if err != nil {
			initErr = err
			return
		}
		sqlDB.Exec("PRAGMA foreign_keys = ON")

		// Auto-migrate the schema
		err = db.AutoMigrate(&User{}, &Group{}, &GroupAssociation{})
		if err != nil {
			initErr = err
			return
		}

		logger.Info("Database initialized successfully")
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

// Close closes the database connection
func Close() error {
	if db != nil {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}
