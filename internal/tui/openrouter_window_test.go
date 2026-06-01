package tui

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/openrouter"
	"github.com/janekbaraniewski/openusage/internal/providers/zai"
)

// TestDetailedCredits_OpenRouterScopesLifetimeSpend covers issue #175: the
// cumulative credit-balance headline must be tagged "all-time" so it is not
// read as spend within the dashboard's selected time window, and the detail
// line shows exactly one windowed figure (the authoritative window_credit_spend
// the daemon computes for the active window), not a 1d/7d/30d dump.
func TestDetailedCredits_OpenRouterScopesLifetimeSpend(t *testing.T) {
	totalCredits := 80.00
	totalUsage := 60.63
	remaining := 19.37
	zero := 0.0

	snap := core.UsageSnapshot{
		ProviderID: "openrouter",
		AccountID:  "openrouter",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"credit_balance": {
				Limit: &totalCredits, Used: &totalUsage, Remaining: &remaining,
				Unit: "USD", Window: "lifetime",
			},
			// The read model collapses the active window to one metric.
			"window_credit_spend": {Used: &zero, Unit: "USD", Window: "30d"},
		},
	}

	got := computeDisplayInfo(snap, openrouter.New().DashboardWidget(), false)

	if !strings.Contains(got.summary, "all-time") {
		t.Errorf("summary must tag the cumulative figure as all-time, got %q", got.summary)
	}
	if !strings.Contains(got.summary, "60.63") || !strings.Contains(got.summary, "80.00") {
		t.Errorf("summary should still show lifetime spent/total, got %q", got.summary)
	}
	// Exactly one windowed figure for the active window, plus remaining.
	for _, want := range []string{"30d $0.00", "$19.37 left"} {
		if !strings.Contains(got.detail, want) {
			t.Errorf("detail missing %q; got %q", want, got.detail)
		}
	}
	for _, unwanted := range []string{"1d $", "7d $"} {
		if strings.Contains(got.detail, unwanted) {
			t.Errorf("detail should show only the active window, got %q", got.detail)
		}
	}
	// The bare, untagged "spent" headline that caused the confusion must be gone.
	if strings.Contains(got.summary, "spent") && !strings.Contains(got.summary, "·") {
		t.Errorf("headline still untagged: %q", got.summary)
	}
}

// TestDetailedCredits_ZaiUsesCurrentScope verifies the same code path tags a
// Z.AI balance (Window "current") as "current", not "all-time", so the shared
// DetailedCredits renderer stays correct for both providers.
func TestDetailedCredits_ZaiUsesCurrentScope(t *testing.T) {
	limit := 10.00
	used := 2.96
	remaining := 7.04

	snap := core.UsageSnapshot{
		ProviderID: "zai",
		AccountID:  "zai",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"credit_balance": {
				Limit: &limit, Used: &used, Remaining: &remaining,
				Unit: "USD", Window: "current",
			},
		},
	}

	got := computeDisplayInfo(snap, zai.New().DashboardWidget(), false)
	if !strings.Contains(got.summary, "current") {
		t.Errorf("zai headline should be tagged current, got %q", got.summary)
	}
	if strings.Contains(got.summary, "all-time") {
		t.Errorf("zai headline must not be tagged all-time, got %q", got.summary)
	}
}

// TestDetailedCredits_WindowCreditSpendLeadsDetail verifies that the
// authoritative window_credit_spend metric becomes the primary windowed figure
// on the detail line, tracking the metric's own window label ("30d $0.00"),
// and that its window tag is not duplicated by the static per-window breakdown.
func TestDetailedCredits_WindowCreditSpendLeadsDetail(t *testing.T) {
	totalCredits := 80.00
	totalUsage := 60.63
	remaining := 19.37
	zero := 0.0

	snap := core.UsageSnapshot{
		ProviderID: "openrouter",
		AccountID:  "openrouter",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"credit_balance": {
				Limit: &totalCredits, Used: &totalUsage, Remaining: &remaining,
				Unit: "USD", Window: "lifetime",
			},
			"window_credit_spend": {Used: &zero, Unit: "USD", Window: "30d"},
			"today_cost":          {Used: &zero, Unit: "USD", Window: "today"},
			"7d_api_cost":         {Used: &zero, Unit: "USD", Window: "7d"},
			"30d_api_cost":        {Used: &zero, Unit: "USD", Window: "30d"},
		},
	}

	got := computeDisplayInfo(snap, openrouter.New().DashboardWidget(), false)

	if !strings.Contains(got.detail, "30d $0.00") {
		t.Errorf("detail should show the windowed figure 30d $0.00, got %q", got.detail)
	}
	if !strings.Contains(got.detail, "$19.37 left") {
		t.Errorf("detail should keep the remaining-balance part, got %q", got.detail)
	}
	// The 30d tag must appear exactly once (no duplicate from the static
	// per-window breakdown).
	if n := strings.Count(got.detail, "30d $"); n != 1 {
		t.Errorf("expected exactly one 30d figure, got %d in %q", n, got.detail)
	}
	// Headline still scoped as before.
	if !strings.Contains(got.summary, "all-time") {
		t.Errorf("summary must still tag the cumulative figure as all-time, got %q", got.summary)
	}
}

// TestDetailedCredits_WindowCreditSpendPartial verifies the "(since YYYY-MM-DD)"
// suffix is appended when the observation history does not cover the full
// window.
func TestDetailedCredits_WindowCreditSpendPartial(t *testing.T) {
	totalCredits := 80.00
	totalUsage := 60.63
	remaining := 19.37
	spend := 4.25

	snap := core.UsageSnapshot{
		ProviderID: "openrouter",
		AccountID:  "openrouter",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"credit_balance": {
				Limit: &totalCredits, Used: &totalUsage, Remaining: &remaining,
				Unit: "USD", Window: "lifetime",
			},
			"window_credit_spend": {Used: &spend, Unit: "USD", Window: "30d"},
		},
		Attributes: map[string]string{
			"window_credit_spend_partial": "true",
			"window_credit_spend_since":   "2026-05-20T08:30:00Z",
		},
	}

	got := computeDisplayInfo(snap, openrouter.New().DashboardWidget(), false)

	if !strings.Contains(got.detail, "30d $4.25") {
		t.Errorf("detail should show the partial windowed figure, got %q", got.detail)
	}
	if !strings.Contains(got.detail, "(since 2026-05-20)") {
		t.Errorf("detail should append the since-date suffix, got %q", got.detail)
	}
}

func TestCreditScopeTag(t *testing.T) {
	cases := map[string]string{
		"lifetime": "all-time",
		"":         "all-time",
		"all-time": "all-time",
		"current":  "current",
		"billing":  "current",
		"7d":       "7d",
	}
	for in, want := range cases {
		if got := creditScopeTag(in); got != want {
			t.Errorf("creditScopeTag(%q) = %q, want %q", in, got, want)
		}
	}
}
