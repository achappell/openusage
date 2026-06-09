//go:build windows

package integrations

import (
	_ "embed"
)

//go:embed assets/claude-hook.cmd.tpl
var claudeCmdTemplate string

// escapeForWindowsCmd transforms the openusage binary path for substitution
// into a .cmd batch file where the path is wrapped in double quotes. Unlike a
// POSIX shell, cmd.exe treats backslashes literally and does not perform $
// expansion, so backslashes in a Windows path (C:\...) must NOT be escaped.
// The path sits inside "...", so the only character we cannot represent is a
// literal double quote; such paths are not expected for an executable, so we
// leave the value otherwise untouched.
func escapeForWindowsCmd(value string) string {
	return value
}

// claudeArtifact describes the platform-specific Claude Code hook artifact.
// On Windows this is a .cmd batch file that Claude Code runs via cmd.exe and
// pipes the hook payload on STDIN.
func claudeArtifact() artifactSpec {
	return artifactSpec{
		Template:  claudeCmdTemplate,
		Basename:  "claude-hook.cmd",
		FileMode:  0o755,
		EscapeBin: escapeForWindowsCmd,
	}
}
