package report

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFromItemized_UsesRecordedCost(t *testing.T) {
	evs := []core.UsageEvent{{
		Time: at("2026-06-01T10:00:00Z"), ProviderID: "amp", Model: "claude", Session: "t1",
		InputTokens: 100, OutputTokens: 10, CostUSD: 1.25, HasCost: true,
	}}
	got := FromItemized(evs, func(string, int, int, int, int, int) float64 { return 99 })
	if len(got) != 1 || got[0].Cost != 1.25 {
		t.Fatalf("expected recorded cost 1.25, got %+v", got)
	}
}

func TestFromItemized_ComputesCostWhenAbsent(t *testing.T) {
	evs := []core.UsageEvent{{
		Time: at("2026-06-01T10:00:00Z"), ProviderID: "zed", Model: "gpt", Session: "t1",
		InputTokens: 1000, HasCost: false,
	}}
	got := FromItemized(evs, func(_ string, in, _, _, _, _ int) float64 { return float64(in) / 1000 })
	if len(got) != 1 || got[0].Cost != 1.0 {
		t.Fatalf("expected computed cost 1.0, got %+v", got)
	}
}

func TestFromItemized_SkipsEmpty(t *testing.T) {
	evs := []core.UsageEvent{{ProviderID: "x", HasCost: false}}
	if got := FromItemized(evs, nil); len(got) != 0 {
		t.Fatalf("expected empty event skipped, got %+v", got)
	}
}
