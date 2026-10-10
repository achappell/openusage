package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestReadSessionUsageBreakdowns_ThreadSettingsApplied(t *testing.T) {
	tmp := t.TempDir()
	sessDir := filepath.Join(tmp, "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessDir, "2026-09-12-test.jsonl")
	content := "{\"timestamp\":\"2026-09-12T14:00:00.000Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"test\",\"session_id\":\"test\",\"originator\":\"Codex Desktop\",\"source\":\"vscode\",\"model_provider\":\"openai\",\"base_instructions\":{\"provenance\":{\"type\":\"custom\"}}}}\n" +
		"{\"timestamp\":\"2026-09-12T14:00:01.000Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"thread_settings_applied\",\"thread_id\":\"t\",\"thread_settings\":{\"model\":\"gpt-6-astra\",\"model_provider_id\":\"openai\"}}}\n" +
		"{\"timestamp\":\"2026-09-12T14:00:02.000Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":1000000,\"cached_input_tokens\":0,\"output_tokens\":500000,\"reasoning_output_tokens\":0,\"total_tokens\":1500000},\"last_token_usage\":{\"input_tokens\":1000000,\"output_tokens\":500000,\"total_tokens\":1500000},\"model_context_window\":128000}}}\n" +
		"{\"timestamp\":\"2026-09-12T14:00:03.000Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":2000000,\"cached_input_tokens\":0,\"output_tokens\":1000000,\"reasoning_output_tokens\":0,\"total_tokens\":3000000},\"last_token_usage\":{\"input_tokens\":1000000,\"output_tokens\":500000,\"total_tokens\":1500000},\"model_context_window\":128000}}}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	p := New()
	acct := core.AccountConfig{ID: "test", Provider: "codex", RuntimeHints: map[string]string{"sessions_dir": sessDir, "config_dir": tmp}}
	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if m, ok := snap.Metrics["model_gpt_6_astra_total_tokens"]; !ok || m.Used == nil || *m.Used != 3000000 {
		t.Fatalf("model_gpt_6_astra_total_tokens = %#v, want 3000000", m)
	}
	if _, ok := snap.Metrics["model_unknown_total_tokens"]; ok {
		t.Fatalf("should not have unknown, got %v", snap.Metrics["model_unknown_total_tokens"])
	}
}

func TestReadSessionUsageBreakdowns_EarlyUnknownFlushedToFirstModel(t *testing.T) {
	tmp := t.TempDir()
	sessDir := filepath.Join(tmp, "sessions")
	if err := os.MkdirAll(sessDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessDir, "2026-08-29-test.jsonl")
	// token_count at ordinal 5 before turn_context at 8, like the real
	// 2026/08/29 10.8M unknown file (codex-auto-review).
	content := "{\"timestamp\":\"2026-08-29T23:15:44.000Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"test\",\"session_id\":\"test\",\"originator\":\"Codex Desktop\",\"source\":\"vscode\",\"model_provider\":\"openai\",\"base_instructions\":{\"provenance\":{\"type\":\"custom\"}}}}\n" +
		"{\"timestamp\":\"2026-08-29T23:15:45.000Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":1000000,\"cached_input_tokens\":0,\"output_tokens\":100000,\"reasoning_output_tokens\":0,\"total_tokens\":1100000},\"last_token_usage\":{\"input_tokens\":1000000,\"output_tokens\":100000,\"total_tokens\":1100000},\"model_context_window\":258400}}}\n" +
		"{\"timestamp\":\"2026-08-29T23:15:46.000Z\",\"type\":\"turn_context\",\"payload\":{\"turn_id\":\"t\",\"model\":\"codex-auto-review\"}}\n" +
		"{\"timestamp\":\"2026-08-29T23:15:47.000Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":2000000,\"cached_input_tokens\":0,\"output_tokens\":200000,\"reasoning_output_tokens\":0,\"total_tokens\":2200000},\"last_token_usage\":{\"input_tokens\":1000000,\"output_tokens\":100000,\"total_tokens\":1100000},\"model_context_window\":258400}}}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	p := New()
	acct := core.AccountConfig{ID: "test", Provider: "codex", RuntimeHints: map[string]string{"sessions_dir": sessDir, "config_dir": tmp}}
	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if m, ok := snap.Metrics["model_codex_auto_review_total_tokens"]; !ok || m.Used == nil || *m.Used != 2200000 {
		t.Fatalf("model_codex_auto_review_total_tokens = %#v, want 2200000 (both deltas flushed)", m)
	}
	if _, ok := snap.Metrics["model_unknown_total_tokens"]; ok {
		t.Fatalf("should not have unknown after flush, got %v", snap.Metrics["model_unknown_total_tokens"])
	}
}
