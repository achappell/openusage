package openclaw

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

func TestResolveAgentsDirs_OverrideWins(t *testing.T) {
	override := t.TempDir()
	home := t.TempDir()
	setHome(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".openclaw", "agents"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	acct := core.AccountConfig{}
	acct.SetPath("agents_dir", override)

	dirs := resolveAgentsDirs(acct)
	if len(dirs) != 1 || dirs[0] != override {
		t.Errorf("dirs = %v, want [%s]", dirs, override)
	}
}

func TestResolveAgentsDirs_OverrideMissingFallsThroughToDefault(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	def := filepath.Join(home, ".openclaw", "agents")
	if err := os.MkdirAll(def, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	acct := core.AccountConfig{}
	acct.SetPath("agents_dir", filepath.Join(t.TempDir(), "does-not-exist"))

	dirs := resolveAgentsDirs(acct)
	if len(dirs) != 1 || dirs[0] != def {
		t.Errorf("dirs = %v, want [%s] (override missing should fall through)", dirs, def)
	}
}

func TestResolveAgentsDirs_OverrideMissingNoDefaults(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	acct := core.AccountConfig{}
	acct.SetPath("agents_dir", filepath.Join(t.TempDir(), "does-not-exist"))

	dirs := resolveAgentsDirs(acct)
	if len(dirs) != 0 {
		t.Errorf("dirs = %v, want empty when override missing and no defaults exist", dirs)
	}
}

func TestResolveAgentsDirs_DefaultPlusLegacyUnion(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	def := filepath.Join(home, ".openclaw", "agents")
	clawd := filepath.Join(home, ".clawdbot", "agents")
	molt := filepath.Join(home, ".moltbot", "agents")
	for _, d := range []string{def, clawd, molt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	got := resolveAgentsDirs(core.AccountConfig{})
	want := []string{def, clawd, molt}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("got[%d] = %s, want %s", i, got[i], p)
		}
	}
}

func TestResolveAgentsDirs_DeDup(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".openclaw", "agents"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Call twice, ensure we don't get duplicates.
	got := resolveAgentsDirs(core.AccountConfig{})
	if len(got) != 1 {
		t.Errorf("got %v, want 1 entry", got)
	}
}

func TestResolveAgentsDirs_NoneExist(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	got := resolveAgentsDirs(core.AccountConfig{})
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
