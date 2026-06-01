package telemetry

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

func seedObs(t *testing.T, s *Store, provider, account, metric, semantics string, at time.Time, used, remaining *float64) {
	t.Helper()
	err := s.RecordBalanceObservations(context.Background(), provider, account, []BalanceObservation{{
		MetricKey:  metric,
		ObservedAt: at,
		Used:       used,
		Remaining:  remaining,
		Unit:       "USD",
		Semantics:  semantics,
	}})
	if err != nil {
		t.Fatalf("seed obs: %v", err)
	}
}

func newObsStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// Cumulative: spend over a window = used(now) − used(at-or-before window start).
func TestWindowedSpend_Cumulative(t *testing.T) {
	s := newObsStore(t)
	now := time.Now().UTC()
	// A monotonic used-counter observed across 40 days. Continuous polling
	// (thinned to daily/hourly) means a reading exists near every window edge.
	seedObs(t, s, "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now.AddDate(0, 0, -40), f64(50.00), nil)
	seedObs(t, s, "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now.AddDate(0, 0, -31), f64(60.00), nil)
	seedObs(t, s, "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now.AddDate(0, 0, -8), f64(60.50), nil)
	seedObs(t, s, "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now.AddDate(0, 0, -5), f64(60.55), nil)
	seedObs(t, s, "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now, f64(60.63), nil)

	// 30d window: used now (60.63) − used at ~30d ago (60.00) = 0.63.
	got, err := s.WindowedSpend(context.Background(), "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("WindowedSpend: %v", err)
	}
	if !got.OK || got.Partial {
		t.Fatalf("expected complete result, got %+v", got)
	}
	if !approx(got.Spend, 0.63) {
		t.Errorf("30d spend = %.4f, want 0.63", got.Spend)
	}

	// 7d window: 60.63 − 60.50 = 0.13.
	got7, _ := s.WindowedSpend(context.Background(), "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now.AddDate(0, 0, -7))
	if !approx(got7.Spend, 0.13) {
		t.Errorf("7d spend = %.4f, want 0.13", got7.Spend)
	}
}

// The #175 scenario: a lifetime counter that has not moved recently → 0 in window.
func TestWindowedSpend_Issue175_NoRecentActivity(t *testing.T) {
	s := newObsStore(t)
	now := time.Now().UTC()
	// $60.63 lifetime, flat for the last 60 days.
	seedObs(t, s, "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now.AddDate(0, 0, -60), f64(60.63), nil)
	seedObs(t, s, "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now.AddDate(0, 0, -1), f64(60.63), nil)
	seedObs(t, s, "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now, f64(60.63), nil)

	got, _ := s.WindowedSpend(context.Background(), "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now.AddDate(0, 0, -30))
	if !got.OK || !approx(got.Spend, 0.0) {
		t.Errorf("30d spend with no recent activity = %+v, want 0", got)
	}
}

// Cumulative with no observation before the window start → partial coverage.
func TestWindowedSpend_CumulativePartial(t *testing.T) {
	s := newObsStore(t)
	now := time.Now().UTC()
	// We only started observing 3 days ago, but the window is 30d.
	seedObs(t, s, "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now.AddDate(0, 0, -3), f64(100.0), nil)
	seedObs(t, s, "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now, f64(105.0), nil)

	got, _ := s.WindowedSpend(context.Background(), "openrouter", "openrouter", "credit_balance", balanceSemanticsCumulative, now.AddDate(0, 0, -30))
	if !got.OK || !got.Partial {
		t.Fatalf("expected partial, got %+v", got)
	}
	if !approx(got.Spend, 5.0) {
		t.Errorf("partial spend = %.4f, want 5.0 (delta over observed range)", got.Spend)
	}
}

// Balance: spend = sum of drops; a mid-window top-up is excluded from spend.
func TestWindowedSpend_BalanceWithTopup(t *testing.T) {
	s := newObsStore(t)
	now := time.Now().UTC()
	// Anchor before the window so the first in-window drop is counted.
	seedObs(t, s, "moonshot", "moonshot", "available_balance", balanceSemanticsBalance, now.AddDate(0, 0, -10), nil, f64(10.00))
	// Within a 7d window: 10 → 7 (spend 3), top-up to 12, → 9 (spend 3).
	seedObs(t, s, "moonshot", "moonshot", "available_balance", balanceSemanticsBalance, now.AddDate(0, 0, -5), nil, f64(7.00))
	seedObs(t, s, "moonshot", "moonshot", "available_balance", balanceSemanticsBalance, now.AddDate(0, 0, -3), nil, f64(12.00)) // top-up +5
	seedObs(t, s, "moonshot", "moonshot", "available_balance", balanceSemanticsBalance, now, nil, f64(9.00))

	got, err := s.WindowedSpend(context.Background(), "moonshot", "moonshot", "available_balance", balanceSemanticsBalance, now.AddDate(0, 0, -7))
	if err != nil {
		t.Fatalf("WindowedSpend: %v", err)
	}
	// Drops: 10→7 (3) + 12→9 (3) = 6. Top-up 7→12 (5) excluded.
	if !approx(got.Spend, 6.0) {
		t.Errorf("balance spend = %.4f, want 6.0", got.Spend)
	}
	if !approx(got.TopUps, 5.0) {
		t.Errorf("topups = %.4f, want 5.0", got.TopUps)
	}
}

// Limit semantics carry no spend signal.
func TestWindowedSpend_LimitSkipped(t *testing.T) {
	s := newObsStore(t)
	got, err := s.WindowedSpend(context.Background(), "cursor", "cursor", "spend_limit", balanceSemanticsLimit, time.Now().AddDate(0, 0, -7))
	if err != nil {
		t.Fatalf("WindowedSpend: %v", err)
	}
	if got.OK {
		t.Errorf("limit semantics should not produce a spend figure, got %+v", got)
	}
}

// Counter reset (cumulative value drops, e.g. new billing cycle) → spend is the
// post-reset cumulative, flagged partial.
func TestWindowedSpend_CumulativeReset(t *testing.T) {
	s := newObsStore(t)
	now := time.Now().UTC()
	seedObs(t, s, "mistral", "mistral", "monthly_spend", balanceSemanticsCumulative, now.AddDate(0, 0, -10), f64(40.0), nil)
	seedObs(t, s, "mistral", "mistral", "monthly_spend", balanceSemanticsCumulative, now, f64(3.0), nil) // reset

	got, _ := s.WindowedSpend(context.Background(), "mistral", "mistral", "monthly_spend", balanceSemanticsCumulative, now.AddDate(0, 0, -7))
	if !got.OK || !got.Partial || !approx(got.Spend, 3.0) {
		t.Errorf("reset handling = %+v, want spend 3.0 partial", got)
	}
}

func TestPruneBalanceObservations_Thins(t *testing.T) {
	s := newObsStore(t)
	now := time.Now().UTC()
	// Two rows on the same old day (40d ago, beyond the 35d floor → both deleted).
	seedObs(t, s, "p", "a", "m", balanceSemanticsCumulative, now.AddDate(0, 0, -40), f64(1), nil)
	// Two rows in the same hour ~5 days ago → thinned to one.
	seedObs(t, s, "p", "a", "m", balanceSemanticsCumulative, now.AddDate(0, 0, -5).Truncate(time.Hour).Add(2*time.Minute), f64(2), nil)
	seedObs(t, s, "p", "a", "m", balanceSemanticsCumulative, now.AddDate(0, 0, -5).Truncate(time.Hour).Add(40*time.Minute), f64(3), nil)
	// A recent row (<48h) → untouched.
	seedObs(t, s, "p", "a", "m", balanceSemanticsCumulative, now.Add(-time.Hour), f64(4), nil)

	if _, err := s.PruneBalanceObservations(context.Background(), 30); err != nil {
		t.Fatalf("prune: %v", err)
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM balance_observations`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	// 40d row deleted, two hourly rows thinned to one, recent row kept = 2.
	if count != 2 {
		t.Errorf("post-prune row count = %d, want 2", count)
	}
}

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}
