//go:build !windows

package integrations

// claudeArtifact describes the platform-specific Claude Code hook artifact.
// On Unix this is the POSIX shell script (claude-hook.sh).
func claudeArtifact() artifactSpec {
	return artifactSpec{
		Template:  claudeTemplate,
		Basename:  "claude-hook.sh",
		FileMode:  0o755,
		EscapeBin: escapeForShellString,
	}
}
