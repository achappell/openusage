package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// End-to-end: seed a cumulative balance series, run the read model for a 7d and
// 30d window, and assert window_credit_spend reflects the real observed delta —
// the #175 scenario where lifetime spend is large but recent windowed spend is
// small.
func TestApplyCanonicalTelemetryView_WindowCreditSpend(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	ctx := context.Background()
	seed := func(daysAgo int, used float64) {
		if err := store.RecordBalanceObservations(ctx, "openrouter", "openrouter", []BalanceObservation{{
			MetricKey: "credit_balance", ObservedAt: now.AddDate(0, 0, -daysAgo),
			Used: f64(used), Unit: "USD", Semantics: balanceSemanticsCumulative,
		}}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// Lifetime $60.63; nearly all of it spent long ago; only $0.13 in the last 7d.
	seed(40, 50.00)
	seed(31, 60.00)
	seed(8, 60.50)
	seed(5, 60.55)
	seed(0, 60.63)

	base := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter", AccountID: "openrouter", Status: core.StatusOK,
			Metrics: map[string]core.Metric{
				"credit_balance": {Used: f64(60.63), Limit: f64(80), Remaining: f64(19.37), Unit: "USD", Window: "lifetime"},
			},
		},
	}

	for _, tc := range []struct {
		window core.TimeWindow
		since  time.Time
		want   float64
	}{
		{core.TimeWindow("30d"), now.AddDate(0, 0, -30), 0.63},
		{core.TimeWindow("7d"), now.AddDate(0, 0, -7), 0.13},
	} {
		got, err := ApplyCanonicalTelemetryViewWithOptions(ctx, dbPath, base, ReadModelOptions{
			Since: tc.since, TimeWindow: tc.window,
		})
		if err != nil {
			t.Fatalf("apply (%s): %v", tc.window, err)
		}
		m, ok := got["openrouter"].Metrics["window_credit_spend"]
		if !ok || m.Used == nil {
			t.Fatalf("%s: window_credit_spend missing: %+v", tc.window, got["openrouter"].Metrics["window_credit_spend"])
		}
		if d := *m.Used - tc.want; d > 1e-6 || d < -1e-6 {
			t.Errorf("%s: window_credit_spend = %.4f, want %.4f", tc.window, *m.Used, tc.want)
		}
		if m.Window != string(tc.window) {
			t.Errorf("%s: metric window = %q, want %q", tc.window, m.Window, string(tc.window))
		}
	}
}

// When the provider exposes its own windowed-spend metric for the active
// window, window_credit_spend must use that authoritative value rather than a
// (possibly partial) delta from our observed series.
func TestApplyCanonicalTelemetryView_PrefersNativeWindowMetric(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	ctx := context.Background()
	// Observed series would yield a delta of 5.00 over 30d...
	if err := store.RecordBalanceObservations(ctx, "openrouter", "openrouter", []BalanceObservation{
		{MetricKey: "credit_balance", ObservedAt: now.AddDate(0, 0, -3), Used: f64(100), Unit: "USD", Semantics: balanceSemanticsCumulative},
		{MetricKey: "credit_balance", ObservedAt: now, Used: f64(105), Unit: "USD", Semantics: balanceSemanticsCumulative},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// ...but OpenRouter's own 30d bucket says 2.50 — that must win.
	native := 2.50
	base := map[string]core.UsageSnapshot{
		"openrouter": {
			ProviderID: "openrouter", AccountID: "openrouter", Status: core.StatusOK,
			Metrics: map[string]core.Metric{
				"credit_balance": {Used: f64(105), Limit: f64(120), Remaining: f64(15), Unit: "USD", Window: "lifetime"},
				"30d_api_cost":   {Used: &native, Unit: "USD", Window: "30d"},
			},
		},
	}
	got, err := ApplyCanonicalTelemetryViewWithOptions(ctx, dbPath, base, ReadModelOptions{
		Since: now.AddDate(0, 0, -30), TimeWindow: core.TimeWindow30d,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	m := got["openrouter"].Metrics["window_credit_spend"]
	if m.Used == nil || *m.Used != native {
		t.Fatalf("window_credit_spend = %+v, want native 2.50 (not tracked delta)", m.Used)
	}
	if got["openrouter"].Attributes["window_credit_spend_partial"] == "true" {
		t.Errorf("native metric is authoritative; must not be marked partial")
	}
}
