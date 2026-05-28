package detect

import (
	"log"
	"path/filepath"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectPi registers a local Pi account when either of the known sessions
// directories exists or the `pi` CLI binary is on PATH.
func detectPi(result *Result) {
	bin := findBinary("pi")
	piDir := defaultPiSessionsDir()
	ompDir := defaultOmpSessionsDir()
	hasPi := piDir != "" && dirExists(piDir)
	hasOmp := ompDir != "" && dirExists(ompDir)

	if bin == "" && !hasPi && !hasOmp {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found Pi at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Pi",
			BinaryPath: bin,
			ConfigDir:  defaultPiConfigDir(),
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:           "pi",
		Provider:     "pi",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if hasPi {
		acct.SetHint("sessions_dir", piDir)
		log.Printf("[detect] Pi sessions dir at %s", piDir)
	}
	if hasOmp {
		acct.SetHint("omp_sessions_dir", ompDir)
		log.Printf("[detect] Pi (omp) sessions dir at %s", ompDir)
	}
	if dir := defaultPiConfigDir(); dir != "" {
		acct.SetHint("data_dir", dir)
	}

	addAccount(result, acct)
}

func defaultPiSessionsDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "sessions")
}

func defaultOmpSessionsDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".omp", "agent", "sessions")
}

func defaultPiConfigDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".pi")
}
