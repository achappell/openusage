package report

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFromSnapshots_DailySeries(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "codex",
		Timestamp:  time.Now(),
		DailySeries: map[string][]core.TimePoint{
			"cost_usd":     {{Date: "2026-06-01", Value: 1.5}, {Date: "2026-06-02", Value: 2.5}},
			"tokens_total": {{Date: "2026-06-01", Value: 1000}, {Date: "2026-06-02", Value: 2000}},
		},
	}
	events := FromSnapshots([]core.UsageSnapshot{snap})
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	for _, e := range events {
		if !e.Synthetic {
			t.Errorf("snapshot event should be synthetic: %+v", e)
		}
		if e.Provider != "codex" {
			t.Errorf("provider = %q, want codex", e.Provider)
		}
	}
	// Aggregating these into a daily report should preserve the per-day cost.
	rep := Build(events, Options{Kind: KindDaily})
	if rep.Totals.Cost != 4.0 {
		t.Errorf("total cost = %v, want 4.0", rep.Totals.Cost)
	}
}

func TestFromSnapshots_FallbackToTotal(t *testing.T) {
	total := 12.0
	snap := core.UsageSnapshot{
		ProviderID: "openrouter",
		Timestamp:  time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		Metrics: map[string]core.Metric{
			"total_cost_usd": {Used: &total, Unit: "USD"},
		},
	}
	events := FromSnapshots([]core.UsageSnapshot{snap})
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 fallback event", len(events))
	}
	if events[0].Cost != 12.0 || !events[0].Synthetic {
		t.Errorf("fallback event = %+v, want cost 12 synthetic", events[0])
	}
}

func TestFromSnapshots_NoCostNoEvent(t *testing.T) {
	snap := core.UsageSnapshot{ProviderID: "anthropic", Timestamp: time.Now()}
	if events := FromSnapshots([]core.UsageSnapshot{snap}); len(events) != 0 {
		t.Errorf("got %d events for cost-less snapshot, want 0", len(events))
	}
}

func TestFromSnapshots_RecognizesAlternateCostKey(t *testing.T) {
	// cursor uses "analytics_cost" for its daily series.
	snap := core.UsageSnapshot{
		ProviderID: "cursor",
		DailySeries: map[string][]core.TimePoint{
			"analytics_cost": {{Date: "2026-06-01", Value: 3.0}},
		},
	}
	events := FromSnapshots([]core.UsageSnapshot{snap})
	if len(events) != 1 || events[0].Cost != 3.0 {
		t.Fatalf("expected 1 event cost 3.0 from analytics_cost, got %+v", events)
	}
}

func TestFromSnapshots_TokenOnlySeriesAppears(t *testing.T) {
	// A token-only provider (no cost series) should still surface with token
	// columns from its tokens_total series.
	snap := core.UsageSnapshot{
		ProviderID: "qwen_cli",
		DailySeries: map[string][]core.TimePoint{
			"tokens_total": {{Date: "2026-06-01", Value: 1500}},
		},
	}
	events := FromSnapshots([]core.UsageSnapshot{snap})
	if len(events) != 1 || events[0].Input != 1500 || events[0].Cost != 0 {
		t.Fatalf("expected 1 token-only event (1500 tokens, $0), got %+v", events)
	}
}
