package migration

import (
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

	MigrateDb()

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
