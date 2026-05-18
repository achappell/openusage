package detect

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectGoose registers a local Goose account when either the CLI binary is
// on PATH or a sessions.db exists at one of the expected locations.
//
// We intentionally accept "binary exists but no DB yet" — the user might
// have just installed it and not run a session. The provider's Fetch
// handles a missing DB gracefully (returns OK with a friendly message).
func detectGoose(result *Result) {
	bin := findBinary("goose")
	dbPath := firstExistingGooseDBPath()

	if bin == "" && dbPath == "" {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found Goose at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Goose",
			BinaryPath: bin,
			ConfigDir:  defaultGooseDataDir(),
			Type:       "cli",
		})
	}

	if bin == "" && dbPath == "" {
		// Neither signal present; nothing to register.
		return
	}

	acct := core.AccountConfig{
		ID:           "goose",
		Provider:     "goose",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if dbPath != "" {
		acct.SetPath("db_path", dbPath)
		acct.SetHint("db_path", dbPath)
		log.Printf("[detect] Goose sessions.db at %s", dbPath)
	}
	if dir := defaultGooseDataDir(); dir != "" {
		acct.SetHint("data_dir", dir)
	}

	addAccount(result, acct)
}

// firstExistingGooseDBPath returns the first existing sessions.db across
// the candidate locations. Mirrors the resolver inside the goose provider
// package without importing it (detect lives upstream of providers).
func firstExistingGooseDBPath() string {
	for _, p := range gooseDBCandidates() {
		if p != "" && fileExists(p) {
			return p
		}
	}
	return ""
}

func gooseDBCandidates() []string {
	var paths []string

	if root := strings.TrimSpace(os.Getenv("GOOSE_PATH_ROOT")); root != "" {
		paths = append(paths, filepath.Join(root, "data", "sessions", "sessions.db"))
	}

	home := homeDir()
	if home == "" {
		return paths
	}

	xdgData := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if xdgData == "" {
		xdgData = filepath.Join(home, ".local", "share")
	}

	switch runtime.GOOS {
	case "darwin":
		paths = append(paths,
			filepath.Join(home, "Library", "Application Support", "Block", "goose", "sessions", "sessions.db"),
			filepath.Join(home, "Library", "Application Support", "goose", "sessions", "sessions.db"),
			filepath.Join(xdgData, "goose", "sessions", "sessions.db"),
		)
	case "linux":
		paths = append(paths,
			filepath.Join(xdgData, "goose", "sessions", "sessions.db"),
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
			filepath.Join(xdgData, "goose", "sessions", "sessions.db"),
		)
	}
	return paths
}

// defaultGooseDataDir returns the most likely data directory parent for
// surfacing in the detected-tool record.
func defaultGooseDataDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	if root := strings.TrimSpace(os.Getenv("GOOSE_PATH_ROOT")); root != "" {
		return filepath.Join(root, "data")
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Block", "goose")
	case "linux":
		base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
		if base == "" {
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, "goose")
	case "windows":
		appData := strings.TrimSpace(os.Getenv("APPDATA"))
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Block", "goose")
	}
	return ""
}
