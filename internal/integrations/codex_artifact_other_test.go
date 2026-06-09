//go:build !windows

package integrations

import "testing"

func TestCodexNotifyTOMLUnixUnchanged(t *testing.T) {
	script := "/home/user/.config/openusage/hooks/codex-notify.sh"
	got := codexNotifyTOML(script)
	want := "notify = [\"" + script + "\"]"
	if got != want {
		t.Fatalf("codexNotifyTOML = %q, want %q", got, want)
	}
	if !codexConfigured(got + "\n") {
		t.Fatalf("codexConfigured did not recognize the Unix notify registration: %q", got)
	}
}

func TestCodexArtifactUnixWritesScript(t *testing.T) {
	art := codexArtifact()
	if art.Basename != "codex-notify.sh" {
		t.Fatalf("Unix codex basename = %q, want codex-notify.sh", art.Basename)
	}
	if art.Template == "" {
		t.Fatal("Unix codex template should be non-empty")
	}
	dirs := Dirs{HooksDir: "/home/user/.config/openusage/hooks"}
	path, writes := codexTargetFile(dirs)
	if !writes {
		t.Fatal("codexTargetFile should report writesArtifact=true on Unix")
	}
	if path != "/home/user/.config/openusage/hooks/codex-notify.sh" {
		t.Fatalf("unexpected target path %q", path)
	}
}
