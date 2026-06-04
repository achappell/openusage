package report

import (
	"strings"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func TestBuildBlocks_SplitsOnGapAndDuration(t *testing.T) {
	events := []Event{
		{Time: at("2026-06-01T10:00:00Z"), Provider: "claude_code", Model: "opus", Cost: 1, Input: 100},
		{Time: at("2026-06-01T11:30:00Z"), Provider: "claude_code", Model: "opus", Cost: 1, Input: 100}, // same block (<5h)
		// > 5h gap from previous entry -> new block
		{Time: at("2026-06-01T20:00:00Z"), Provider: "claude_code", Model: "opus", Cost: 1, Input: 100},
	}
	rep := Build(events, Options{Kind: KindBlocks, Now: at("2026-06-02T00:00:00Z")})
	if len(rep.Rows) != 2 {
		t.Fatalf("got %d blocks, want 2", len(rep.Rows))
	}
	if rep.Rows[0].Start != at("2026-06-01T10:00:00Z") {
		t.Errorf("block1 start = %v, want floored to 10:00", rep.Rows[0].Start)
	}
	if rep.Totals.Cost != 3 {
		t.Errorf("totals cost = %v, want 3", rep.Totals.Cost)
	}
}

func TestBuildBlocks_ActiveBlockBurnAndProjection(t *testing.T) {
	// Block starts 12:00; two entries an hour apart; "now" is 13:00 (1h elapsed,
	// 4h remaining in the 5h window).
	events := []Event{
		{Time: at("2026-06-01T12:00:00Z"), Provider: "claude_code", Model: "opus", Cost: 10, Input: 100},
		{Time: at("2026-06-01T13:00:00Z"), Provider: "claude_code", Model: "opus", Cost: 10, Input: 100},
	}
	now := at("2026-06-01T13:00:00Z")
	rep := Build(events, Options{Kind: KindBlocks, Now: now})

	active, ok := rep.ActiveBlock()
	if !ok {
		t.Fatal("expected an active block")
	}
	if active.Cost != 20 {
		t.Errorf("active cost = %v, want 20", active.Cost)
	}
	// elapsed (first->last) = 60 min -> burn = 20/60*60 = $20/h
	if active.BurnRateUSDPerHour != 20 {
		t.Errorf("burn = %v, want 20", active.BurnRateUSDPerHour)
	}
	// remaining 4h -> projected = 20 + 20*4 = 100
	if active.ProjectedCost != 100 {
		t.Errorf("projected = %v, want 100", active.ProjectedCost)
	}
	if active.TimeRemaining != 4*time.Hour {
		t.Errorf("time remaining = %v, want 4h", active.TimeRemaining)
	}
}

func TestBuildBlocks_ExcludesSyntheticWithNote(t *testing.T) {
	e := Event{Time: at("2026-06-01T10:00:00Z"), Provider: "openrouter", Model: "(total)", Cost: 5, Synthetic: true}
	rep := Build([]Event{e}, Options{Kind: KindBlocks, Now: at("2026-06-01T12:00:00Z")})
	if len(rep.Rows) != 0 {
		t.Fatalf("synthetic events produced %d blocks, want 0", len(rep.Rows))
	}
	if !strings.Contains(rep.Note, "Claude Code") {
		t.Errorf("expected a note about Claude Code logs, got %q", rep.Note)
	}
}

func TestBuildBlocks_PastBlockNotActive(t *testing.T) {
	events := []Event{
		{Time: at("2026-06-01T10:00:00Z"), Provider: "claude_code", Model: "opus", Cost: 1, Input: 1},
	}
	rep := Build(events, Options{Kind: KindBlocks, Now: at("2026-06-02T10:00:00Z")})
	if _, ok := rep.ActiveBlock(); ok {
		t.Error("block from yesterday should not be active")
	}
}
