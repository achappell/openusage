package active

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestBuildFactsUsesExplicitResetKey(t *testing.T) {
	remaining := 37.0
	reset := at("2026-08-15T14:00:00Z")
	snap := core.NewUsageSnapshot("claude_code", "default")
	snap.Metrics["usage_five_hour"] = core.Metric{
		Remaining: &remaining,
		Unit:      "%",
		ResetKey:  "billing_block",
	}
	snap.Resets["billing_block"] = reset

	facts := BuildFacts(snap, time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC))
	if facts.PctRemaining == nil || *facts.PctRemaining != remaining {
		t.Fatalf("PctRemaining = %v, want %.1f", facts.PctRemaining, remaining)
	}
	if facts.ResetAt == nil || !facts.ResetAt.Equal(reset) {
		t.Fatalf("ResetAt = %v, want %v", facts.ResetAt, reset)
	}
}

func TestBuildFactsIgnoresNonQuotaCounters(t *testing.T) {
	used := 1204.0
	snap := core.NewUsageSnapshot("opencode", "default")
	snap.Metrics["weekly_usage"] = core.Metric{Used: &used, Unit: "requests", Window: "7d"}
	snap.Metrics["requests_today"] = core.Metric{Used: &used, Unit: "requests", Window: "1d"}

	facts := BuildFacts(snap, time.Now())
	if facts.PctRemaining != nil {
		t.Fatalf("PctRemaining = %v, want nil", facts.PctRemaining)
	}
	if facts.RequestsToday == nil || *facts.RequestsToday != used {
		t.Fatalf("RequestsToday = %v, want %.0f", facts.RequestsToday, used)
	}
}
