package detect

import (
	"log"
	"path/filepath"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// detectKimiCLI registers a local Kimi CLI account when ~/.kimi/sessions/
// exists, ~/.kimi/config.json exists, or the `kimi` binary is on PATH. The
// account id is "kimi_cli", which is intentionally distinct from the
// API-key based "moonshot-ai" account so the two coexist as separate tiles.
func detectKimiCLI(result *Result) {
	bin := findBinary("kimi")
	sessionsDir := defaultKimiSessionsDir()
	configPath := defaultKimiConfigPath()
	hasSessions := sessionsDir != "" && dirExists(sessionsDir)
	hasConfig := configPath != "" && fileExists(configPath)

	if bin == "" && !hasSessions && !hasConfig {
		return
	}

	if bin != "" {
		log.Printf("[detect] Found Kimi CLI at %s", bin)
		result.Tools = append(result.Tools, DetectedTool{
			Name:       "Kimi CLI",
			BinaryPath: bin,
			ConfigDir:  defaultKimiConfigDir(),
			Type:       "cli",
		})
	}

	acct := core.AccountConfig{
		ID:           "kimi_cli",
		Provider:     "kimi_cli",
		Auth:         "local",
		Binary:       bin,
		RuntimeHints: make(map[string]string),
	}
	if hasSessions {
		acct.SetPath("sessions_dir", sessionsDir)
		acct.SetHint("sessions_dir", sessionsDir)
		log.Printf("[detect] Kimi CLI sessions dir at %s", sessionsDir)
	}
	if hasConfig {
		acct.SetPath("config_path", configPath)
		acct.SetHint("config_path", configPath)
	}
	if dir := defaultKimiConfigDir(); dir != "" {
		acct.SetHint("data_dir", dir)
	}

	addAccount(result, acct)
}

func defaultKimiSessionsDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".kimi", "sessions")
}

func defaultKimiConfigPath() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".kimi", "config.json")
}

func defaultKimiConfigDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".kimi")
}
