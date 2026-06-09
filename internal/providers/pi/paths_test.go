package pi

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// setHome points the home-directory resolver at dir on every OS. On Windows
// os.UserHomeDir() reads %USERPROFILE% (not $HOME), so both must be set or the
// resolver lands on the real user profile instead of the temp fixture.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
}

func TestResolveSessionsDirs_OverrideWins(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	override := t.TempDir()
	acct := core.AccountConfig{ID: "pi", Provider: "pi"}
	acct.SetPath("sessions_dir", override)

	dirs := resolveSessionsDirs(acct)
	if len(dirs) != 1 || dirs[0] != override {
		t.Errorf("dirs = %v, want [%s]", dirs, override)
	}
}

func TestResolveSessionsDirs_OverrideMissingFallsThrough(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	acct := core.AccountConfig{ID: "pi", Provider: "pi"}
	acct.SetPath("sessions_dir", filepath.Join(t.TempDir(), "does-not-exist"))

	dirs := resolveSessionsDirs(acct)
	for _, d := range dirs {
		if !dirExists(d) {
			t.Errorf("returned non-existent dir %q", d)
		}
	}
}

func TestResolveSessionsDirs_OnlySessionsDirSet(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	piOverride := t.TempDir()
	// Also create the default OMP dir so we can verify it's still picked up
	// independently of the pi override.
	ompDefault := filepath.Join(home, ".omp", "agent", "sessions")
	if err := os.MkdirAll(ompDefault, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	acct := core.AccountConfig{ID: "pi", Provider: "pi"}
	acct.SetPath("sessions_dir", piOverride)

	dirs := resolveSessionsDirs(acct)
	want := []string{piOverride, ompDefault}
	if len(dirs) != len(want) {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
	for i, p := range want {
		if dirs[i] != p {
			t.Errorf("dirs[%d] = %s, want %s", i, dirs[i], p)
		}
	}
}

func TestResolveSessionsDirs_OnlyOmpSessionsDirSet(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	ompOverride := t.TempDir()
	piDefault := filepath.Join(home, ".pi", "agent", "sessions")
	if err := os.MkdirAll(piDefault, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	acct := core.AccountConfig{ID: "pi", Provider: "pi"}
	acct.SetPath("omp_sessions_dir", ompOverride)

	dirs := resolveSessionsDirs(acct)
	want := []string{piDefault, ompOverride}
	if len(dirs) != len(want) {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
	for i, p := range want {
		if dirs[i] != p {
			t.Errorf("dirs[%d] = %s, want %s", i, dirs[i], p)
		}
	}
}

func TestResolveSessionsDirs_BothOverridesSet(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	piOverride := t.TempDir()
	ompOverride := t.TempDir()

	acct := core.AccountConfig{ID: "pi", Provider: "pi"}
	acct.SetPath("sessions_dir", piOverride)
	acct.SetPath("omp_sessions_dir", ompOverride)

	dirs := resolveSessionsDirs(acct)
	want := []string{piOverride, ompOverride}
	if len(dirs) != len(want) {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
	for i, p := range want {
		if dirs[i] != p {
			t.Errorf("dirs[%d] = %s, want %s", i, dirs[i], p)
		}
	}
}

func TestResolveSessionsDirs_NeitherOverrideSet(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	piDefault := filepath.Join(home, ".pi", "agent", "sessions")
	ompDefault := filepath.Join(home, ".omp", "agent", "sessions")
	for _, d := range []string{piDefault, ompDefault} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	dirs := resolveSessionsDirs(core.AccountConfig{ID: "pi", Provider: "pi"})
	want := []string{piDefault, ompDefault}
	if len(dirs) != len(want) {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
	for i, p := range want {
		if dirs[i] != p {
			t.Errorf("dirs[%d] = %s, want %s", i, dirs[i], p)
		}
	}
}

func TestDirExists(t *testing.T) {
	if dirExists("") {
		t.Error("dirExists(\"\") = true, want false")
	}
	if dirExists(filepath.Join(t.TempDir(), "missing")) {
		t.Error("missing dir reported as existing")
	}
	if !dirExists(t.TempDir()) {
		t.Error("temp dir reported as missing")
	}
}
