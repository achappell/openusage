package kiro

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultDBPaths_WindowsLocalAppData(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows path resolution test")
	}
	local := t.TempDir()
	roaming := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", roaming)
	t.Setenv("KIRO_DATA_DIR", "")

	paths := defaultDBPaths()
	wantLocal := filepath.Join(local, "kiro-cli", "data.sqlite3")
	wantRoaming := filepath.Join(roaming, "kiro-cli", "data.sqlite3")
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want 2 entries", paths)
	}
	if paths[0] != wantLocal {
		t.Errorf("paths[0] = %s, want %s (LOCALAPPDATA wins)", paths[0], wantLocal)
	}
	if paths[1] != wantRoaming {
		t.Errorf("paths[1] = %s, want %s (APPDATA fallback)", paths[1], wantRoaming)
	}
}

func TestDefaultDBPaths_WindowsKiroDataDirWins(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows path resolution test")
	}
	override := t.TempDir()
	local := t.TempDir()
	t.Setenv("KIRO_DATA_DIR", override)
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", "")

	paths := defaultDBPaths()
	want := filepath.Join(override, "data.sqlite3")
	if len(paths) == 0 || paths[0] != want {
		t.Errorf("paths = %v, want first entry %s", paths, want)
	}
}
