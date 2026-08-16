package sketchybar

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
}

func TestBuildSnippetUsesNeutralAssetDirectory(t *testing.T) {
	snippet, err := BuildSnippet(InstallOptions{
		Preset:  DefaultPreset,
		Binary:  "/Applications/Open Usage/openusage",
		DataDir: "/tmp/openusage sketchybar",
	})
	if err != nil {
		t.Fatalf("BuildSnippet: %v", err)
	}
	for _, want := range []string{
		SentinelStart,
		SentinelEnd,
		"OPENUSAGE_SKETCHYBAR_DIR='/tmp/openusage sketchybar'",
		"ai-usage.sh",
		"provider-select.sh",
	} {
		if !strings.Contains(snippet, want) {
			t.Fatalf("snippet missing %q:\n%s", want, snippet)
		}
	}
	if strings.Contains(snippet, ".config/sketchybar/plugins") && !strings.Contains(snippet, "outside ~/.config/sketchybar/plugins") {
		t.Fatalf("snippet points at the user's plugins directory:\n%s", snippet)
	}
	if strings.Contains(snippet, "\n+") {
		t.Fatalf("snippet contains an accidental generated '+' line:\n%s", snippet)
	}

	path := filepath.Join(t.TempDir(), "snippet.sh")
	if err := os.WriteFile(path, []byte(snippet), 0o600); err != nil {
		t.Fatalf("write snippet: %v", err)
	}
	if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("bash -n snippet: %v\n%s", err, out)
	}
}

func TestInstallWritesAssetsAndSentinel(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	configPath := filepath.Join(home, ".config", "sketchybar", "sketchybarrc")
	dataDir := filepath.Join(home, ".local", "share", "openusage", "sketchybar")
	pluginsDir := filepath.Join(home, ".config", "sketchybar", "plugins")

	var out bytes.Buffer
	path, err := Install(&out, InstallOptions{Write: true, ConfigPath: configPath, DataDir: dataDir})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if path != configPath {
		t.Fatalf("path = %q, want %q", path, configPath)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !bytes.Contains(config, []byte(SentinelStart)) || !bytes.Contains(config, []byte(SentinelEnd)) {
		t.Fatalf("config missing managed block:\n%s", config)
	}
	if _, err := os.Stat(pluginsDir); !os.IsNotExist(err) {
		t.Fatalf("installer touched plugins directory: err=%v", err)
	}
	for _, name := range assetNames {
		path := filepath.Join(dataDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("asset %s: %v", name, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("asset %s is not executable: mode=%o", name, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read asset %s: %v", name, err)
		}
		if strings.Contains(strings.ToLower(string(data)), "python") {
			t.Fatalf("asset %s reintroduced a Python dependency", name)
		}
		if output, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
			t.Fatalf("bash -n %s: %v\n%s", name, err, output)
		}
	}
}

func TestInstallReplacesBlockAndUninstallPreservesUserConfig(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	configPath := filepath.Join(home, "sketchybarrc")
	if err := os.WriteFile(configPath, []byte("# before\n"+SentinelStart+"\nsketchybar --update\n"+SentinelEnd+"\n# after\n"), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	var out bytes.Buffer
	if _, err := Install(&out, InstallOptions{Write: true, ConfigPath: configPath, DataDir: filepath.Join(home, "scripts")}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read replacement: %v", err)
	}
	if got := strings.Count(string(data), SentinelStart); got != 1 {
		t.Fatalf("sentinel count = %d, want 1:\n%s", got, data)
	}
	if !bytes.Contains(data, []byte("# before")) || !bytes.Contains(data, []byte("# after")) {
		t.Fatalf("replacement clobbered user config:\n%s", data)
	}
	if _, err := os.Stat(configPath + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}

	if err := Uninstall(&out, configPath); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	data, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read uninstall: %v", err)
	}
	if bytes.Contains(data, []byte(SentinelStart)) || !bytes.Contains(data, []byte("# before")) || !bytes.Contains(data, []byte("# after")) {
		t.Fatalf("uninstall damaged config:\n%s", data)
	}
}

func TestInstallFollowsSymlinkedConfig(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	target := filepath.Join(home, "dotfiles", "sketchybarrc")
	link := filepath.Join(home, ".config", "sketchybar", "sketchybarrc")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir link: %v", err)
	}
	if err := os.WriteFile(target, []byte("# dotfiles config\n"), 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	var out bytes.Buffer
	if _, err := Install(&out, InstallOptions{Write: true, ConfigPath: link, DataDir: filepath.Join(home, "scripts")}); err != nil {
		t.Fatalf("Install through symlink: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("installer replaced symlink with regular file: mode=%v", info.Mode())
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !bytes.Contains(data, []byte(SentinelStart)) {
		t.Fatalf("target missing managed block:\n%s", data)
	}
}

func TestDoctorReportsIntegrationState(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	configPath := filepath.Join(home, "sketchybarrc")
	dataDir := filepath.Join(home, "scripts")
	var out bytes.Buffer
	if err := Doctor(&out, DoctorOptions{ConfigPath: configPath, DataDir: dataDir, Binary: filepath.Join(home, "missing-openusage"), Sketchybar: filepath.Join(home, "missing-sketchybar")}); err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if !strings.Contains(out.String(), "no openusage block") || !strings.Contains(out.String(), "generated script: missing") {
		t.Fatalf("doctor output missing checks:\n%s", out.String())
	}
}
