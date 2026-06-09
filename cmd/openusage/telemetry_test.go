package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTelemetryDaemonRunCommandExposesRunFlags(t *testing.T) {
	cmd := newTelemetryDaemonCommand()
	runCmd, _, err := cmd.Find([]string{"run"})
	if err != nil {
		t.Fatalf("find run command: %v", err)
	}
	if runCmd == nil || runCmd.Use != "run" {
		t.Fatalf("expected run command, got %#v", runCmd)
	}

	for _, name := range []string{
		"db-path",
		"spool-dir",
		"interval",
		"collect-interval",
		"poll-interval",
		"verbose",
	} {
		if runCmd.Flags().Lookup(name) == nil {
			t.Fatalf("run command missing %q flag", name)
		}
	}
}

func TestTelemetryHookAcceptsPositionalPayload(t *testing.T) {
	spoolDir := t.TempDir()
	cmd := newTelemetryHookCommand()
	cmd.SetArgs([]string{
		"--spool-only",
		"--spool-dir", spoolDir,
		"codex",
		`{"type":"agent-turn-complete","turn-id":"t1"}`,
	})
	// No stdin is provided; the positional payload must be used. To prove the
	// stdin path is not taken, point stdin at a closed file (reading it would
	// error). cobra reads os.Stdin inside RunE.
	origStdin := os.Stdin
	r, _, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_ = r.Close() // closed reader: any ReadAll would fail
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	if err := cmd.Execute(); err != nil {
		t.Fatalf("hook with positional payload failed: %v", err)
	}

	// A spool file is written from the positional payload (proving stdin was
	// not consumed, since stdin here is a closed reader).
	entries, err := os.ReadDir(spoolDir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	var written string
	for _, e := range entries {
		if !e.IsDir() {
			written = filepath.Join(spoolDir, e.Name())
			break
		}
	}
	if written == "" {
		t.Fatalf("expected a spooled payload file in %s, found %d entries", spoolDir, len(entries))
	}
	data, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("read spool file: %v", err)
	}
	if !strings.Contains(string(data), "agent-turn-complete") {
		t.Fatalf("spooled payload missing positional content, got: %s", string(data))
	}
}

func TestTelemetryHookRejectsEmptyPositionalAndStdin(t *testing.T) {
	cmd := newTelemetryHookCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--spool-only", "--spool-dir", t.TempDir(), "codex", "   "})

	// Empty stdin so the whitespace-only positional falls through to stdin,
	// which is also empty -> error.
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_ = w.Close() // EOF immediately -> empty stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin; _ = r.Close() }()

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for empty payload, got nil")
	}
}
