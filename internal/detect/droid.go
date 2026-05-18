package detect

import (
	"log"
	"path/filepath"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectDroid registers a local Droid account when ~/.factory/sessions/
// exists. We also accept the `droid` CLI binary on PATH as a signal so a
// freshly installed tool surfaces even before its first session runs.
func detectDroid(result *Result) {
	bin := findBinary("droid")
	sessionsDir := defaultDroidSessionsDir()
	hasSessions := sessionsDir != "" && dirExists(sessionsDir)

	if bin == "" && !hasSessions {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found Droid at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Droid",
			BinaryPath: bin,
			ConfigDir:  defaultDroidConfigDir(),
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:           "droid",
		Provider:     "droid",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if hasSessions {
		acct.SetPath("sessions_dir", sessionsDir)
		acct.SetHint("sessions_dir", sessionsDir)
		log.Printf("[detect] Droid sessions dir at %s", sessionsDir)
	}
	if dir := defaultDroidConfigDir(); dir != "" {
		acct.SetHint("data_dir", dir)
	}

	addAccount(result, acct)
}

func defaultDroidSessionsDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".factory", "sessions")
}

func defaultDroidConfigDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".factory")
}
