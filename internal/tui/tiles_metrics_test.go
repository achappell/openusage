package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestCodexCreditsCompactSummaryFitsNormalTileWidth(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "codex",
		Metrics: map[string]core.Metric{
			"codex_credit_percent_used": {Used: core.Float64Ptr(0.9), Limit: core.Float64Ptr(100), Unit: "%"},
			"codex_credit_limit":        {Used: core.Float64Ptr(71), Limit: core.Float64Ptr(7500), Unit: "credits"},
			"codex_credit_burn_rate":    {Used: core.Float64Ptr(1), Unit: "credits/hour"},
			"codex_credit_runout_hours": {Used: core.Float64Ptr(9.2), Unit: "h"},
			"credit_balance":            {Used: core.Float64Ptr(71), Unit: "credits"},
		},
	}

	const innerW = 90
	lines, _ := buildTileCompactMetricSummaryLines(snap, dashboardWidget("codex"), innerW)
	if len(lines) == 0 {
		t.Fatal("expected Codex compact credit summary")
	}

	line := lines[0]
	if got := lipgloss.Width(line); got > innerW {
		t.Fatalf("credit summary width = %d, want <= %d: %q", got, innerW, line)
	}
	if strings.Contains(line, "…") {
		t.Fatalf("credit summary should fit without truncation: %q", line)
	}
	for _, want := range []string{"used 1%", "total 71/7.5k", "rate 1/h", "runout 9.2h"} {
		if !strings.Contains(line, want) {
			t.Errorf("credit summary missing %q: %q", want, line)
		}
	}
}
