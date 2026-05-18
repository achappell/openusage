package roocode

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathHintTasksDirKey is the AccountConfig hint key used to override the
// resolved per-task-root directory. Detectors set this to the absolute
// path of the extension's `tasks/` directory. Users can also configure
// the path explicitly via settings.json.
const PathHintTasksDirKey = "tasks_dir"

// RooExtensionSubdir is the VS Code globalStorage subdirectory the Roo
// Code extension writes to.
const RooExtensionSubdir = "rooveterinaryinc.roo-cline"

// KiloExtensionSubdir is the VS Code globalStorage subdirectory the Kilo
// Code extension writes to.
const KiloExtensionSubdir = "kilocode.kilo-code"

// VSCodeVariant describes the per-OS layout for one VS Code-family
// installation. Roo Code and Kilo Code can both be installed under any of
// these because they ship as standard VS Code extensions; users are also
// known to install them on Cursor (which inherits VS Code's globalStorage
// layout).
type VSCodeVariant struct {
	// Name is a stable identifier ("vscode", "vscode-insiders", "vscodium",
	// "cursor", "windsurf") used for log lines and metric attribution.
	Name string

	// macSupportDir is the directory name under
	// ~/Library/Application Support/ that holds the variant's User dir.
	macSupportDir string

	// linuxConfigDir is the directory name under ~/.config/ on Linux.
	linuxConfigDir string

	// winAppDataDir is the directory name under %APPDATA% on Windows /
	// inside WSL (where /mnt/c/Users/<u>/AppData/Roaming/<dir> is reachable).
	winAppDataDir string
}

// knownVariants lists every VS Code-family install location we probe.
// Order is significant only insofar as the first match wins when the same
// extension subdir exists in multiple locations (rare, but possible if a
// user installs the extension in both Code and Code-Insiders).
var knownVariants = []VSCodeVariant{
	{Name: "vscode", macSupportDir: "Code", linuxConfigDir: "Code", winAppDataDir: "Code"},
	{Name: "vscode-insiders", macSupportDir: "Code - Insiders", linuxConfigDir: "Code - Insiders", winAppDataDir: "Code - Insiders"},
	{Name: "vscodium", macSupportDir: "VSCodium", linuxConfigDir: "VSCodium", winAppDataDir: "VSCodium"},
	{Name: "vscodium-insiders", macSupportDir: "VSCodium - Insiders", linuxConfigDir: "VSCodium - Insiders", winAppDataDir: "VSCodium - Insiders"},
	{Name: "cursor", macSupportDir: "Cursor", linuxConfigDir: "Cursor", winAppDataDir: "Cursor"},
	{Name: "windsurf", macSupportDir: "Windsurf", linuxConfigDir: "Windsurf", winAppDataDir: "Windsurf"},
}

// VSCodeGlobalStorageRoots returns every candidate VS Code globalStorage
// root we should probe for installed Roo Code / Kilo Code extensions.
// The list is OS-aware; on Linux we additionally probe Windows-mounted
// WSL paths under /mnt/c/Users so a user running OpenUsage inside WSL can
// still see usage logged by their Windows-side VS Code install.
func VSCodeGlobalStorageRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}

	var roots []string
	switch runtime.GOOS {
	case "darwin":
		appSupport := filepath.Join(home, "Library", "Application Support")
		for _, v := range knownVariants {
			roots = append(roots, filepath.Join(appSupport, v.macSupportDir, "User", "globalStorage"))
		}
	case "linux":
		config := filepath.Join(home, ".config")
		if override := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); override != "" {
			config = override
		}
		for _, v := range knownVariants {
			roots = append(roots, filepath.Join(config, v.linuxConfigDir, "User", "globalStorage"))
		}
		// WSL: probe the Windows host's AppData/Roaming. /mnt/c is the
		// conventional Windows-drive mount; we only emit these if the
		// mount actually exists so we don't return phantom candidates on
		// pure Linux installs.
		if mountedUsersDir := "/mnt/c/Users"; dirExists(mountedUsersDir) {
			entries, _ := os.ReadDir(mountedUsersDir)
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				name := entry.Name()
				if isWindowsSystemUserDir(name) {
					continue
				}
				appData := filepath.Join(mountedUsersDir, name, "AppData", "Roaming")
				if !dirExists(appData) {
					continue
				}
				for _, v := range knownVariants {
					roots = append(roots, filepath.Join(appData, v.winAppDataDir, "User", "globalStorage"))
				}
			}
		}
	case "windows":
		appData := strings.TrimSpace(os.Getenv("APPDATA"))
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		for _, v := range knownVariants {
			roots = append(roots, filepath.Join(appData, v.winAppDataDir, "User", "globalStorage"))
		}
	default:
		// Best-effort: try Linux conventions.
		for _, v := range knownVariants {
			roots = append(roots, filepath.Join(home, ".config", v.linuxConfigDir, "User", "globalStorage"))
		}
	}
	return roots
}

// FindTaskDirs walks the known VS Code globalStorage roots and returns the
// list of per-task subdirectories for the given extension subdir
// (e.g. "rooveterinaryinc.roo-cline" or "kilocode.kilo-code").
//
// We return a flat list of absolute task directory paths; the caller
// passes each to ParseTaskDir. Empty extension subdirs and missing root
// dirs are silently skipped so a misconfigured workstation never throws
// on this code path.
func FindTaskDirs(extensionSubdir string) []string {
	extensionSubdir = strings.TrimSpace(extensionSubdir)
	if extensionSubdir == "" {
		return nil
	}

	var tasks []string
	for _, root := range VSCodeGlobalStorageRoots() {
		tasksRoot := filepath.Join(root, extensionSubdir, "tasks")
		entries, err := os.ReadDir(tasksRoot)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			tasks = append(tasks, filepath.Join(tasksRoot, entry.Name()))
		}
	}
	return tasks
}

// FirstExistingTasksRoot returns the absolute path to the first
// `<root>/<extensionSubdir>/tasks` directory we find on disk. This is the
// stable hint a detector should publish; callers should still use
// FindTaskDirs at fetch time to enumerate per-task subdirs (the user may
// have multiple VS Code variants installed simultaneously).
//
// Returns "" when no globalStorage root contains the extension subdir.
func FirstExistingTasksRoot(extensionSubdir string) string {
	extensionSubdir = strings.TrimSpace(extensionSubdir)
	if extensionSubdir == "" {
		return ""
	}
	for _, root := range VSCodeGlobalStorageRoots() {
		tasksRoot := filepath.Join(root, extensionSubdir, "tasks")
		if dirExists(tasksRoot) {
			return tasksRoot
		}
	}
	return ""
}

// AnyExtensionInstalled reports whether at least one VS Code variant
// contains the extension's globalStorage subdir (regardless of whether
// it has any tasks yet). Detectors call this to decide whether to register
// an account.
func AnyExtensionInstalled(extensionSubdir string) bool {
	extensionSubdir = strings.TrimSpace(extensionSubdir)
	if extensionSubdir == "" {
		return false
	}
	for _, root := range VSCodeGlobalStorageRoots() {
		extDir := filepath.Join(root, extensionSubdir)
		if dirExists(extDir) {
			return true
		}
	}
	return false
}

// resolveTaskDirs returns the per-task directories to parse for the given
// account, preferring an explicit per-account override over the
// auto-discovered set.
//
// When the account hint points to a tasks root we walk that root once;
// otherwise we discover across every known VS Code variant.
func resolveTaskDirs(acct core.AccountConfig, extensionSubdir string) []string {
	if override := strings.TrimSpace(acct.Path(PathHintTasksDirKey, "")); override != "" {
		if entries, err := os.ReadDir(override); err == nil {
			var out []string
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				out = append(out, filepath.Join(override, entry.Name()))
			}
			return out
		}
	}
	return FindTaskDirs(extensionSubdir)
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isWindowsSystemUserDir filters out the well-known reserved entries
// under /mnt/c/Users that aren't real user profiles. Cheap heuristic to
// keep the WSL probe quiet.
func isWindowsSystemUserDir(name string) bool {
	switch strings.ToLower(name) {
	case "all users", "default", "default user", "public", "desktop.ini":
		return true
	}
	return false
}
