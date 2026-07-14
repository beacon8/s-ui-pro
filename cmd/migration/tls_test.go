package migration

import (
	"encoding/json"
	"testing"

	"github.com/admin8800/s-ui/config"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateDbUpdatesTlsPinDataForProVersion(t *testing.T) {
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
		`INSERT INTO tls (id, server, client) VALUES (1, '{}', '{"certificate_public_key_sha256":["stale"]}')`,
		`INSERT INTO inbounds (id, tls_id, out_json) VALUES (1, 1, '{"tls":{"certificate_public_key_sha256":["stale"]}}')`,
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

	MigrateDb()

	db, err = gorm.Open(sqlite.Open(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT client FROM tls WHERE id = 1",
		"SELECT out_json FROM inbounds WHERE id = 1",
	} {
		var raw string
		if err := db.Raw(query).Scan(&raw).Error; err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatalf("%s returned invalid JSON %q: %v", query, raw, err)
		}
		if tlsValue, ok := value["tls"].(map[string]any); ok {
			value = tlsValue
		}
		if _, exists := value["certificate_public_key_sha256"]; exists {
			t.Fatalf("%s kept stale TLS pin: %s", query, raw)
		}
	}
}
