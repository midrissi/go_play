package persistence

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	db     *gorm.DB
	dbOnce sync.Once
)

// Init opens the SQLite database and runs migrations.
func Init(dbPath string) error {
	var initErr error
	dbOnce.Do(func() {
		// Ensure directory exists for the database file
		if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
			if err := ensureDir(dir); err != nil {
				initErr = fmt.Errorf("failed to ensure db directory: %w", err)
				return
			}
		}

		d, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
		if err != nil {
			initErr = fmt.Errorf("failed to open db: %w", err)
			return
		}
		db = d

		// Auto-migrate models
		if err := db.AutoMigrate(&CandleModel{}); err != nil {
			initErr = fmt.Errorf("auto migrate failed: %w", err)
			return
		}
	})
	return initErr
}

// SaveCandle persists a candle row; upserts on unique index (exchange+symbol+interval+open_time).
func SaveCandle(c CandleModel) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	// Use upsert on conflict
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "exchange"}, {Name: "symbol"}, {Name: "interval"}, {Name: "open_time"}},
		DoUpdates: clause.AssignmentColumns([]string{"close_time", "open", "high", "low", "close", "volume"}),
	}).Create(&c).Error
}

func ensureDir(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if os.IsNotExist(err) {
		return os.MkdirAll(path, 0o755)
	} else {
		return err
	}
}
