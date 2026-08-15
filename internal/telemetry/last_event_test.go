package telemetry

import (
	"context"
	"testing"
	"time"
)

func TestLastEventTimesGroupsByProviderAccount(t *testing.T) {
	_, db, store := openUsageViewRawTestStore(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx,
		`INSERT INTO usage_raw_events
			(raw_event_id, ingested_at, source_system, source_channel,
			 source_schema_version, source_payload, source_payload_hash)
		 VALUES ('raw-1', '2026-08-15T12:00:00Z', 'test', 'hook', '1', '{}', 'hash-1')`)
	if err != nil {
		t.Fatalf("insert raw: %v", err)
	}

	insert := func(id, provider, account, occurred string) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`INSERT INTO usage_events
				(event_id, occurred_at, provider_id, agent_name, account_id,
				 event_type, status, dedup_key, raw_event_id, normalization_version)
			 VALUES (?, ?, ?, 'test', ?, 'message', 'ok', ?, 'raw-1', '1')`,
			id, occurred, provider, account, id)
		if err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	insert("e1", "codex", "default", "2026-08-15T10:00:00Z")
	insert("e2", "codex", "default", "2026-08-15T12:00:00Z")
	insert("e3", "claude_code", "default", "2026-08-15T11:00:00Z")

	got, err := store.LastEventTimes(ctx)
	if err != nil {
		t.Fatalf("LastEventTimes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d keys, want 2: %v", len(got), got)
	}
	wantCodex := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	if !got["codex:default"].Equal(wantCodex) {
		t.Errorf("codex:default = %v, want %v", got["codex:default"], wantCodex)
	}
	wantClaude := time.Date(2026, 8, 15, 11, 0, 0, 0, time.UTC)
	if !got["claude_code:default"].Equal(wantClaude) {
		t.Errorf("claude_code:default = %v, want %v", got["claude_code:default"], wantClaude)
	}
}

func TestLastEventTimesEmptyStore(t *testing.T) {
	_, _, store := openUsageViewRawTestStore(t)
	got, err := store.LastEventTimes(context.Background())
	if err != nil {
		t.Fatalf("LastEventTimes: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
