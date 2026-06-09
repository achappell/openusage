package codebuff

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// setHome redirects the home directory for the test. defaultDataDirs()
// resolves via os.UserHomeDir(), which reads %USERPROFILE% on Windows, not
// $HOME, so we must set both for tests to be portable.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
}

func TestResolveDataDirs_OverrideWins(t *testing.T) {
	dir := t.TempDir()
	override := filepath.Join(dir, "explicit")
	if err := os.MkdirAll(override, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	acct := core.AccountConfig{ID: "codebuff", Provider: "codebuff", Auth: "local"}
	acct.SetPath("data_dir", override)

	setHome(t, t.TempDir())
	t.Setenv("CODEBUFF_DATA_DIR", "")

	got := resolveDataDirs(acct)
	if len(got) == 0 {
		t.Fatal("got empty dirs")
	}
	if got[0] != override {
		t.Errorf("first dir = %q, want %q", got[0], override)
	}
}

func TestResolveDataDirs_EnvOverrideAppended(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	// Create stable manicode root.
	stable := filepath.Join(home, ".config", "manicode")
	if err := os.MkdirAll(stable, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	envDir := filepath.Join(t.TempDir(), "envroot")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("mkdir env: %v", err)
	}
	t.Setenv("CODEBUFF_DATA_DIR", envDir)

	got := resolveDataDirs(core.AccountConfig{ID: "codebuff"})
	if len(got) != 2 {
		t.Fatalf("got %d dirs, want 2: %v", len(got), got)
	}
	if got[0] != stable {
		t.Errorf("first = %q, want stable %q", got[0], stable)
	}
	if got[1] != envDir {
		t.Errorf("second = %q, want env %q", got[1], envDir)
	}
}

func TestResolveDataDirs_MissingDirsSkipped(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("CODEBUFF_DATA_DIR", "")

	got := resolveDataDirs(core.AccountConfig{ID: "codebuff"})
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestResolveDataDirs_AllChannelsDetected(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("CODEBUFF_DATA_DIR", "")

	for _, name := range []string{"manicode", "manicode-dev", "manicode-staging"} {
		if err := os.MkdirAll(filepath.Join(home, ".config", name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}

	got := resolveDataDirs(core.AccountConfig{ID: "codebuff"})
	if len(got) != 3 {
		t.Fatalf("got %d, want 3: %v", len(got), got)
	}
}
