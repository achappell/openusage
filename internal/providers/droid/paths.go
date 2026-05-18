package droid

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathHintSessionsDirKey overrides the resolved sessions directory location.
const PathHintSessionsDirKey = "sessions_dir"

// defaultSessionsDir returns the canonical location of Droid's per-session
// settings files: $HOME/.factory/sessions
func defaultSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".factory", "sessions")
}

// resolveSessionsDir returns the path to the sessions directory, preferring
// an explicit per-account override.
func resolveSessionsDir(acct core.AccountConfig) string {
	if override := strings.TrimSpace(acct.Path(PathHintSessionsDirKey, "")); override != "" {
		if dirExists(override) {
			return override
		}
	}
	if def := defaultSessionsDir(); def != "" && dirExists(def) {
		return def
	}
	return ""
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
