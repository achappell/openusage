package pi

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const PathHintSessionsDirKey = "sessions_dir"

func defaultPiSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "sessions")
}

func defaultOmpSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".omp", "agent", "sessions")
}

// resolveSessionsDirs returns the set of sessions directories to scan,
// honoring a single per-account override. Only existing directories are
// returned.
func resolveSessionsDirs(acct core.AccountConfig) []string {
	if override := strings.TrimSpace(acct.Path(PathHintSessionsDirKey, "")); override != "" {
		if dirExists(override) {
			return []string{override}
		}
	}
	out := make([]string, 0, 2)
	if d := defaultPiSessionsDir(); d != "" && dirExists(d) {
		out = append(out, d)
	}
	if d := defaultOmpSessionsDir(); d != "" && dirExists(d) {
		out = append(out, d)
	}
	return out
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
