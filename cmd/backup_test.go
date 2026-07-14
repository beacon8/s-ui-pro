package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupDbRestrictsExistingOutputPermissions(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	output := filepath.Join(t.TempDir(), "backup.db")
	if err := os.WriteFile(output, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(output, 0644); err != nil {
		t.Fatal(err)
	}

	backupDb(output, "")

	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("backup permissions = %04o, want 0600", got)
	}
}
