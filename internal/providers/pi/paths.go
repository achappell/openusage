package pi

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const (
	PathHintSessionsDirKey    = "sessions_dir"
	PathHintOmpSessionsDirKey = "omp_sessions_dir"
)

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
// honoring independent per-account overrides for the Pi and OMP roots.
// Only existing directories are returned.
func resolveSessionsDirs(acct core.AccountConfig) []string {
	piRoot := strings.TrimSpace(acct.Path(PathHintSessionsDirKey, ""))
	if piRoot == "" {
		piRoot = defaultPiSessionsDir()
	}
	ompRoot := strings.TrimSpace(acct.Path(PathHintOmpSessionsDirKey, ""))
	if ompRoot == "" {
		ompRoot = defaultOmpSessionsDir()
	}

	out := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	add := func(p string) {
		if p == "" || !dirExists(p) {
			return
		}
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	add(piRoot)
	add(ompRoot)
	return out
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
