package kiro

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathHintDBKey is the AccountConfig path hint key used to override the
// resolved data.sqlite3 location. Detectors set this on auto-detected
// accounts; users can also set it in their settings.json.
const PathHintDBKey = "db_path"

// PathHintSessionsDirKey is the AccountConfig path hint key used to
// override the file-based session directory.
const PathHintSessionsDirKey = "sessions_dir"

// defaultDBPaths returns the OS-appropriate candidate paths where Kiro CLI
// stores its data.sqlite3 file, in priority order. The first existing
// file wins; if none exist, the resolver returns "".
//
// Kiro CLI is the renamed Amazon Q Developer CLI; the filename is identical
// across both products.
func defaultDBPaths() []string {
	var paths []string

	if root := strings.TrimSpace(os.Getenv("KIRO_DATA_DIR")); root != "" {
		paths = append(paths, filepath.Join(root, "data.sqlite3"))
	}

	switch runtime.GOOS {
	case "windows":
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			paths = append(paths, filepath.Join(local, "kiro-cli", "data.sqlite3"))
		}
		if roaming := strings.TrimSpace(os.Getenv("APPDATA")); roaming != "" {
			paths = append(paths, filepath.Join(roaming, "kiro-cli", "data.sqlite3"))
		}
		return paths
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return paths
	}

	switch runtime.GOOS {
	case "darwin":
		paths = append(paths,
			filepath.Join(home, "Library", "Application Support", "kiro-cli", "data.sqlite3"),
		)
	default:
		paths = append(paths, xdgDataHomePath(home, "kiro-cli", "data.sqlite3"))
	}

	return paths
}

// defaultSessionDirs returns candidate ~/.kiro file-session directories.
func defaultSessionDirs() []string {
	var paths []string

	if root := strings.TrimSpace(os.Getenv("KIRO_SESSIONS_DIR")); root != "" {
		paths = append(paths, root)
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return paths
	}
	paths = append(paths, filepath.Join(home, ".kiro", "sessions", "cli"))
	return paths
}

// xdgDataHomePath honours $XDG_DATA_HOME (falling back to ~/.local/share)
// and joins the supplied subpath components.
func xdgDataHomePath(home string, parts ...string) string {
	base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if base == "" {
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

// resolveDBPath returns the first existing candidate path on disk, preferring
// any explicit per-account override stored in AccountConfig.
//
// Returns "" when no candidate exists.
func resolveDBPath(acct core.AccountConfig) string {
	if override := strings.TrimSpace(acct.Path(PathHintDBKey, "")); override != "" {
		if fileExists(override) {
			return override
		}
	}
	for _, candidate := range defaultDBPaths() {
		if candidate == "" {
			continue
		}
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func resolveSessionsDir(acct core.AccountConfig) string {
	if override := strings.TrimSpace(acct.Path(PathHintSessionsDirKey, "")); override != "" {
		if dirExists(override) {
			return override
		}
	}
	for _, candidate := range defaultSessionDirs() {
		if candidate == "" {
			continue
		}
		if dirExists(candidate) {
			return candidate
		}
	}
	return ""
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
