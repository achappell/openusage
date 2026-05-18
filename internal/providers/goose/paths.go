package goose

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathHintDBKey is the AccountConfig path hint key used to override the
// resolved sessions.db location. Detectors set this on auto-detected
// accounts; users can also set it in their settings.json.
const PathHintDBKey = "db_path"

// defaultSessionsDBPaths returns the OS-appropriate candidate paths where
// Goose stores its sessions.db, in priority order. The first existing file
// wins; if none exist, the resolver returns the empty string.
//
// Priority:
//  1. $GOOSE_PATH_ROOT/data/sessions/sessions.db (explicit user override)
//  2. Platform-default location (etcetera-style "Block" qualifier).
//  3. The XDG_DATA_HOME fallback on macOS/Linux for users whose installs
//     follow the linux-style layout.
//
// We deliberately probe several candidates per OS because the upstream tool
// has shipped multiple data-dir conventions over its history and users may
// have data in any one of them.
func defaultSessionsDBPaths() []string {
	var paths []string

	if root := strings.TrimSpace(os.Getenv("GOOSE_PATH_ROOT")); root != "" {
		paths = append(paths, filepath.Join(root, "data", "sessions", "sessions.db"))
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return paths
	}

	switch runtime.GOOS {
	case "darwin":
		// Upstream uses the etcetera "Block" qualifier on macOS, which
		// expands to ~/Library/Application Support/Block/goose. We also
		// probe the non-qualified variant for older installs and an
		// XDG-style data dir for users who follow Linux conventions on
		// macOS.
		paths = append(paths,
			filepath.Join(home, "Library", "Application Support", "Block", "goose", "sessions", "sessions.db"),
			filepath.Join(home, "Library", "Application Support", "goose", "sessions", "sessions.db"),
			xdgDataHomePath(home, "goose", "sessions", "sessions.db"),
		)
	case "linux":
		paths = append(paths,
			xdgDataHomePath(home, "goose", "sessions", "sessions.db"),
			// Legacy "Block/goose" subdir from etcetera's qualifier on
			// some older builds.
			filepath.Join(home, ".local", "share", "Block", "goose", "sessions", "sessions.db"),
		)
	case "windows":
		appData := strings.TrimSpace(os.Getenv("APPDATA"))
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		paths = append(paths,
			filepath.Join(appData, "Block", "goose", "data", "sessions", "sessions.db"),
			filepath.Join(appData, "goose", "data", "sessions", "sessions.db"),
		)
	default:
		paths = append(paths,
			xdgDataHomePath(home, "goose", "sessions", "sessions.db"),
		)
	}

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
// Returns "" when no candidate exists; callers should treat that as
// "no local data" rather than an error.
func resolveDBPath(acct core.AccountConfig) string {
	if override := strings.TrimSpace(acct.Path(PathHintDBKey, "")); override != "" {
		if fileExists(override) {
			return override
		}
	}
	for _, candidate := range defaultSessionsDBPaths() {
		if candidate == "" {
			continue
		}
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

// firstCandidatePath returns the first candidate path regardless of whether
// it exists. Used by detectors when surfacing "expected location" hints.
func firstCandidatePath() string {
	for _, candidate := range defaultSessionsDBPaths() {
		if candidate != "" {
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
