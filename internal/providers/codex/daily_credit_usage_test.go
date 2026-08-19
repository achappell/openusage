package codex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFetchDailyCreditUsageBuildsCompleteAccountSeries(t *testing.T) {
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"tokens":{"access_token":"test-token","account_id":"acct-123"}}`), 0600); err != nil {
		t.Fatal(err)
	}

	resetAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	observedAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	periodStart, ok := inferCreditPeriodStart(resetAt, observedAt)
	if !ok {
		t.Fatal("expected an inferred period start")
	}
	startDay := startOfCodexDay(periodStart, time.Local)
	today := startOfCodexDay(observedAt, time.Local)
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/backend-api/wham/usage/daily-workspace-user-credit-usage" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct-123" {
			t.Fatalf("ChatGPT-Account-Id = %q, want acct-123", got)
		}
		if got := r.URL.Query().Get("start_date"); got != formatCodexDay(startDay) {
			t.Fatalf("start_date = %q, want %q", got, formatCodexDay(startDay))
		}
		if got := r.URL.Query().Get("end_date"); got != formatCodexDay(today) {
			t.Fatalf("end_date = %q, want %q", got, formatCodexDay(today))
		}
		if got := r.URL.Query().Get("breakdown"); got != "product" {
			t.Fatalf("breakdown = %q, want product", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, fmt.Sprintf(`{
			"data": [
				{"date": %q, "values": {"codex": 10}},
				{"date": %q, "values": {"codex": 999}}
			],
			"data_freshness_ts": "2026-08-03T12:00:00Z"
		}`, formatCodexDay(startDay), formatCodexDay(today)))
	}))
	defer server.Close()

	limit := 100.0
	used := 35.0
	account := core.AccountConfig{
		ID: "test",
		RuntimeHints: map[string]string{
			"config_dir":       tmpDir,
			"auth_file":        authPath,
			"chatgpt_base_url": server.URL + "/backend-api",
		},
	}
	snap := core.NewUsageSnapshot("codex", "test")
	snap.Timestamp = observedAt
	snap.Metrics["codex_credit_limit"] = core.Metric{Limit: &limit, Used: &used, Unit: "credits"}
	snap.Resets["codex_credit_limit"] = resetAt

	p := New()
	p.HTTPClient = server.Client()
	err := p.fetchDailyCreditUsage(context.Background(), account, tmpDir, &snap)
	if err != nil {
		t.Fatalf("fetchDailyCreditUsage() error: %v", err)
	}

	points := snap.DailySeries[codexCreditUsageDailySeriesKey]
	if len(points) != 3 {
		t.Fatalf("daily points = %d, want 3: %+v", len(points), points)
	}
	if points[0].Date != formatCodexDay(startDay) || points[0].Value != 10 {
		t.Fatalf("first daily point = %+v, want %s/10", points[0], formatCodexDay(startDay))
	}
	if points[1].Value != 0 {
		t.Fatalf("missing day value = %v, want 0", points[1].Value)
	}
	// The live cumulative total wins for today: 35 - 10 historical credits.
	if points[2].Date != formatCodexDay(today) || points[2].Value != 25 {
		t.Fatalf("today daily point = %+v, want %s/25", points[2], formatCodexDay(today))
	}
	if snap.Raw["credit_daily_usage_source"] != "account" || snap.Raw["credit_daily_usage_complete"] != "true" {
		t.Fatalf("unexpected daily usage metadata: %+v", snap.Raw)
	}
	if snap.Raw["credit_daily_usage_data_freshness"] != "2026-08-03T12:00:00Z" {
		t.Fatalf("unexpected freshness timestamp: %q", snap.Raw["credit_daily_usage_data_freshness"])
	}

	// Historical daily data is cached for the current day, but today's value
	// must continue to follow the live cumulative quota.
	usedAgain := 40.0
	snapAgain := core.NewUsageSnapshot("codex", "test")
	snapAgain.Timestamp = observedAt
	snapAgain.Metrics["codex_credit_limit"] = core.Metric{Limit: &limit, Used: &usedAgain, Unit: "credits"}
	snapAgain.Resets["codex_credit_limit"] = resetAt
	if err := p.fetchDailyCreditUsage(context.Background(), account, tmpDir, &snapAgain); err != nil {
		t.Fatalf("cached fetchDailyCreditUsage() error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("daily endpoint requests = %d, want one cached request", requests)
	}
	pointsAgain := snapAgain.DailySeries[codexCreditUsageDailySeriesKey]
	if pointsAgain[len(pointsAgain)-1].Value != 30 {
		t.Fatalf("cached today's value = %v, want 30", pointsAgain[len(pointsAgain)-1].Value)
	}
	if snapAgain.Raw["credit_daily_usage_cache"] != "hit" {
		t.Fatalf("expected cache-hit metadata, got %+v", snapAgain.Raw)
	}
}

func TestApplyCreditForecastUsesAccountDailyAverageAndProjectsReserve(t *testing.T) {
	resetAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	observedAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	periodStart, ok := inferCreditPeriodStart(resetAt, observedAt)
	if !ok {
		t.Fatal("expected an inferred period start")
	}
	startDay := startOfCodexDay(periodStart, time.Local)

	limit := 100.0
	used := 35.0
	snap := core.NewUsageSnapshot("codex", "test")
	snap.Timestamp = observedAt
	snap.Metrics["codex_credit_limit"] = core.Metric{Limit: &limit, Used: &used, Unit: "credits"}
	snap.Resets["codex_credit_limit"] = resetAt
	snap.DailySeries = map[string][]core.TimePoint{
		codexCreditUsageDailySeriesKey: {
			{Date: formatCodexDay(startDay), Value: 10},
			{Date: formatCodexDay(startDay.AddDate(0, 0, 1)), Value: 0},
			// This stale today value must be replaced with used - history = 25.
			{Date: formatCodexDay(startDay.AddDate(0, 0, 2)), Value: 999},
		},
	}

	New().applyCreditForecast(&snap, "test")

	dailyAverage := snap.Metrics["codex_credit_daily_average"]
	if dailyAverage.Used == nil || *dailyAverage.Used < 11.666 || *dailyAverage.Used > 11.667 {
		t.Fatalf("daily average = %v, want 35/3", dailyAverage.Used)
	}
	projected := snap.Metrics["codex_credit_projected_credits_at_reset"]
	if projected.Used == nil || *projected.Used < 361.66 || *projected.Used > 361.67 {
		t.Fatalf("projected credits = %v, want about 361.67", projected.Used)
	}
	reserve := snap.Metrics["codex_credit_projected_reserve_at_reset"]
	if reserve.Used == nil || *reserve.Used > -261.66 || *reserve.Used < -261.67 {
		t.Fatalf("projected reserve = %v, want about -261.67", reserve.Used)
	}
	rate := snap.Metrics["codex_credit_burn_rate"]
	if rate.Used == nil || *rate.Used < 0.486 || *rate.Used > 0.487 {
		t.Fatalf("daily burn rate = %v, want about 0.486 credits/hour", rate.Used)
	}
	if snap.Raw["credit_forecast_source"] != "account_daily_history" {
		t.Fatalf("forecast source = %q, want account_daily_history", snap.Raw["credit_forecast_source"])
	}
}
