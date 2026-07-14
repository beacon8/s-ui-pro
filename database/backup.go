package database

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/admin8800/s-ui/cmd/migration"
	"github.com/admin8800/s-ui/config"
	"github.com/admin8800/s-ui/logger"
	"github.com/admin8800/s-ui/util/common"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	sqliteSignature             = "SQLite format 3\x00"
	defaultMaxRestoreUploadSize = int64(4 << 30)
	restartDelay                = 500 * time.Millisecond
)

var (
	backupRestoreMu       sync.RWMutex
	restoreRestartPending atomic.Bool
)

var coreRestoreSchema = map[string][]string{
	"settings":  {"key", "value"},
	"users":     {"username", "password"},
	"inbounds":  {"tag"},
	"outbounds": {"tag"},
	"clients":   {"name"},
}

var completeRestoreTables = []string{
	"settings",
	"tls",
	"inbounds",
	"outbounds",
	"services",
	"endpoints",
	"users",
	"tokens",
	"stats",
	"clients",
	"changes",
}

func GetDb(exclude string) ([]byte, error) {
	backupRestoreMu.RLock()
	defer backupRestoreMu.RUnlock()

	backupPath, err := createBackupFile(exclude)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := os.RemoveAll(filepath.Dir(backupPath)); err != nil {
			logger.Warning("remove temporary backup directory: ", err)
		}
	}()

	return os.ReadFile(backupPath)
}

// createBackupFile uses one SQLite statement to capture the whole database.
// VACUUM INTO is a consistent snapshot and automatically includes new tables,
// indexes and triggers without maintaining a second schema list here.
func createBackupFile(exclude string) (string, error) {
	if db == nil {
		return "", common.NewError("database is not initialized")
	}

	backupDir, err := os.MkdirTemp("", config.GetName()+"-backup-*")
	if err != nil {
		return "", common.NewErrorf("create private backup directory: %v", err)
	}
	backupPath := filepath.Join(backupDir, "backup.db")
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(backupDir)
		}
	}()

	if err := db.Exec("VACUUM INTO ?", backupPath).Error; err != nil {
		return "", common.NewErrorf("create consistent database snapshot: %v", err)
	}
	if err := os.Chmod(backupPath, 0600); err != nil {
		return "", common.NewErrorf("secure backup file: %v", err)
	}

	backupDB, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		return "", common.NewErrorf("open backup snapshot: %v", err)
	}
	backupSQL, err := backupDB.DB()
	if err != nil {
		return "", common.NewErrorf("open backup connection: %v", err)
	}
	backupSQL.SetMaxOpenConns(1)
	closed := false
	defer func() {
		if !closed {
			_ = backupSQL.Close()
		}
	}()

	excluded := parseExcludedTables(exclude)
	hasExcludedData := false
	for _, table := range []string{"stats", "changes"} {
		if excluded[table] {
			hasExcludedData = true
			if err := backupDB.Exec("DELETE FROM " + table).Error; err != nil {
				return "", common.NewErrorf("exclude %s from backup: %v", table, err)
			}
		}
	}
	// Rebuild the file so excluded rows cannot remain recoverable in free pages.
	if hasExcludedData {
		if err := backupDB.Exec("VACUUM").Error; err != nil {
			return "", common.NewErrorf("purge excluded backup data: %v", err)
		}
	}
	if err := validateSQLiteDB(backupDB, completeRestoreTables); err != nil {
		return "", common.NewErrorf("validate backup snapshot: %v", err)
	}
	if err := backupSQL.Close(); err != nil {
		return "", common.NewErrorf("close backup snapshot: %v", err)
	}
	closed = true
	keep = true
	return backupPath, nil
}

func parseExcludedTables(exclude string) map[string]bool {
	result := make(map[string]bool)
	for _, table := range strings.Split(exclude, ",") {
		table = strings.ToLower(strings.TrimSpace(table))
		if table == "stats" || table == "changes" {
			result[table] = true
		}
	}
	return result
}

func ImportDB(file multipart.File) error {
	return importDB(file, SendSighup)
}

func ImportDBReader(file io.Reader) error {
	return importDB(file, SendSighup)
}

func MaxRestoreUploadSize() int64 {
	if value, err := strconv.ParseInt(os.Getenv("SUI_MAX_RESTORE_BYTES"), 10, 64); err == nil && value > 0 && value <= 1<<50 {
		return value
	}
	return defaultMaxRestoreUploadSize
}

func importDB(file io.Reader, restart func() error) error {
	if file == nil {
		return common.NewError("database file is required")
	}
	dbPath := config.GetDBPath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 01740); err != nil {
		return common.NewErrorf("create database directory: %v", err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(dbPath), "."+config.GetName()+"-restore-*.db")
	if err != nil {
		return common.NewErrorf("create restore file: %v", err)
	}
	tempPath := tempFile.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tempFile.Close()
		}
		removeSQLiteFiles(tempPath)
	}()

	maxRestoreSize := MaxRestoreUploadSize()
	written, err := io.Copy(tempFile, io.LimitReader(file, maxRestoreSize+1))
	if err != nil {
		return common.NewErrorf("save restore file: %v", err)
	}
	if written > maxRestoreSize {
		return common.NewErrorf("restore file exceeds %d bytes", maxRestoreSize)
	}
	if err := tempFile.Sync(); err != nil {
		return common.NewErrorf("sync restore file: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		return common.NewErrorf("close restore file: %v", err)
	}
	closed = true

	if err := validateSQLiteFile(tempPath, nil); err != nil {
		return common.NewErrorf("invalid database file: %v", err)
	}
	if err := validateSUIRestoreFile(tempPath, coreRestoreSchema); err != nil {
		return common.NewErrorf("invalid s-ui database: %v", err)
	}
	if err := migration.MigrateDbAt(tempPath); err != nil {
		return common.NewErrorf("migrate restore database: %v", err)
	}

	preparedDB, err := prepareDB(tempPath)
	if err != nil {
		return common.NewErrorf("prepare restore database: %v", err)
	}
	preparedSQL, err := preparedDB.DB()
	if err != nil {
		closeDB(preparedDB)
		return common.NewErrorf("open prepared restore connection: %v", err)
	}
	if err := preparedDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		_ = preparedSQL.Close()
		return common.NewErrorf("checkpoint prepared restore database: %v", err)
	}
	if err := preparedSQL.Close(); err != nil {
		return common.NewErrorf("close prepared restore database: %v", err)
	}
	if err := validateSQLiteFile(tempPath, completeRestoreTables); err != nil {
		return common.NewErrorf("validate prepared restore database: %v", err)
	}
	if err := validateSUIRestoreFile(tempPath, coreRestoreSchema); err != nil {
		return common.NewErrorf("validate prepared s-ui database: %v", err)
	}

	// Uploading and validating use a unique private file and do not need to
	// block backups. Serialize only the live database commit and restart.
	backupRestoreMu.Lock()
	defer backupRestoreMu.Unlock()
	if err := ensureMatchingPageSize(tempPath); err != nil {
		return common.NewErrorf("restore database is incompatible: %v", err)
	}
	if err := replaceSQLiteDatabase(tempPath); err != nil {
		return common.NewErrorf("restore database: %v", err)
	}
	if restart != nil {
		if err := restart(); err != nil {
			return common.NewErrorf("database restored successfully, but app restart failed; restart manually: %v", err)
		}
	}
	return nil
}

func validateSQLiteFile(path string, requiredTables []string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	isSQLite, signatureErr := IsSQLiteDB(file)
	closeErr := file.Close()
	if signatureErr != nil {
		return signatureErr
	}
	if closeErr != nil {
		return closeErr
	}
	if !isSQLite {
		return common.NewError("invalid SQLite header")
	}

	target, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return err
	}
	sqlDB, err := target.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	return validateSQLiteDB(target, requiredTables)
}

func validateSQLiteDB(target *gorm.DB, requiredTables []string) error {
	if target == nil {
		return common.NewError("database is not initialized")
	}
	var results []string
	if err := target.Raw("PRAGMA integrity_check").Scan(&results).Error; err != nil {
		return err
	}
	if len(results) != 1 || strings.ToLower(results[0]) != "ok" {
		return common.NewErrorf("integrity_check failed: %v", results)
	}
	for _, table := range requiredTables {
		if !target.Migrator().HasTable(table) {
			return common.NewErrorf("required table %s is missing", table)
		}
	}
	return nil
}

func validateSUIRestoreFile(path string, schema map[string][]string) error {
	target, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return err
	}
	sqlDB, err := target.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()

	for table, columns := range schema {
		if !target.Migrator().HasTable(table) {
			return common.NewErrorf("required table %s is missing", table)
		}
		for _, column := range columns {
			if !target.Migrator().HasColumn(table, column) {
				return common.NewErrorf("required column %s.%s is missing", table, column)
			}
		}
	}
	var userCount int64
	if err := target.Table("users").Count(&userCount).Error; err != nil {
		return err
	}
	if userCount < 1 {
		return common.NewError("database has no users")
	}
	var configs []string
	if err := target.Table("settings").Where("key = ?", "config").Pluck("value", &configs).Error; err != nil {
		return err
	}
	if len(configs) != 1 {
		return common.NewError("database has no unique config setting")
	}
	if !json.Valid([]byte(configs[0])) {
		return common.NewError("database config setting is not valid JSON")
	}
	return nil
}

func ensureMatchingPageSize(sourcePath string) error {
	if db == nil {
		return common.NewError("database is not initialized")
	}
	var livePageSize int
	if err := db.Raw("PRAGMA page_size").Scan(&livePageSize).Error; err != nil {
		return err
	}
	source, err := gorm.Open(sqlite.Open(sourcePath), &gorm.Config{})
	if err != nil {
		return err
	}
	sourceSQL, err := source.DB()
	if err != nil {
		return err
	}
	defer sourceSQL.Close()
	var sourcePageSize int
	if err := source.Raw("PRAGMA page_size").Scan(&sourcePageSize).Error; err != nil {
		return err
	}
	if livePageSize != sourcePageSize {
		return common.NewErrorf("SQLite page size %d does not match live database page size %d", sourcePageSize, livePageSize)
	}
	return nil
}

func removeSQLiteFiles(path string) {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			logger.Warning("remove temporary database file ", candidate, ": ", err)
		}
	}
}

func IsSQLiteDB(file io.Reader) (bool, error) {
	buf := make([]byte, len(sqliteSignature))
	if _, err := io.ReadFull(file, buf); err != nil {
		return false, err
	}
	return bytes.Equal(buf, []byte(sqliteSignature)), nil
}

func SendSighup() error {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}

	// The signal handler must discard the old core's counters: the global DB
	// already points at the restored snapshot, so a normal graceful flush would
	// contaminate it with traffic from the pre-restore runtime.
	restoreRestartPending.Store(true)
	go func() {
		time.Sleep(restartDelay)
		var signalErr error
		if runtime.GOOS == "windows" {
			signalErr = process.Kill()
		} else {
			signalErr = process.Signal(syscall.SIGHUP)
		}
		if signalErr != nil {
			restoreRestartPending.Store(false)
			logger.Error("send signal SIGHUP failed:", signalErr)
		}
	}()
	return nil
}

func ConsumeRestoreRestart() bool {
	return restoreRestartPending.Swap(false)
}
