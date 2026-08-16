package sketchybar

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Doctor prints independent checks and continues after a failed check so the
// user gets one useful report instead of a scavenger hunt.
func Doctor(out io.Writer, opts DoctorOptions) error {
	configPath, err := configPathOrDefault(opts.ConfigPath, out)
	if err != nil {
		fmt.Fprintf(out, "[FAIL] sketchybarrc: %v\n", err)
	} else {
		present, presentErr := SentinelPresent(configPath)
		if presentErr != nil {
			fmt.Fprintf(out, "[FAIL] sketchybarrc: %v\n", presentErr)
		} else if present {
			fmt.Fprintf(out, "[ OK ] sketchybarrc: openusage block present at %s\n", configPath)
		} else {
			fmt.Fprintf(out, "[INFO] sketchybarrc: no openusage block at %s (run `openusage sketchybar install --write`)\n", configPath)
		}
	}

	dataDir := strings.TrimSpace(opts.DataDir)
	if dataDir == "" {
		dataDir, err = DefaultDataDir()
	} else {
		dataDir, err = expandPath(dataDir)
	}
	if err != nil {
		fmt.Fprintf(out, "[FAIL] generated scripts: %v\n", err)
	} else {
		for _, name := range assetNames {
			path := filepath.Join(dataDir, name)
			info, statErr := os.Stat(path)
			switch {
			case statErr != nil && os.IsNotExist(statErr):
				fmt.Fprintf(out, "[INFO] generated script: missing %s\n", path)
			case statErr != nil:
				fmt.Fprintf(out, "[FAIL] generated script: %s: %v\n", path, statErr)
			case info.Mode()&0o111 == 0:
				fmt.Fprintf(out, "[WARN] generated script: not executable %s\n", path)
			default:
				fmt.Fprintf(out, "[ OK ] generated script: %s\n", path)
			}
		}
	}

	checkBinary(out, "openusage", opts.Binary)
	checkBinary(out, "sketchybar", opts.Sketchybar)
	fmt.Fprintln(out, "done.")
	return nil
}

func checkBinary(out io.Writer, name, override string) {
	path := strings.TrimSpace(override)
	if path != "" {
		if expanded, err := expandPath(path); err == nil {
			path = expanded
		}
	}
	if path == "" {
		found, err := exec.LookPath(name)
		if err != nil {
			fmt.Fprintf(out, "[INFO] %s: not found in PATH\n", name)
			return
		}
		path = found
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(out, "[FAIL] %s: %v\n", name, err)
		return
	}
	fmt.Fprintf(out, "[ OK ] %s: %s\n", name, path)
}
