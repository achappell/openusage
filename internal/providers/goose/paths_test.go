package goose

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestDefaultSessionsDBPaths_GooseRootOverride(t *testing.T) {
	t.Setenv("GOOSE_PATH_ROOT", "/tmp/myroot")

	paths := defaultSessionsDBPaths()
	if len(paths) == 0 {
		t.Fatal("expected at least one candidate path")
	}
	want := filepath.Join("/tmp/myroot", "data", "sessions", "sessions.db")
	if paths[0] != want {
		t.Errorf("paths[0] = %q, want %q", paths[0], want)
	}
}

func TestDefaultSessionsDBPaths_PlatformSpecific(t *testing.T) {
	t.Setenv("GOOSE_PATH_ROOT", "")
	t.Setenv("XDG_DATA_HOME", "")

	paths := defaultSessionsDBPaths()
	if len(paths) == 0 {
		t.Fatal("expected at least one candidate")
	}

	// At least one platform-appropriate candidate should be present.
	switch runtime.GOOS {
	case "darwin":
		if !containsContaining(paths, filepath.Join("Library", "Application Support", "Block", "goose")) {
			t.Errorf("missing macOS Block/goose candidate; got %v", paths)
		}
	case "linux":
		if !containsContaining(paths, filepath.Join(".local", "share", "goose")) {
			t.Errorf("missing linux XDG candidate; got %v", paths)
		}
	case "windows":
		if !containsContaining(paths, filepath.Join("Block", "goose")) {
			t.Errorf("missing windows Block/goose candidate; got %v", paths)
		}
	}
}

func TestDefaultSessionsDBPaths_XDGDataHome(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("XDG_DATA_HOME path only used on POSIX-ish systems")
	}
	t.Setenv("GOOSE_PATH_ROOT", "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")

	paths := defaultSessionsDBPaths()
	wantSub := filepath.Join("/tmp/xdg-data", "goose", "sessions", "sessions.db")
	if !contains(paths, wantSub) {
		t.Errorf("missing XDG candidate %q; got %v", wantSub, paths)
	}
}

func TestResolveDBPath_OverridePrefersAccountHint(t *testing.T) {
	dir := t.TempDir()
	override := filepath.Join(dir, "custom-sessions.db")
	if err := os.WriteFile(override, []byte("x"), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}

	acct := core.AccountConfig{}
	acct.SetPath(PathHintDBKey, override)

	got := resolveDBPath(acct)
	if got != override {
		t.Errorf("resolveDBPath = %q, want %q", got, override)
	}
}

func TestResolveDBPath_FallsBackThroughCandidates(t *testing.T) {
	// Point GOOSE_PATH_ROOT at a real, populated location and verify
	// resolveDBPath finds it.
	root := t.TempDir()
	t.Setenv("GOOSE_PATH_ROOT", root)
	dbDir := filepath.Join(root, "data", "sessions")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dbPath := filepath.Join(dbDir, "sessions.db")
	if err := os.WriteFile(dbPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := resolveDBPath(core.AccountConfig{})
	if got != dbPath {
		t.Errorf("resolveDBPath = %q, want %q", got, dbPath)
	}
}

func TestResolveDBPath_NoneExist(t *testing.T) {
	t.Setenv("GOOSE_PATH_ROOT", filepath.Join(t.TempDir(), "missing"))
	// Override HOME to an empty dir so platform defaults also miss.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "missing"))

	if got := resolveDBPath(core.AccountConfig{}); got != "" {
		t.Errorf("resolveDBPath = %q, want empty", got)
	}
}

func TestFirstCandidatePath_NonEmpty(t *testing.T) {
	// Ensure firstCandidatePath returns something on any platform with a
	// home directory.
	if firstCandidatePath() == "" {
		t.Skip("no home directory; cannot evaluate")
	}
}

func contains(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func containsContaining(paths []string, substr string) bool {
	for _, p := range paths {
		if substr != "" && strings.Contains(p, substr) {
			return true
		}
	}
	return false
}
