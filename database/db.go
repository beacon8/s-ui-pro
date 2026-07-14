package database

import (
	"encoding/json"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/admin8800/s-ui/config"
	"github.com/admin8800/s-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

func initUser(target *gorm.DB) error {
	var count int64
	err := target.Model(&model.User{}).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		user := &model.User{
			Username: "admin",
			Password: "admin",
		}
		return target.Create(user).Error
	}
	return nil
}

func openDB(dbPath string) (*gorm.DB, error) {
	dir := path.Dir(dbPath)
	err := os.MkdirAll(dir, 01740)
	if err != nil {
		return nil, err
	}

	var gormLogger logger.Interface

	if config.IsDebug() {
		gormLogger = logger.Default
	} else {
		gormLogger = logger.Discard
	}

	c := &gorm.Config{
		Logger: gormLogger,
	}
	sep := "?"
	if strings.Contains(dbPath, "?") {
		sep = "&"
	}
	// _cache_size=-200 caps each connection's page cache at ~200 KiB
	// (default is ~2 MiB), reducing memory amplification if a connection
	// escapes the pool.
	dsn := dbPath + sep + "_busy_timeout=10000&_journal_mode=WAL&_cache_size=-200"
	candidate, err := gorm.Open(sqlite.Open(dsn), c)
	if err != nil {
		return nil, err
	}

	sqlDB, err := candidate.DB()
	if err != nil {
		return nil, err
	}
	// SQLite has a single writer. Keeping one application connection serializes
	// panel, cron and restore writes in the pool instead of surfacing SQLITE_BUSY
	// between this process's own goroutines.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	if config.IsDebug() {
		candidate = candidate.Debug()
	}
	return candidate, nil
}

func OpenDB(dbPath string) error {
	candidate, err := openDB(dbPath)
	if err != nil {
		return err
	}
	oldDB := db
	db = candidate
	closeDB(oldDB)
	return nil
}

func InitDB(dbPath string) error {
	candidate, err := prepareDB(dbPath)
	if err != nil {
		return err
	}
	oldDB := db
	db = candidate
	closeDB(oldDB)
	return nil
}

func prepareDB(dbPath string) (*gorm.DB, error) {
	candidate, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}
	if err := initializeDB(candidate); err != nil {
		closeDB(candidate)
		return nil, err
	}
	return candidate, nil
}

func initializeDB(target *gorm.DB) error {

	// Default Outbounds
	if !target.Migrator().HasTable(&model.Outbound{}) {
		if err := target.Migrator().CreateTable(&model.Outbound{}); err != nil {
			return err
		}
		defaultOutbound := []model.Outbound{
			{Type: "direct", Tag: "direct", Options: json.RawMessage(`{}`)},
		}
		if err := target.Create(&defaultOutbound).Error; err != nil {
			return err
		}
	}

	if err := dedupStats(target); err != nil {
		return err
	}

	if err := target.AutoMigrate(
		&model.Setting{},
		&model.Tls{},
		&model.Inbound{},
		&model.Outbound{},
		&model.Service{},
		&model.Endpoint{},
		&model.User{},
		&model.Tokens{},
		&model.Stats{},
		&model.Client{},
		&model.Changes{},
	); err != nil {
		return err
	}
	if err := initUser(target); err != nil {
		return err
	}

	return nil
}

func closeDB(target *gorm.DB) {
	if target == nil {
		return
	}
	if sqlDB, err := target.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// dedupStats merges traffic for duplicate groups of (resource, tag, date_time, direction)
func dedupStats(target *gorm.DB) error {
	if !target.Migrator().HasTable(&model.Stats{}) {
		return nil
	}

	var dupGroups int64
	err := target.Raw("SELECT COUNT(*) FROM (SELECT 1 FROM stats GROUP BY resource, tag, date_time, direction HAVING COUNT(*) > 1)").Scan(&dupGroups).Error
	if err != nil {
		return err
	}
	if dupGroups == 0 {
		return nil
	}
	log.Printf("stats: collapsing %d duplicate group(s) before adding unique index", dupGroups)

	return target.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`CREATE TEMP TABLE stats_dedup AS
			SELECT MIN(id) AS id, resource, tag, date_time, direction, SUM(traffic) AS traffic
			FROM stats GROUP BY resource, tag, date_time, direction`).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM stats").Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO stats (id, resource, tag, date_time, direction, traffic)
			SELECT id, resource, tag, date_time, direction, traffic FROM stats_dedup`).Error; err != nil {
			return err
		}
		return tx.Exec("DROP TABLE stats_dedup").Error
	})
}

func GetDB() *gorm.DB {
	return db
}

func IsNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}
