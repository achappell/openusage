//go:build windows

package integrations

import (
	"strings"
	"testing"
)

func TestCodexNotifyTOMLWindowsUsesLiteralStrings(t *testing.T) {
	exe := `C:\Users\someone\AppData\Local\openusage\openusage.exe`
	got := codexNotifyTOML(exe)

	// Must register the binary directly with the telemetry hook subcommand.
	want := "notify = ['" + exe + "', 'telemetry', 'hook', 'codex']"
	if got != want {
		t.Fatalf("codexNotifyTOML = %q, want %q", got, want)
	}

	// Literal (single-quoted) TOML strings must NOT backslash-escape the path.
	if strings.Contains(got, `\\`) {
		t.Fatalf("path was backslash-escaped (would corrupt the path): %q", got)
	}
	if !strings.Contains(got, exe) {
		t.Fatalf("verbatim exe path missing from %q", got)
	}

	// And the detector must recognize this registration as configured.
	if !codexConfigured(got + "\n") {
		t.Fatalf("codexConfigured did not recognize the Windows notify registration: %q", got)
	}
}

func TestCodexArtifactWindowsWritesNoFile(t *testing.T) {
	art := codexArtifact()
	if art.Template != "" || art.Basename != "" {
		t.Fatalf("expected no artifact on Windows, got template=%q basename=%q", art.Template, art.Basename)
	}
	dirs := Dirs{OpenusageBin: `C:\bin\openusage.exe`}
	path, writes := codexTargetFile(dirs)
	if writes {
		t.Fatal("codexTargetFile should report writesArtifact=false on Windows")
	}
	if path != dirs.OpenusageBin {
		t.Fatalf("codexTargetFile path = %q, want the binary path %q", path, dirs.OpenusageBin)
	}
}
