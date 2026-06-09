//go:build windows

package integrations

import "strings"

// codexArtifact describes the Codex notify hook artifact on Windows. Codex
// execs the notify array directly (no shell), so a .sh script has no
// interpreter. Instead of writing a script, we register the openusage binary
// directly in the notify array; Codex appends the event JSON as the next argv
// element, yielding `openusage telemetry hook codex <payload>`. An empty
// Template/Basename signals the installer to write no artifact file.
func codexArtifact() artifactSpec {
	return artifactSpec{
		Template:  "",
		Basename:  "",
		FileMode:  0o644,
		EscapeBin: func(s string) string { return s },
	}
}

// codexTargetFile returns the value Codex's notify array points at (the
// openusage binary) and reports that no artifact file is written on Windows.
func codexTargetFile(dirs Dirs) (path string, writesArtifact bool) {
	return dirs.OpenusageBin, false
}

// codexNotifyTOML renders the TOML notify assignment on Windows as an array of
// TOML literal (single-quoted) strings. Literal strings do not process
// backslash escapes, so a Windows path like C:\Users\...\openusage.exe is
// preserved verbatim. The trailing elements turn the binary into the full hook
// command; Codex appends the event JSON as the next argv element.
func codexNotifyTOML(exePath string) string {
	parts := []string{exePath, "telemetry", "hook", "codex"}
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		quoted = append(quoted, "'"+p+"'")
	}
	return "notify = [" + strings.Join(quoted, ", ") + "]"
}

// codexConfigured reports whether the Codex config registers the openusage
// notify hook on Windows: a notify line that invokes the binary with the
// `telemetry hook codex` subcommand.
func codexConfigured(content string) bool {
	return strings.Contains(content, "notify") &&
		strings.Contains(content, "telemetry") &&
		strings.Contains(content, "hook") &&
		strings.Contains(content, "codex")
}
