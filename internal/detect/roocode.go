package detect

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// rooCodeExtensionSubdir is the VS Code globalStorage subdirectory the Roo
// Code extension writes to. Kept in sync with
// internal/providers/roocode.RooExtensionSubdir; this detector lives
// upstream of providers so we can't import the constant directly.
const rooCodeExtensionSubdir = "rooveterinaryinc.roo-cline"

// detectRooCode registers a local Roo Code account when the extension's
// VS Code globalStorage subdirectory is present in any known VS Code
// variant. We treat "extension dir exists but no tasks yet" as a valid
// detection so users see the tile immediately after install — the
// provider's Fetch handles missing/empty tasks gracefully.
func detectRooCode(result *Result) {
	tasksRoot := firstExistingExtensionTasksRoot(rooCodeExtensionSubdir)
	extensionDir := firstExistingExtensionDir(rooCodeExtensionSubdir)
	if tasksRoot == "" && extensionDir == "" {
		return
	}

	log.Printf("[detect] Found Roo Code extension at %s", firstNonEmpty(extensionDir, tasksRoot))

	if extensionDir != "" {
		result.Tools = append(result.Tools, DetectedTool{
			Name:      "Roo Code",
			ConfigDir: extensionDir,
			Type:      "ide",
		})
	}

	acct := core.AccountConfig{
		ID:           "roocode",
		Provider:     "roocode",
		Auth:         "local",
		RuntimeHints: make(map[string]string),
	}
	if tasksRoot != "" {
		acct.SetPath("tasks_dir", tasksRoot)
		acct.SetHint("tasks_dir", tasksRoot)
	}
	if extensionDir != "" {
		acct.SetHint("extension_dir", extensionDir)
	}
	acct.SetHint("credential_source", "vscode_global_storage")

	addAccount(result, acct)
}

// vscodeGlobalStorageRoots mirrors
// internal/providers/roocode.VSCodeGlobalStorageRoots — kept private to the
// detect package so we don't import upward. Update both when adding new VS
// Code-family installs.
func vscodeGlobalStorageRoots() []string {
	home := homeDir()
	if home == "" {
		return nil
	}

	type variant struct {
		mac, linux, win string
	}
	variants := []variant{
		{mac: "Code", linux: "Code", win: "Code"},
		{mac: "Code - Insiders", linux: "Code - Insiders", win: "Code - Insiders"},
		{mac: "VSCodium", linux: "VSCodium", win: "VSCodium"},
		{mac: "VSCodium - Insiders", linux: "VSCodium - Insiders", win: "VSCodium - Insiders"},
		{mac: "Cursor", linux: "Cursor", win: "Cursor"},
		{mac: "Windsurf", linux: "Windsurf", win: "Windsurf"},
	}

	var roots []string
	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		for _, v := range variants {
			roots = append(roots, filepath.Join(base, v.mac, "User", "globalStorage"))
		}
	case "linux":
		config := filepath.Join(home, ".config")
		if override := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); override != "" {
			config = override
		}
		for _, v := range variants {
			roots = append(roots, filepath.Join(config, v.linux, "User", "globalStorage"))
		}
		// WSL: probe Windows-side AppData. We only emit candidates when the
		// mount exists so we don't pollute the list on pure Linux installs.
		if dirExists("/mnt/c/Users") {
			entries, _ := os.ReadDir("/mnt/c/Users")
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				name := entry.Name()
				switch strings.ToLower(name) {
				case "all users", "default", "default user", "public", "desktop.ini":
					continue
				}
				appData := filepath.Join("/mnt/c/Users", name, "AppData", "Roaming")
				if !dirExists(appData) {
					continue
				}
				for _, v := range variants {
					roots = append(roots, filepath.Join(appData, v.win, "User", "globalStorage"))
				}
			}
		}
	case "windows":
		appData := strings.TrimSpace(os.Getenv("APPDATA"))
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		for _, v := range variants {
			roots = append(roots, filepath.Join(appData, v.win, "User", "globalStorage"))
		}
	default:
		for _, v := range variants {
			roots = append(roots, filepath.Join(home, ".config", v.linux, "User", "globalStorage"))
		}
	}
	return roots
}

// firstExistingExtensionTasksRoot returns the absolute path to the first
// `<globalStorage>/<extensionSubdir>/tasks` directory on disk, or "" if
// none exist.
func firstExistingExtensionTasksRoot(extensionSubdir string) string {
	for _, root := range vscodeGlobalStorageRoots() {
		candidate := filepath.Join(root, extensionSubdir, "tasks")
		if dirExists(candidate) {
			return candidate
		}
	}
	return ""
}

// firstExistingExtensionDir returns the absolute path to the first
// `<globalStorage>/<extensionSubdir>` directory on disk, or "" if none
// exist.
func firstExistingExtensionDir(extensionSubdir string) string {
	for _, root := range vscodeGlobalStorageRoots() {
		candidate := filepath.Join(root, extensionSubdir)
		if dirExists(candidate) {
			return candidate
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
