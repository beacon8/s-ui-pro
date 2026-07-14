package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/admin8800/s-ui/config"
	"github.com/admin8800/s-ui/util"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateDbRunsUpstreamMigrationsForProVersion(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	path := config.GetDBPath()
	db, err := gorm.Open(sqlite.Open(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)",
		"CREATE TABLE users (id INTEGER PRIMARY KEY, password TEXT)",
		"CREATE TABLE tls (id INTEGER PRIMARY KEY, server BLOB, client BLOB)",
		"CREATE TABLE inbounds (id INTEGER PRIMARY KEY, tls_id INTEGER, out_json BLOB)",
		"INSERT INTO settings (key, value) VALUES ('version', '1.6.19')",
		"INSERT INTO users (id, password) VALUES (1, '$2plaintext')",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	if err := MigrateDb(); err != nil {
		t.Fatal(err)
	}

	db, err = gorm.Open(sqlite.Open(filepath.Clean(path)))
	if err != nil {
		t.Fatal(err)
	}
	var password, version string
	if err := db.Raw("SELECT password FROM users WHERE id = 1").Scan(&password).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT value FROM settings WHERE key = 'version'").Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	if !util.IsHashedPassword(password) || !util.CheckPassword("$2plaintext", password) {
		t.Fatalf("password was not migrated to a valid hash: %q", password)
	}
	if version != config.GetVersion() {
		t.Fatalf("database version = %q, want %q", version, config.GetVersion())
	}
}

func TestMigrateDbAtReturnsErrorInsteadOfExitingOnInvalidSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.db")
	db, err := gorm.Open(sqlite.Open(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE unrelated (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err != nil {
		t.Fatal(err)
	} else if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	if err := MigrateDbAt(path); err == nil {
		t.Fatal("MigrateDbAt accepted a database without the settings table")
	}

	db, err = gorm.Open(sqlite.Open(path))
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err != nil {
		t.Fatal(err)
	} else {
		defer sqlDB.Close()
	}
	var count int64
	if err := db.Table("unrelated").Count(&count).Error; err != nil {
		t.Fatalf("database became unavailable after failed migration: %v", err)
	}
}

func TestMigrateDbAtMalformedLegacyConfigReturnsErrorAndKeepsDatabase(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "legacy.db")
	db, err := gorm.Open(sqlite.Open(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)",
		"CREATE TABLE changes (id INTEGER PRIMARY KEY, actor TEXT, obj BLOB)",
		"CREATE TABLE marker (id INTEGER PRIMARY KEY, value TEXT)",
		"INSERT INTO marker (id, value) VALUES (1, 'untouched')",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if sqlDB, err := db.DB(); err != nil {
		t.Fatal(err)
	} else if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Missing `inbounds` previously caused an unchecked type assertion panic.
	if err := os.WriteFile(filepath.Join(binDir, "config.json"), []byte(`{"outbounds":[],"experimental":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	originalArg0 := os.Args[0]
	os.Args[0] = filepath.Join(root, "s-ui")
	t.Cleanup(func() { os.Args[0] = originalArg0 })
	t.Setenv("SUI_BIN_FOLDER", "bin")

	if err := MigrateDbAt(path); err == nil {
		t.Fatal("MigrateDbAt accepted malformed legacy config")
	}

	db, err = gorm.Open(sqlite.Open(path))
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err != nil {
		t.Fatal(err)
	} else {
		defer sqlDB.Close()
	}
	var value string
	if err := db.Raw("SELECT value FROM marker WHERE id = 1").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if value != "untouched" {
		t.Fatalf("failed migration changed original database: %q", value)
	}
}
