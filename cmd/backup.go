package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/admin8800/s-ui/config"
	"github.com/admin8800/s-ui/database"
)

func backupDb(output string, exclude string) {
	if output == "" {
		fmt.Println("backup failed: -output is required (use - for stdout)")
		return
	}
	if err := database.InitDB(config.GetDBPath()); err != nil {
		fmt.Println("backup failed:", err)
		return
	}
	data, err := database.GetDb(exclude)
	if err != nil {
		fmt.Println("backup failed:", err)
		return
	}
	if output == "-" {
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintln(os.Stderr, "backup failed:", err)
		}
		return
	}
	if err := writeBackupFile(output, data); err != nil {
		fmt.Println("backup failed:", err)
		return
	}
	fmt.Println("backup saved to", output)
}

func writeBackupFile(output string, data []byte) error {
	if _, err := os.Stat(output); err == nil {
		// Secure an existing backup before doing any new work. WriteFile's mode
		// argument does not change permissions on an existing file.
		if err := os.Chmod(output, 0600); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(output), "."+filepath.Base(output)+"-*.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tempFile.Close()
		}
		_ = os.Remove(tempPath)
	}()
	if err := tempFile.Chmod(0600); err != nil {
		return err
	}
	if _, err := tempFile.Write(data); err != nil {
		return err
	}
	if err := tempFile.Sync(); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	closed = true

	if err := os.Rename(tempPath, output); err != nil {
		// Keep the previous backup intact if the atomic replacement cannot be
		// completed. In particular, never delete the destination and retry.
		return err
	}
	return os.Chmod(output, 0600)
}
