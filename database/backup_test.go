package database

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/admin8800/s-ui/config"
	"github.com/admin8800/s-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func initBackupTestDB(t *testing.T) string {
	t.Helper()
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	dbPath := config.GetDBPath()
	if err := InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db == nil {
			return
		}
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return dbPath
}

func openBackupBytes(t *testing.T, data []byte) *gorm.DB {
	t.Helper()
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := os.WriteFile(backupPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	backupDB, err := gorm.Open(sqlite.Open(backupPath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := backupDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return backupDB
}

func TestGetDbIncludesPreviouslyMissingPersistentTables(t *testing.T) {
	initBackupTestDB(t)

	service := model.Service{Type: "socks", Tag: "backup-service", Options: json.RawMessage(`{}`)}
	if err := db.Create(&service).Error; err != nil {
		t.Fatal(err)
	}
	var user model.User
	if err := db.First(&user).Error; err != nil {
		t.Fatal(err)
	}
	token := model.Tokens{Desc: "backup-token", Token: "secret-token", UserId: user.Id}
	if err := db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}

	data, err := GetDb("")
	if err != nil {
		t.Fatal(err)
	}
	backupDB := openBackupBytes(t, data)

	for name, target := range map[string]any{
		"services": &model.Service{},
		"tokens":   &model.Tokens{},
	} {
		var count int64
		if err := backupDB.Model(target).Count(&count).Error; err != nil {
			t.Fatalf("backup is missing %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("backup %s count = %d, want 1", name, count)
		}
	}
}

func TestImportDBRejectsCorruptSQLiteWithoutClosingCurrentDB(t *testing.T) {
	initBackupTestDB(t)
	if err := db.Create(&model.Setting{Key: "restore-marker", Value: "kept"}).Error; err != nil {
		t.Fatal(err)
	}

	corruptPath := filepath.Join(t.TempDir(), "corrupt.db")
	corrupt := append([]byte("SQLite format 3\x00"), []byte(strings.Repeat("not-a-database", 32))...)
	if err := os.WriteFile(corruptPath, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(corruptPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	if err := ImportDB(file); err == nil {
		t.Fatal("ImportDB accepted a corrupt SQLite file")
	}

	var marker model.Setting
	if err := db.Where("key = ?", "restore-marker").First(&marker).Error; err != nil {
		t.Fatalf("current database became unavailable after rejected restore: %v", err)
	}
	if marker.Value != "kept" {
		t.Fatalf("current database changed after rejected restore: %#v", marker)
	}
}

func TestImportDBRejectsConfiguredOversizeWithoutChangingCurrentDB(t *testing.T) {
	initBackupTestDB(t)
	t.Setenv("SUI_MAX_RESTORE_BYTES", "32")
	if err := db.Create(&model.Setting{Key: "restore-marker", Value: "kept"}).Error; err != nil {
		t.Fatal(err)
	}

	oversizePath := filepath.Join(t.TempDir(), "oversize.db")
	if err := os.WriteFile(oversizePath, []byte(strings.Repeat("x", 33)), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(oversizePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := importDB(file, nil); err == nil || !strings.Contains(err.Error(), "exceeds 32 bytes") {
		t.Fatalf("oversize restore error = %v", err)
	}

	var marker model.Setting
	if err := db.Where("key = ?", "restore-marker").First(&marker).Error; err != nil {
		t.Fatalf("current database became unavailable: %v", err)
	}
	if marker.Value != "kept" {
		t.Fatalf("current database changed: %#v", marker)
	}
}

func TestGetDbExcludesRowsButKeepsSchemas(t *testing.T) {
	initBackupTestDB(t)
	if err := db.Create(&model.Stats{DateTime: 1, Resource: "user", Tag: "alice", Traffic: 10}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Changes{DateTime: 1, Actor: "test", Key: "config", Action: "save", Obj: json.RawMessage(`{}`)}).Error; err != nil {
		t.Fatal(err)
	}

	data, err := GetDb("stats, changes")
	if err != nil {
		t.Fatal(err)
	}
	backupDB := openBackupBytes(t, data)
	for name, target := range map[string]any{
		"stats":   &model.Stats{},
		"changes": &model.Changes{},
	} {
		if !backupDB.Migrator().HasTable(target) {
			t.Fatalf("excluded table %s schema is missing", name)
		}
		var count int64
		if err := backupDB.Model(target).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("excluded table %s has %d rows", name, count)
		}
	}
}

func TestConcurrentGetDbReturnsIndependentValidSnapshots(t *testing.T) {
	initBackupTestDB(t)

	const workers = 8
	start := make(chan struct{})
	results := make(chan struct {
		data []byte
		err  error
	}, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			data, err := GetDb("stats,changes")
			results <- struct {
				data []byte
				err  error
			}{data: data, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	index := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent backup %d failed: %v", index, result.err)
		}
		backupDB := openBackupBytes(t, result.data)
		if err := validateSQLiteDB(backupDB, completeRestoreTables); err != nil {
			t.Fatalf("concurrent backup %d is invalid: %v", index, err)
		}
		index++
	}
	if index != workers {
		t.Fatalf("received %d backups, want %d", index, workers)
	}
}

func TestImportDBRejectsUnrelatedSQLiteWithoutChangingCurrentDB(t *testing.T) {
	initBackupTestDB(t)
	if err := db.Create(&model.Setting{Key: "restore-marker", Value: "kept"}).Error; err != nil {
		t.Fatal(err)
	}

	unrelatedPath := filepath.Join(t.TempDir(), "unrelated.db")
	unrelated, err := gorm.Open(sqlite.Open(unrelatedPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := unrelated.Exec("CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := unrelated.DB(); err != nil {
		t.Fatal(err)
	} else if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(unrelatedPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	if err := ImportDB(file); err == nil {
		t.Fatal("ImportDB accepted an unrelated SQLite database")
	}
	var marker model.Setting
	if err := db.Where("key = ?", "restore-marker").First(&marker).Error; err != nil {
		t.Fatalf("current database became unavailable: %v", err)
	}
	if marker.Value != "kept" {
		t.Fatalf("current database changed: %#v", marker)
	}
}

func TestImportDBReplacesLiveDatabaseAndKeepsConnectionUsable(t *testing.T) {
	initBackupTestDB(t)
	for key, value := range map[string]string{
		"config":         `{}`,
		"version":        config.GetVersion(),
		"restore-marker": "old",
	} {
		if err := db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
			t.Fatal(err)
		}
	}

	sourceBytes, err := GetDb("")
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "source.db")
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
	if sourceSQL, err := sourceDB.DB(); err != nil {
		t.Fatal(err)
	} else if err := sourceSQL.Close(); err != nil {
		t.Fatal(err)
	}
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceFile.Close()

	restarts := 0
	if err := importDB(sourceFile, func() error {
		restarts++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if restarts != 1 {
		t.Fatalf("restart calls = %d, want 1", restarts)
	}

	var marker model.Setting
	if err := db.Where("key = ?", "restore-marker").First(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if marker.Value != "new" {
		t.Fatalf("restored marker = %q, want new", marker.Value)
	}
	if err := db.Create(&model.Setting{Key: "after-restore", Value: "writable"}).Error; err != nil {
		t.Fatalf("restored database is not writable: %v", err)
	}
	if err := validateSQLiteDB(db, completeRestoreTables); err != nil {
		t.Fatalf("restored live database is invalid: %v", err)
	}
}

func TestImportDBReportsRestartFailureAfterCommittedRestore(t *testing.T) {
	initBackupTestDB(t)
	for key, value := range map[string]string{
		"config":         `{}`,
		"version":        config.GetVersion(),
		"restore-marker": "old",
	} {
		if err := db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
			t.Fatal(err)
		}
	}

	sourceBytes, err := GetDb("")
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "restart-source.db")
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
	if sourceSQL, err := sourceDB.DB(); err != nil {
		t.Fatal(err)
	} else if err := sourceSQL.Close(); err != nil {
		t.Fatal(err)
	}
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceFile.Close()

	err = importDB(sourceFile, func() error { return os.ErrPermission })
	if err == nil || !strings.Contains(err.Error(), "database restored successfully") {
		t.Fatalf("restart failure did not report the restore commit point: %v", err)
	}
	var marker model.Setting
	if err := db.Where("key = ?", "restore-marker").First(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if marker.Value != "new" {
		t.Fatalf("committed restore was unexpectedly rolled back: marker = %q", marker.Value)
	}
}

func TestConsumeRestoreRestartIsOneShot(t *testing.T) {
	restoreRestartPending.Store(true)
	t.Cleanup(func() { restoreRestartPending.Store(false) })
	if !ConsumeRestoreRestart() {
		t.Fatal("restore restart marker was not consumed")
	}
	if ConsumeRestoreRestart() {
		t.Fatal("restore restart marker was consumed more than once")
	}
}
