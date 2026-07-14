package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/admin8800/s-ui/config"
)

func TestMigrateCommandReturnsNonZeroOnFailure(t *testing.T) {
	if os.Getenv("SUI_MIGRATE_FAILURE_HELPER") == "1" {
		os.Args = []string{"sui", "migrate"}
		ParseCmd()
		t.Fatal("migrate command unexpectedly returned")
	}

	dbDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dbDir, config.GetName()+".db"), []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestMigrateCommandReturnsNonZeroOnFailure$")
	command.Env = append(os.Environ(),
		"SUI_MIGRATE_FAILURE_HELPER=1",
		"SUI_DB_FOLDER="+dbDir,
	)
	err := command.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() == 0 {
		t.Fatalf("migrate failure exit error = %v", err)
	}
}
