package migration

import (
	"fmt"
	"os"
	"strings"

	"github.com/admin8800/s-ui/config"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func MigrateDb() error {
	return MigrateDbAt(config.GetDBPath())
}

// MigrateDbAt migrates a database in place. Returning errors instead of exiting
// the process lets restore callers validate and migrate a temporary database
// before it can affect the live database.
func MigrateDbAt(path string) (resultErr error) {
	// Legacy migrations predate structured validation and may encounter shapes
	// produced by hand-edited config.json files. Never let such input terminate
	// a restore/CLI process with a panic; the transaction rollback defers below
	// run first, then this boundary converts the panic to an ordinary error.
	defer func() {
		if recovered := recover(); recovered != nil {
			resultErr = fmt.Errorf("migration panic: %v", recovered)
		}
	}()

	// void running on first install
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		fmt.Println("Database not found")
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat database: %w", err)
	}

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
	}()
	tx := db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("begin migration: %w", tx.Error)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback().Error
		}
	}()

	currentVersion := config.GetVersion()
	dbVersion := ""
	if err := tx.Raw("SELECT value FROM settings WHERE key = ?", "version").Scan(&dbVersion).Error; err != nil {
		return fmt.Errorf("read database version: %w", err)
	}
	fmt.Println("Current version:", currentVersion, "\nDatabase version:", dbVersion)

	if currentVersion == dbVersion {
		fmt.Println("Database is up to date, no need to migrate")
		if err := tx.Commit().Error; err != nil {
			return fmt.Errorf("commit migration: %w", err)
		}
		committed = true
		return nil
	}

	fmt.Println("Start migrating database...")

	// Before 1.2
	if dbVersion == "" {
		err = to1_1(tx)
		if err != nil {
			return fmt.Errorf("migration to 1.1 failed: %w", err)
		}
		err = to1_2(tx)
		if err != nil {
			return fmt.Errorf("migration to 1.2 failed: %w", err)
		}
		dbVersion = "1.2"
	}

	// Before 1.3
	if strings.HasPrefix(dbVersion, "1.2") {
		err = to1_3(tx)
		if err != nil {
			return fmt.Errorf("migration to 1.3 failed: %w", err)
		}
	}

	// These migrations are idempotent and must also run for s-ui-pro databases
	// whose version already passed upstream's 1.5.x version numbers.
	err = to1_5_1(tx)
	if err != nil {
		return fmt.Errorf("migration to 1.5.1 failed: %w", err)
	}

	err = to1_5_2(tx)
	if err != nil {
		return fmt.Errorf("migration to 1.5.2 failed: %w", err)
	}

	// Set version
	err = tx.Exec("UPDATE settings SET value = ? WHERE key = ?", currentVersion, "version").Error
	if err != nil {
		return fmt.Errorf("update database version: %w", err)
	}
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	committed = true
	fmt.Println("Migration done!")
	return nil
}
