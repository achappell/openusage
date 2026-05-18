package claude_code

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAgentType_MainSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session-1.jsonl")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := detectAgentType(path); got != "main" {
		t.Fatalf("expected main, got %q", got)
	}
}

func TestDetectAgentType_MetaSidecar(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "session-A", "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	jsonlPath := filepath.Join(dir, "agent-xyz.jsonl")
	if err := os.WriteFile(jsonlPath, []byte{}, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	metaPath := filepath.Join(dir, "agent-xyz.meta.json")
	if err := os.WriteFile(metaPath, []byte(`{"agentType":"code-reviewer"}`), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	if got := detectAgentType(jsonlPath); got != "code-reviewer" {
		t.Fatalf("expected code-reviewer, got %q", got)
	}
}

func TestDetectAgentType_ParentToolUseCrossJoin(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "session-B")
	subagentDir := filepath.Join(sessionDir, "subagents")
	if err := os.MkdirAll(subagentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Parent transcript carries a tool_use that references the subagent id.
	parentPath := filepath.Join(root, "session-B.jsonl")
	parentLine := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Task","input":{"subagent_type":"test-runner","description":"agentId: abc123"}}]}}`
	if err := os.WriteFile(parentPath, []byte(parentLine+"\n"), 0o644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	jsonlPath := filepath.Join(subagentDir, "agent-abc123.jsonl")
	if err := os.WriteFile(jsonlPath, []byte{}, 0o644); err != nil {
		t.Fatalf("write subagent: %v", err)
	}
	if got := detectAgentType(jsonlPath); got != "test-runner" {
		t.Fatalf("expected test-runner, got %q", got)
	}
}

func TestDetectAgentType_GenericFallback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "session-C", "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	jsonlPath := filepath.Join(dir, "agent-unknown.jsonl")
	if err := os.WriteFile(jsonlPath, []byte{}, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := detectAgentType(jsonlPath); got != "agent" {
		t.Fatalf("expected generic agent label, got %q", got)
	}
}

func TestMergeStreamingDuplicates_MaxPerField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	// Two streaming retransmits of the same logical message: same messageId
	// and requestId, but the second carries the larger totals. The merged
	// record must keep the per-field MAX in every column.
	lines := `{"type":"assistant","timestamp":"2026-05-18T10:00:00Z","sessionId":"s1","requestId":"r1","message":{"id":"m1","model":"claude-sonnet-4","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":10,"cache_creation_input_tokens":5,"reasoning_tokens":2}}}
{"type":"assistant","timestamp":"2026-05-18T10:00:05Z","sessionId":"s1","requestId":"r1","message":{"id":"m1","model":"claude-sonnet-4","usage":{"input_tokens":200,"output_tokens":40,"cache_read_input_tokens":30,"cache_creation_input_tokens":4,"reasoning_tokens":1}}}
`
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	records := parseConversationRecords(path)
	if len(records) != 1 {
		t.Fatalf("expected 1 merged record, got %d", len(records))
	}
	got := records[0].usage
	if got.InputTokens != 200 {
		t.Errorf("InputTokens: got %d, want 200", got.InputTokens)
	}
	if got.OutputTokens != 50 {
		t.Errorf("OutputTokens: got %d, want 50", got.OutputTokens)
	}
	if got.CacheReadInputTokens != 30 {
		t.Errorf("CacheReadInputTokens: got %d, want 30", got.CacheReadInputTokens)
	}
	if got.CacheCreationInputTokens != 5 {
		t.Errorf("CacheCreationInputTokens: got %d, want 5", got.CacheCreationInputTokens)
	}
	if got.ReasoningTokens != 2 {
		t.Errorf("ReasoningTokens: got %d, want 2", got.ReasoningTokens)
	}
}
