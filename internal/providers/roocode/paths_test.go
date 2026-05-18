package roocode

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// TestVSCodeGlobalStorageRoots_ContainsKnownVariants asserts that the
// path enumeration includes every VS Code variant we claim to support.
// We can't assert anything about disk state — only that the candidate
// list reflects our variant table.
func TestVSCodeGlobalStorageRoots_ContainsKnownVariants(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Force XDG_CONFIG_HOME to the empty default so Linux paths fall back
	// to ~/.config. (The env var is consulted only on Linux.)
	t.Setenv("XDG_CONFIG_HOME", "")

	roots := VSCodeGlobalStorageRoots()
	if len(roots) == 0 {
		t.Fatal("expected at least one candidate root")
	}

	// Build a needle per known variant. The path component (mac vs linux
	// vs windows) depends on GOOS so we pick the right field by runtime.
	for _, v := range knownVariants {
		var needle string
		switch runtime.GOOS {
		case "darwin":
			needle = v.macSupportDir
		case "linux":
			needle = v.linuxConfigDir
		case "windows":
			needle = v.winAppDataDir
		default:
			needle = v.linuxConfigDir
		}
		found := false
		for _, root := range roots {
			if containsPathSegment(root, needle) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("variant %q (segment %q) not in roots: %v", v.Name, needle, roots)
		}
	}
}

// TestFindTaskDirs_DiscoversAcrossVariants creates fake globalStorage
// trees for two VS Code variants and verifies FindTaskDirs returns every
// per-task subdir.
func TestFindTaskDirs_DiscoversAcrossVariants(t *testing.T) {
	// Skip on Windows because the path layout is host-dependent and harder
	// to fake portably; the macOS/Linux branches share enough code to give
	// confidence.
	if runtime.GOOS == "windows" {
		t.Skip("path layout is Windows-specific; tested on darwin/linux")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	// Build two variant globalStorage trees with one task subdir each.
	var roots []string
	switch runtime.GOOS {
	case "darwin":
		roots = []string{
			filepath.Join(home, "Library", "Application Support", "Code", "User", "globalStorage"),
			filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage"),
		}
	default: // linux + fallback
		roots = []string{
			filepath.Join(home, ".config", "Code", "User", "globalStorage"),
			filepath.Join(home, ".config", "Cursor", "User", "globalStorage"),
		}
	}
	for i, root := range roots {
		tasksDir := filepath.Join(root, RooExtensionSubdir, "tasks", "task-"+itoa(i))
		if err := os.MkdirAll(tasksDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	dirs := FindTaskDirs(RooExtensionSubdir)
	if got, want := len(dirs), 2; got != want {
		t.Fatalf("dirs = %d (%v), want %d", got, dirs, want)
	}
}

// TestFindTaskDirs_EmptyForUnknownExtension verifies the helper returns
// an empty slice (not an error) when the extension subdir doesn't exist.
func TestFindTaskDirs_EmptyForUnknownExtension(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := FindTaskDirs("nonexistent.extension-id"); len(got) != 0 {
		t.Errorf("dirs = %v, want empty", got)
	}
}

// TestAnyExtensionInstalled_DetectsExtensionDir confirms the boolean
// helper reports installation correctly when the directory exists but
// has no tasks yet.
func TestAnyExtensionInstalled_DetectsExtensionDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path layout is Windows-specific; tested on darwin/linux")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	var extDir string
	switch runtime.GOOS {
	case "darwin":
		extDir = filepath.Join(home, "Library", "Application Support", "Code", "User", "globalStorage", RooExtensionSubdir)
	default:
		extDir = filepath.Join(home, ".config", "Code", "User", "globalStorage", RooExtensionSubdir)
	}
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !AnyExtensionInstalled(RooExtensionSubdir) {
		t.Error("AnyExtensionInstalled = false, want true")
	}
	if AnyExtensionInstalled("definitely.not-installed") {
		t.Error("AnyExtensionInstalled returned true for missing subdir")
	}
}

// TestResolveTaskDirs_AccountOverride takes precedence over auto-discovery.
func TestResolveTaskDirs_AccountOverride(t *testing.T) {
	override := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := os.MkdirAll(filepath.Join(override, "task-"+itoa(i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Drop a non-dir entry to confirm we skip files.
	if err := os.WriteFile(filepath.Join(override, "not-a-task.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	acct := core.AccountConfig{}
	acct.SetPath("tasks_dir", override)
	dirs := resolveTaskDirs(acct, RooExtensionSubdir)
	if got, want := len(dirs), 3; got != want {
		t.Fatalf("dirs = %d (%v), want %d", got, dirs, want)
	}
}

// containsPathSegment reports whether path contains the named component as
// a discrete segment (so that `Code` doesn't falsely match `Code - Insiders`).
func containsPathSegment(path, segment string) bool {
	for _, part := range filepath.SplitList(path) {
		_ = part
	}
	// SplitList works on PATH-style strings; for filesystem paths we walk
	// components by filepath.Dir.
	for p := path; p != "" && p != filepath.Dir(p); p = filepath.Dir(p) {
		if filepath.Base(p) == segment {
			return true
		}
	}
	return false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + (i % 10))
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
