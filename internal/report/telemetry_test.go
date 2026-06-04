package report

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func i64(v int64) *int64      { return &v }
func f64(v float64) *float64  { return &v }
func tsAt(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }

func TestFromTelemetry_MapsMessageUsage(t *testing.T) {
	events := []shared.TelemetryEvent{
		{
			EventType:  shared.TelemetryEventTypeMessageUsage,
			OccurredAt: tsAt("2026-06-01T10:00:00Z"),
			ProviderID: "codex",
			ModelRaw:   "gpt-5-codex",
			SessionID:  "s1",
			TokenUsage: core.TokenUsage{InputTokens: i64(1000), OutputTokens: i64(200), CostUSD: f64(0.5)},
		},
		// tool_usage event must be ignored
		{EventType: shared.TelemetryEventTypeToolUsage, OccurredAt: tsAt("2026-06-01T10:01:00Z"), ProviderID: "codex"},
		// zero-usage, zero-cost message must be skipped
		{EventType: shared.TelemetryEventTypeMessageUsage, OccurredAt: tsAt("2026-06-01T10:02:00Z"), ProviderID: "codex"},
	}
	got := FromTelemetry(events, "codex", nil)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	e := got[0]
	if e.Provider != "codex" || e.Model != "gpt-5-codex" || e.Session != "s1" {
		t.Errorf("bad mapping: %+v", e)
	}
	if e.Input != 1000 || e.Output != 200 || e.Cost != 0.5 {
		t.Errorf("bad tokens/cost: %+v", e)
	}
}

func TestFromTelemetry_ComputesCostWhenAbsent(t *testing.T) {
	events := []shared.TelemetryEvent{{
		EventType:  shared.TelemetryEventTypeMessageUsage,
		OccurredAt: tsAt("2026-06-01T10:00:00Z"),
		ProviderID: "gemini_cli",
		ModelRaw:   "gemini-2.5-pro",
		TokenUsage: core.TokenUsage{InputTokens: i64(1_000_000)},
	}}
	// inject a deterministic cost func ($2 per 1M input)
	cost := func(model string, in, out, cr, cc, re int) float64 { return float64(in) * 2.0 / 1e6 }
	got := FromTelemetry(events, "gemini_cli", cost)
	if len(got) != 1 || got[0].Cost != 2.0 {
		t.Fatalf("expected computed cost 2.0, got %+v", got)
	}
}

func TestPricingCost_OfflineReturnsZero(t *testing.T) {
	c := PricingCost(true)
	if got := c("gpt-5", 1000, 1000, 0, 0, 0); got != 0 {
		t.Errorf("offline cost = %v, want 0", got)
	}
}
