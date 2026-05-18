package detect

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectHermes registers a local Hermes Agent account when either the CLI
// binary is on PATH or a state.db exists at one of the expected locations
// (~/.hermes/state.db or $HERMES_HOME/state.db).
//
// "Binary on PATH but no DB yet" is acceptable: the provider's Fetch handles
// a missing DB gracefully.
func detectHermes(result *Result) {
	bin := findBinary("hermes")
	dbPath := firstExistingHermesDBPath()

	if bin == "" && dbPath == "" {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found Hermes at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Hermes Agent",
			BinaryPath: bin,
			ConfigDir:  defaultHermesDataDir(),
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:           "hermes",
		Provider:     "hermes",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if dbPath != "" {
		acct.SetPath("db_path", dbPath)
		acct.SetHint("db_path", dbPath)
		log.Printf("[detect] Hermes state.db at %s", dbPath)
	}
	if dir := defaultHermesDataDir(); dir != "" {
		acct.SetHint("data_dir", dir)
	}

	addAccount(result, acct)
}

func firstExistingHermesDBPath() string {
	for _, p := range hermesDBCandidates() {
		if p != "" && fileExists(p) {
			return p
		}
	}
	return ""
}

func hermesDBCandidates() []string {
	var paths []string

	if root := strings.TrimSpace(os.Getenv("HERMES_HOME")); root != "" {
		paths = append(paths, filepath.Join(root, "state.db"))
	}

	home := homeDir()
	if home == "" {
		return paths
	}
	paths = append(paths, filepath.Join(home, ".hermes", "state.db"))
	return paths
}

func defaultHermesDataDir() string {
	if root := strings.TrimSpace(os.Getenv("HERMES_HOME")); root != "" {
		return root
	}
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".hermes")
}
