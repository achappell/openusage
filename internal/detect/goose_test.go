package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectGoose_NeitherBinNorDB(t *testing.T) {
	// Force PATH lookup to find nothing.
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())
	t.Setenv("PATH", "")
	t.Setenv("GOOSE_PATH_ROOT", filepath.Join(t.TempDir(), "missing-root"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "missing-xdg"))

	var result Result
	detectGoose(&result)

	if len(result.Tools) != 0 {
		t.Errorf("Tools = %d, want 0", len(result.Tools))
	}
	if len(result.Accounts) != 0 {
		t.Errorf("Accounts = %d, want 0", len(result.Accounts))
	}
}

func TestDetectGoose_DBOnlyRegistersAccount(t *testing.T) {
	// Create a sessions.db at the GOOSE_PATH_ROOT-style location.
	root := t.TempDir()
	dbDir := filepath.Join(root, "data", "sessions")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dbPath := filepath.Join(dbDir, "sessions.db")
	if err := os.WriteFile(dbPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Setenv("GOOSE_PATH_ROOT", root)
	// Prevent finding a real goose binary on the dev machine.
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", t.TempDir())
	t.Setenv("PATH", "")

	var result Result
	detectGoose(&result)

	if len(result.Accounts) != 1 {
		t.Fatalf("Accounts = %d, want 1", len(result.Accounts))
	}
	acct := result.Accounts[0]
	if acct.ID != "goose" {
		t.Errorf("acct.ID = %q, want goose", acct.ID)
	}
	if acct.Provider != "goose" {
		t.Errorf("acct.Provider = %q, want goose", acct.Provider)
	}
	if acct.Auth != "local" {
		t.Errorf("acct.Auth = %q, want local", acct.Auth)
	}
	if got := acct.Path("db_path", ""); got != dbPath {
		t.Errorf("db_path = %q, want %q", got, dbPath)
	}
}

func TestDetectGoose_BinaryRegistersTool(t *testing.T) {
	// Fabricate a goose binary on PATH.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "goose")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	t.Setenv("OPENUSAGE_DETECT_BIN_DIRS", binDir)
	t.Setenv("PATH", "")
	t.Setenv("GOOSE_PATH_ROOT", filepath.Join(t.TempDir(), "missing-root"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "missing-xdg"))

	var result Result
	detectGoose(&result)

	if len(result.Tools) != 1 {
		t.Fatalf("Tools = %d, want 1", len(result.Tools))
	}
	if result.Tools[0].Name != "Goose" {
		t.Errorf("Tool.Name = %q, want Goose", result.Tools[0].Name)
	}
	if len(result.Accounts) != 1 {
		t.Fatalf("Accounts = %d, want 1", len(result.Accounts))
	}
	if result.Accounts[0].Binary != binPath {
		t.Errorf("Account.Binary = %q, want %q", result.Accounts[0].Binary, binPath)
	}
}
