package detect

import (
	"log"
	"path/filepath"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectMux registers a local Mux account when ~/.mux/sessions/ exists. We
// also accept the `mux` CLI binary on PATH as a signal so a freshly-installed
// tool surfaces even before its first workspace runs.
func detectMux(result *Result) {
	bin := findBinary("mux")
	sessionsDir := defaultMuxSessionsDir()
	hasSessions := sessionsDir != "" && dirExists(sessionsDir)

	if bin == "" && !hasSessions {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found Mux at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Mux",
			BinaryPath: bin,
			ConfigDir:  defaultMuxConfigDir(),
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:           "mux",
		Provider:     "mux",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if hasSessions {
		acct.SetPath("sessions_dir", sessionsDir)
		acct.SetHint("sessions_dir", sessionsDir)
		log.Printf("[detect] Mux sessions dir at %s", sessionsDir)
	}
	if dir := defaultMuxConfigDir(); dir != "" {
		acct.SetHint("data_dir", dir)
	}

	addAccount(result, acct)
}

func defaultMuxSessionsDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".mux", "sessions")
}

func defaultMuxConfigDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".mux")
}
