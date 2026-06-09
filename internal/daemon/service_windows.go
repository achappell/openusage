//go:build windows

package daemon

import (
	"fmt"
	"os"
	"os/user"
	"strings"
	"unicode/utf16"
)

// Windows parity for the daemon lifecycle. macOS uses a launchd LaunchAgent and
// Linux a systemd --user unit — both per-user services that auto-start at login
// in the user's session. The faithful Windows analogue is a logon-triggered
// Scheduled Task running in the user context: no Administrator, no Service
// Control Manager handler, no session-0 isolation (which would put state in the
// wrong profile). We drive it with `schtasks` and a generated task definition.

func (m ServiceManager) Install() error {
	if isTransientExecutablePath(m.exePath) {
		return fmt.Errorf(
			"refusing to install telemetry daemon service from transient executable %q (likely from `go run`); build a stable binary first, then run `openusage telemetry daemon install`",
			m.exePath,
		)
	}
	if err := os.MkdirAll(m.stateDir, 0o755); err != nil {
		return fmt.Errorf("create telemetry state dir: %w", err)
	}
	// Capture API keys / settings so the daemon (which a scheduled task can't
	// inject env into) can load them via LoadServiceEnv at startup.
	if err := writeServiceEnvFile(m.EnvFilePath(), currentServiceEnvSnapshot()); err != nil {
		return err
	}
	xml, err := m.taskXML()
	if err != nil {
		return err
	}
	// schtasks /Create /XML requires the definition file to be UTF-16.
	if err := os.WriteFile(m.unitPath, utf16LEBytes(xml), 0o644); err != nil {
		return fmt.Errorf("write scheduled task definition: %w", err)
	}
	if _, err := RunCommand("schtasks", "/Create", "/TN", WindowsScheduledTask, "/XML", m.unitPath, "/F"); err != nil {
		// Keep IsInstalled() honest: drop the marker if registration failed.
		_ = os.Remove(m.unitPath)
		return err
	}
	// Start immediately, matching systemd `enable --now` / launchd RunAtLoad so
	// `daemon install` leaves the daemon running, not just registered for next
	// logon.
	return m.Start()
}

func (m ServiceManager) Uninstall() error {
	// Stop the running instance first (parity with systemd `disable --now` /
	// launchd bootout), then delete the task. Both are best-effort/idempotent.
	_, _ = RunCommand("schtasks", "/End", "/TN", WindowsScheduledTask)
	_, _ = RunCommand("schtasks", "/Delete", "/TN", WindowsScheduledTask, "/F")
	if err := os.Remove(m.unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove scheduled task definition: %w", err)
	}
	return nil
}

func (m ServiceManager) Start() error {
	_, err := RunCommand("schtasks", "/Run", "/TN", WindowsScheduledTask)
	return err
}

// taskXML builds a Task Scheduler 1.2 definition: a logon trigger for the
// current user, least-privilege interactive token (no stored password), restart
// on failure, and a cmd.exe action that runs the daemon with stdout/stderr
// redirected to the same log files macOS/Linux use, so StartupDiagnostics can
// tail them identically.
func (m ServiceManager) taskXML() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("resolve current user: %w", err)
	}
	userID := xmlEscape(u.Username)
	// Run the binary directly (not via cmd.exe). schtasks /End terminates the
	// task's own process, so a wrapper shell would be killed while orphaning the
	// daemon child — running openusage directly lets uninstall actually stop it.
	args := fmt.Sprintf(`telemetry daemon run --socket-path "%s"`, m.socketPath)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>OpenUsage Telemetry Daemon</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%s</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>%s</Arguments>
    </Exec>
  </Actions>
</Task>`, userID, userID, xmlEscape(m.exePath), xmlEscape(args)), nil
}

// utf16LEBytes encodes s as little-endian UTF-16 with a BOM, the encoding
// `schtasks /Create /XML` requires for its task-definition file.
func utf16LEBytes(s string) []byte {
	codes := utf16.Encode([]rune(s))
	out := make([]byte, 0, len(codes)*2+2)
	out = append(out, 0xFF, 0xFE) // UTF-16 LE BOM
	for _, c := range codes {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

// xmlEscape escapes the minimal set of characters that are unsafe in XML text
// content. `&` must be first.
func xmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	).Replace(s)
}
