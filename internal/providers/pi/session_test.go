package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPiSessionFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	body := `{"type":"session","id":"pi_ses_001","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/home/jane/work/project-x"}
{"type":"message","id":"msg_001","parentId":null,"timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","model":"claude-3-5-sonnet","provider":"anthropic","usage":{"input":100,"output":50,"cacheRead":10,"cacheWrite":5,"totalTokens":165}}}
{"type":"message","id":"msg_002","timestamp":"2026-01-01T00:00:02.000Z","message":{"role":"user","model":"claude-3-5-sonnet","provider":"anthropic"}}
{"type":"message","id":"msg_003","timestamp":"2026-01-01T00:00:03.000Z","message":{"role":"assistant","model":"claude-3-5-sonnet","provider":"anthropic"}}
this is not json at all, skip me
{"type":"message","id":"msg_004","timestamp":"2026-01-01T00:00:04.000Z","message":{"role":"assistant","model":"gpt-4o","provider":"openai","usage":{"input":200,"output":80}}}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	entries, meta, err := readPiSessionFile(path)
	if err != nil {
		t.Fatalf("readPiSessionFile: %v", err)
	}
	if meta.SessionID != "pi_ses_001" {
		t.Errorf("session id = %q, want pi_ses_001", meta.SessionID)
	}
	if meta.WorkspaceLabel != "project-x" {
		t.Errorf("workspace label = %q, want project-x", meta.WorkspaceLabel)
	}
	if meta.HeaderTime.IsZero() {
		t.Error("header time not parsed")
	}

	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (one assistant w/ usage, one gpt assistant w/ usage)", len(entries))
	}

	first := entries[0]
	if first.Model != "claude-3-5-sonnet" || first.Provider != "anthropic" {
		t.Errorf("first entry model/provider = %q/%q", first.Model, first.Provider)
	}
	if first.Input != 100 || first.Output != 50 || first.CacheRead != 10 || first.CacheWrite != 5 {
		t.Errorf("first entry tokens unexpected: %+v", first)
	}
	if first.SessionID != "pi_ses_001" {
		t.Errorf("first entry session id = %q", first.SessionID)
	}
	if first.WorkspaceLabel != "project-x" {
		t.Errorf("first entry workspace = %q", first.WorkspaceLabel)
	}

	second := entries[1]
	if second.Model != "gpt-4o" || second.Provider != "openai" {
		t.Errorf("second model/provider = %q/%q", second.Model, second.Provider)
	}
	if second.Input != 200 || second.Output != 80 || second.CacheRead != 0 || second.CacheWrite != 0 {
		t.Errorf("second tokens unexpected: %+v", second)
	}
}

func TestReadPiSessionFile_InvalidHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	body := `{"type":"message","id":"msg_001","message":{"role":"assistant","model":"x","provider":"y","usage":{"input":1}}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, meta, err := readPiSessionFile(path)
	if err != nil {
		t.Fatalf("readPiSessionFile: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected zero entries from header-less file, got %d", len(entries))
	}
	if meta.SessionID != "" {
		t.Errorf("expected empty meta, got %+v", meta)
	}
}

func TestReadPiSessionFile_GarbageHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	body := "not json\n" + `{"type":"message","message":{"role":"assistant","usage":{"input":1}}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, _, err := readPiSessionFile(path)
	if err != nil {
		t.Fatalf("readPiSessionFile: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected zero entries from garbage-header file, got %d", len(entries))
	}
}

func TestReadPiSessionFile_FallbackToMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	body := `{"type":"session","id":"pi_ses_002","cwd":"/x"}
{"type":"message","id":"msg_001","message":{"role":"assistant","model":"m","provider":"p","usage":{"input":10,"output":20}}}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, _, err := readPiSessionFile(path)
	if err != nil {
		t.Fatalf("readPiSessionFile: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Timestamp.IsZero() {
		t.Error("expected mtime fallback timestamp, got zero")
	}
}

func TestReadPiSessionFile_AllZeroTokensFiltered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	body := `{"type":"session","id":"pi_ses_003","cwd":"/x"}
{"type":"message","message":{"role":"assistant","model":"m","provider":"p","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0}}}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, _, err := readPiSessionFile(path)
	if err != nil {
		t.Fatalf("readPiSessionFile: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected zero entries for all-zero usage, got %d", len(entries))
	}
}

func TestWorkspaceLabel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"/", ""},
		{"/home/jane/work/project-x", "project-x"},
		{"/home/jane/work/project-x/", "project-x"},
		{"project-x", "project-x"},
		{`C:\Users\jane\proj`, `C:\Users\jane\proj`},
	}
	for _, tc := range cases {
		got := workspaceLabel(tc.in)
		if got != tc.want {
			t.Errorf("workspaceLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReadPiSessionFile_LongLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	big := strings.Repeat("x", 200_000)
	body := `{"type":"session","id":"pi_ses_004","cwd":"/x"}` + "\n" +
		`{"type":"message","message":{"role":"assistant","model":"` + big + `","provider":"p","usage":{"input":1}}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, _, err := readPiSessionFile(path)
	if err != nil {
		t.Fatalf("readPiSessionFile: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}
