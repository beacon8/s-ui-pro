//go:build cgo

package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/admin8800/s-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSQLiteReadOnlyDSNHandlesWindowsDrivePath(t *testing.T) {
	dsn := sqliteReadOnlyDSN(`C:\s-ui data\database.db`)
	if !strings.HasPrefix(dsn, "file:///C:/s-ui%20data/database.db?") {
		t.Fatalf("Windows SQLite URI = %q", dsn)
	}
}

func TestReplaceSQLiteDatabaseRollsBackFailedFinalValidation(t *testing.T) {
	initBackupTestDB(t)
	if err := db.Create(&model.Setting{Key: "restore-marker", Value: "old"}).Error; err != nil {
		t.Fatal(err)
	}

	sourceBytes, err := GetDb("")
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "invalid-source.db")
	if err := os.WriteFile(sourcePath, sourceBytes, 0600); err != nil {
		t.Fatal(err)
	}
	sourceDB, err := gorm.Open(sqlite.Open(sourcePath))
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceDB.Model(&model.Setting{}).Where("key = ?", "restore-marker").Update("value", "new").Error; err != nil {
		t.Fatal(err)
	}
	if err := sourceDB.Migrator().DropTable(&model.Tokens{}); err != nil {
		t.Fatal(err)
	}
	if sourceSQL, err := sourceDB.DB(); err != nil {
		t.Fatal(err)
	} else if err := sourceSQL.Close(); err != nil {
		t.Fatal(err)
	}

	if err := replaceSQLiteDatabase(sourcePath); err == nil {
		t.Fatal("replaceSQLiteDatabase accepted a source missing a required table")
	}
	var marker model.Setting
	if err := db.Where("key = ?", "restore-marker").First(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if marker.Value != "old" {
		t.Fatalf("live database was not rolled back: marker = %q", marker.Value)
	}
	if err := validateSQLiteDB(db, completeRestoreTables); err != nil {
		t.Fatalf("rolled-back live database is invalid: %v", err)
	}
}
