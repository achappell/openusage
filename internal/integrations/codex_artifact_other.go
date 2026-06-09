//go:build !windows

package integrations

import (
	"path/filepath"
	"strings"
)

// codexArtifact describes the Codex notify hook artifact on Unix: a POSIX
// shell script (codex-notify.sh) that Codex execs directly with the event
// JSON as argv[1].
func codexArtifact() artifactSpec {
	return artifactSpec{
		Template:  codexTemplate,
		Basename:  "codex-notify.sh",
		FileMode:  0o755,
		EscapeBin: escapeForShellString,
	}
}

// codexTargetFile returns the path of the artifact Codex's notify array points
// at, and whether an artifact file is written. On Unix this is the shell
// script under the hooks dir.
func codexTargetFile(dirs Dirs) (path string, writesArtifact bool) {
	return filepath.Join(dirs.HooksDir, "codex-notify.sh"), true
}

// codexNotifyTOML renders the TOML notify assignment. On Unix this is a single
// element array containing the script path (basic string, matching the prior
// behavior exactly).
func codexNotifyTOML(targetFile string) string {
	return "notify = [\"" + targetFile + "\"]"
}

// codexConfigured reports whether the Codex config content registers the
// openusage notify hook. On Unix this is the presence of the script reference.
func codexConfigured(content string) bool {
	return strings.Contains(content, "notify") && strings.Contains(content, "codex-notify.sh")
}
