package pi

import (
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestResolveSessionsDirs_OverrideWins(t *testing.T) {
	override := t.TempDir()
	acct := core.AccountConfig{ID: "pi", Provider: "pi"}
	acct.SetPath("sessions_dir", override)

	dirs := resolveSessionsDirs(acct)
	if len(dirs) != 1 || dirs[0] != override {
		t.Errorf("dirs = %v, want [%s]", dirs, override)
	}
}

func TestResolveSessionsDirs_OverrideMissingFallsThrough(t *testing.T) {
	acct := core.AccountConfig{ID: "pi", Provider: "pi"}
	acct.SetPath("sessions_dir", filepath.Join(t.TempDir(), "does-not-exist"))

	// With no real default dirs we expect an empty slice (unless the test
	// machine happens to have ~/.pi/agent/sessions or ~/.omp/agent/sessions,
	// in which case the result may legitimately be non-empty — we only
	// assert that the missing override didn't lock the resolver to it).
	dirs := resolveSessionsDirs(acct)
	for _, d := range dirs {
		if !dirExists(d) {
			t.Errorf("returned non-existent dir %q", d)
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
