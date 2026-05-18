package roocode

import (
	"testing"
	"time"
)

// TestDedup_FoldsDuplicateTaskTimestamp confirms that two APICall entries
// sharing both task ID and timestamp collapse to a single entry, with the
// higher-token row winning.
func TestDedup_FoldsDuplicateTaskTimestamp(t *testing.T) {
	ts := time.Date(2025, 5, 18, 12, 0, 0, 0, time.UTC)
	calls := []APICall{
		{TaskID: "t1", Timestamp: ts, TokensIn: 100, TokensOut: 50, Cost: 0.01},
		{TaskID: "t1", Timestamp: ts, TokensIn: 0, TokensOut: 0, Cost: 0}, // zero-padded loser
		{TaskID: "t2", Timestamp: ts, TokensIn: 5, TokensOut: 1, Cost: 0.001},
	}
	out := Dedup(calls)
	if got, want := len(out), 2; got != want {
		t.Fatalf("len = %d, want %d", got, want)
	}

	byTask := make(map[string]APICall, len(out))
	for _, c := range out {
		byTask[c.TaskID] = c
	}
	if byTask["t1"].TokensIn != 100 {
		t.Errorf("t1 tokens = %d, want 100 (higher-score row should win)", byTask["t1"].TokensIn)
	}
}

// TestDedup_KeepsDistinctTimestamps confirms different timestamps within
// the same task are NOT collapsed.
func TestDedup_KeepsDistinctTimestamps(t *testing.T) {
	calls := []APICall{
		{TaskID: "t1", Timestamp: time.Date(2025, 5, 18, 12, 0, 0, 0, time.UTC), TokensIn: 100},
		{TaskID: "t1", Timestamp: time.Date(2025, 5, 18, 12, 0, 1, 0, time.UTC), TokensIn: 200},
		{TaskID: "t1", Timestamp: time.Date(2025, 5, 18, 12, 0, 2, 0, time.UTC), TokensIn: 300},
	}
	out := Dedup(calls)
	if got, want := len(out), 3; got != want {
		t.Fatalf("len = %d, want %d", got, want)
	}
	// Chronological ordering preserved.
	for i := 0; i < len(out)-1; i++ {
		if !out[i].Timestamp.Before(out[i+1].Timestamp) {
			t.Errorf("out[%d].ts (%v) not before out[%d].ts (%v)", i, out[i].Timestamp, i+1, out[i+1].Timestamp)
		}
	}
}

// TestDedup_UndatedCallsAreUnique asserts that undated calls (zero
// timestamp) are not collapsed into a single bucket — each one is its
// own entry because we can't safely identify duplicates without a key.
func TestDedup_UndatedCallsAreUnique(t *testing.T) {
	calls := []APICall{
		{TaskID: "t1", TokensIn: 1},
		{TaskID: "t1", TokensIn: 2},
		{TaskID: "t1", TokensIn: 3},
	}
	out := Dedup(calls)
	if got, want := len(out), 3; got != want {
		t.Fatalf("len = %d, want %d", got, want)
	}
}

// TestCallsAfter_FiltersByTimestamp confirms the helper drops events at
// or before the cutoff and excludes undated rows.
func TestCallsAfter_FiltersByTimestamp(t *testing.T) {
	cutoff := time.Date(2025, 5, 18, 12, 0, 0, 0, time.UTC)
	calls := []APICall{
		{Timestamp: cutoff.Add(-time.Hour), TokensIn: 1},
		{Timestamp: cutoff, TokensIn: 2}, // strictly after, so this is excluded
		{Timestamp: cutoff.Add(time.Second), TokensIn: 3},
		{}, // undated
	}
	out := CallsAfter(calls, cutoff)
	if got, want := len(out), 1; got != want {
		t.Fatalf("len = %d, want %d", got, want)
	}
	if out[0].TokensIn != 3 {
		t.Errorf("kept call tokens = %d, want 3", out[0].TokensIn)
	}
}

// TestCallsAfter_NoCutoffIsIdentity confirms a zero cutoff returns the
// input slice unchanged.
func TestCallsAfter_NoCutoffIsIdentity(t *testing.T) {
	calls := []APICall{{TokensIn: 1}, {TokensIn: 2}}
	out := CallsAfter(calls, time.Time{})
	if got, want := len(out), 2; got != want {
		t.Errorf("len = %d, want %d", got, want)
	}
}
