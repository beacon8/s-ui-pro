//go:build cgo

package database

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/admin8800/s-ui/config"
	"github.com/admin8800/s-ui/util/common"
	sqlite3 "github.com/mattn/go-sqlite3"
)

const (
	restoreConnectionTimeout = 30 * time.Second
	restoreOperationTimeout  = 10 * time.Minute
)

// replaceSQLiteDatabase keeps the pool's only live connection for the whole
// operation. It first snapshots the current database, restores the candidate,
// validates the live result, and rolls back from that snapshot on any failure.
func replaceSQLiteDatabase(sourcePath string) error {
	if db == nil {
		return common.NewError("database is not initialized")
	}
	destinationSQL, err := db.DB()
	if err != nil {
		return err
	}

	rollbackDir, err := os.MkdirTemp(filepath.Dir(config.GetDBPath()), ".s-ui-rollback-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(rollbackDir)
	rollbackPath := filepath.Join(rollbackDir, "database.db")

	ctx, cancel := context.WithTimeout(context.Background(), restoreConnectionTimeout)
	defer cancel()
	destinationConn, err := destinationSQL.Conn(ctx)
	if err != nil {
		return err
	}
	defer destinationConn.Close()

	sourceSQL, err := sql.Open("sqlite3", sqliteReadOnlyDSN(sourcePath))
	if err != nil {
		return err
	}
	sourceSQL.SetMaxOpenConns(1)
	defer sourceSQL.Close()
	if err := sourceSQL.PingContext(ctx); err != nil {
		return err
	}
	sourceConn, err := sourceSQL.Conn(ctx)
	if err != nil {
		return err
	}
	defer sourceConn.Close()

	rollbackSQL, err := sql.Open("sqlite3", rollbackPath+"?_busy_timeout=10000")
	if err != nil {
		return err
	}
	rollbackSQL.SetMaxOpenConns(1)
	defer rollbackSQL.Close()
	if err := rollbackSQL.PingContext(ctx); err != nil {
		return err
	}
	if err := os.Chmod(rollbackPath, 0600); err != nil {
		return err
	}
	rollbackConn, err := rollbackSQL.Conn(ctx)
	if err != nil {
		return err
	}
	defer rollbackConn.Close()

	if err := copySQLiteWithTimeout(rollbackConn, destinationConn); err != nil {
		return common.NewErrorf("snapshot live database before restore: %v", err)
	}
	if err := copySQLiteWithTimeout(destinationConn, sourceConn); err != nil {
		rollbackErr := rollbackSQLiteDatabase(destinationConn, rollbackConn)
		return errors.Join(err, wrapRollbackError(rollbackErr))
	}
	validationCtx, cancelValidation := context.WithTimeout(context.Background(), restoreOperationTimeout)
	validationErr := validateLiveConnection(validationCtx, destinationConn)
	cancelValidation()
	if validationErr != nil {
		rollbackErr := rollbackSQLiteDatabase(destinationConn, rollbackConn)
		return errors.Join(validationErr, wrapRollbackError(rollbackErr))
	}
	return nil
}

func rollbackSQLiteDatabase(destinationConn, rollbackConn *sql.Conn) error {
	return copySQLiteWithTimeout(destinationConn, rollbackConn)
}

func copySQLiteWithTimeout(destinationConn, sourceConn *sql.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), restoreOperationTimeout)
	defer cancel()
	return copySQLiteConnections(ctx, destinationConn, sourceConn)
}

func sqliteReadOnlyDSN(path string) string {
	normalized := strings.ReplaceAll(filepath.ToSlash(path), `\`, "/")
	if len(normalized) >= 2 && normalized[1] == ':' && normalized[0] != '/' {
		normalized = "/" + normalized
	}
	fileURL := (&url.URL{Scheme: "file", Path: normalized}).String()
	return fileURL + "?mode=ro&_busy_timeout=10000"
}

func copySQLiteConnections(ctx context.Context, destinationConn, sourceConn *sql.Conn) error {
	return destinationConn.Raw(func(destinationDriver any) error {
		destination, ok := destinationDriver.(*sqlite3.SQLiteConn)
		if !ok {
			return common.NewErrorf("unexpected destination SQLite driver %T", destinationDriver)
		}
		return sourceConn.Raw(func(sourceDriver any) error {
			source, ok := sourceDriver.(*sqlite3.SQLiteConn)
			if !ok {
				return common.NewErrorf("unexpected source SQLite driver %T", sourceDriver)
			}
			return runSQLiteBackup(ctx, destination, source)
		})
	})
}

func runSQLiteBackup(ctx context.Context, destination, source *sqlite3.SQLiteConn) error {
	backup, err := destination.Backup("main", source, "main")
	if err != nil {
		return err
	}
	for {
		done, stepErr := backup.Step(256)
		if stepErr != nil {
			return errors.Join(stepErr, backup.Finish())
		}
		if done {
			return backup.Finish()
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), backup.Finish())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func validateLiveConnection(ctx context.Context, connection *sql.Conn) error {
	var integrity string
	if err := connection.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if strings.ToLower(integrity) != "ok" {
		return common.NewErrorf("live integrity_check failed: %s", integrity)
	}
	for _, table := range completeRestoreTables {
		var exists int
		if err := connection.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)", table,
		).Scan(&exists); err != nil {
			return err
		}
		if exists != 1 {
			return common.NewErrorf("restored live database is missing table %s", table)
		}
	}
	return nil
}

func wrapRollbackError(err error) error {
	if err == nil {
		return nil
	}
	return common.NewErrorf("restore rollback failed: %v", err)
}
