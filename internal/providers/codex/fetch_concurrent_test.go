package codex

import (
	"context"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestCodexDailyCreditTaskRunsWhileSessionBreakdownIsBlocked(t *testing.T) {
	base := core.NewUsageSnapshot("codex", "account")
	base.DailySeries = make(map[string][]core.TimePoint)

	scanStarted := make(chan struct{})
	releaseScan := make(chan struct{})
	scanDone := startCodexSessionBreakdown("/sessions", base, func(_ string, _ *core.UsageSnapshot) error {
		close(scanStarted)
		<-releaseScan
		return nil
	})
	<-scanStarted

	dailyFinished := make(chan struct{})
	dailyDone := startCodexDailyCreditFetch(
		context.Background(),
		core.AccountConfig{ID: "account"},
		"/config",
		base,
		func(_ context.Context, _ core.AccountConfig, _ string, _ *core.UsageSnapshot) error {
			close(dailyFinished)
			return nil
		},
	)

	select {
	case <-dailyFinished:
	case <-time.After(time.Second):
		t.Fatal("daily credit fetch waited for the blocked session breakdown")
	}

	close(releaseScan)
	if result := <-scanDone; result.err != nil {
		t.Fatalf("session breakdown returned error: %v", result.err)
	}
	if result := <-dailyDone; result.err != nil {
		t.Fatalf("daily credit fetch returned error: %v", result.err)
	}
}

func TestMergeCodexSessionBreakdownPreservesRemoteQuota(t *testing.T) {
	remote := core.NewUsageSnapshot("codex", "account")
	ensureCodexSnapshotMaps(&remote)
	remoteUsed := 90.0
	remoteLimit := 100.0
	remoteReset := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	remote.Metrics["codex_credit_limit"] = core.Metric{Limit: &remoteLimit, Used: &remoteUsed, Unit: "credits"}
	remote.Resets["codex_credit_limit"] = remoteReset
	remote.Raw["rate_limit_source"] = "live"
	remote.DailySeries[codexCreditUsageDailySeriesKey] = []core.TimePoint{{Date: "2026-08-20", Value: 90}}

	local := core.NewUsageSnapshot("codex", "account")
	ensureCodexSnapshotMaps(&local)
	localUsed := 10.0
	localLimit := 100.0
	localReset := remoteReset.Add(-24 * time.Hour)
	local.Metrics["codex_credit_limit"] = core.Metric{Limit: &localLimit, Used: &localUsed, Unit: "credits"}
	local.Resets["codex_credit_limit"] = localReset
	local.Metrics["model_gpt_5_codex_total_tokens"] = core.Metric{Used: core.Float64Ptr(42), Unit: "tokens"}
	local.Raw["model_usage"] = "gpt-5-codex 42"
	local.DailySeries["tokens_model_gpt_5_codex"] = []core.TimePoint{{Date: "2026-08-20", Value: 42}}

	mergeCodexSessionBreakdown(&remote, &local)

	if got := *remote.Metrics["codex_credit_limit"].Used; got != remoteUsed {
		t.Fatalf("remote credit usage was overwritten: got %.1f, want %.1f", got, remoteUsed)
	}
	if got := remote.Resets["codex_credit_limit"]; !got.Equal(remoteReset) {
		t.Fatalf("remote credit reset was overwritten: got %s, want %s", got, remoteReset)
	}
	if got := remote.Raw["rate_limit_source"]; got != "live" {
		t.Fatalf("remote rate-limit source was overwritten: got %q", got)
	}
	if _, ok := remote.Metrics["model_gpt_5_codex_total_tokens"]; !ok {
		t.Fatal("local breakdown metric was not merged")
	}
	if got := remote.Raw["model_usage"]; got != "gpt-5-codex 42" {
		t.Fatalf("local breakdown raw summary was not merged: %q", got)
	}
	if got := remote.DailySeries[codexCreditUsageDailySeriesKey][0].Value; got != 90 {
		t.Fatalf("account daily series was overwritten: got %.1f, want 90", got)
	}
}

func TestMergeCodexQuotaSnapshotUsesCLIFallbackWhenLiveIsUnavailable(t *testing.T) {
	dst := core.NewUsageSnapshot("codex", "account")
	ensureCodexSnapshotMaps(&dst)
	used := 80.0
	limit := 100.0
	dst.Metrics["rate_limit_primary"] = core.Metric{Limit: &limit, Used: &used, Unit: "%", Window: "5h"}
	dst.Raw["rate_limit_source"] = "session"

	live := core.NewUsageSnapshot("codex", "account")
	ensureCodexSnapshotMaps(&live)
	live.Raw["rate_limit_source"] = "live_unavailable"
	mergeCodexQuotaSnapshot(&dst, &live, true)
	if got := *dst.Metrics["rate_limit_primary"].Used; got != 80 {
		t.Fatalf("live response without windows should retain session quota, got %.1f", got)
	}
	// Fetch clears session windows once it has a usable live or CLI window.
	clearRateLimitMetrics(&dst)

	cli := core.NewUsageSnapshot("codex", "account")
	ensureCodexSnapshotMaps(&cli)
	cliUsed := 20.0
	cliLimit := 100.0
	cli.Metrics["rate_limit_primary"] = core.Metric{Limit: &cliLimit, Used: &cliUsed, Unit: "%", Window: "5h"}
	cli.Raw["rate_limit_source"] = "cli_rpc"
	mergeCodexQuotaSnapshot(&dst, &cli, false)

	if got := *dst.Metrics["rate_limit_primary"].Used; got != 20 {
		t.Fatalf("CLI fallback used = %.1f, want 20", got)
	}
	if got := dst.Raw["rate_limit_source"]; got != "cli_rpc" {
		t.Fatalf("rate_limit_source = %q, want cli_rpc", got)
	}
}

func TestMergeCodexQuotaPoolsKeepWindowAndResetAttribution(t *testing.T) {
	dst := core.NewUsageSnapshot("codex", "account")
	live := core.NewUsageSnapshot("codex", "account")
	cli := core.NewUsageSnapshot("codex", "account")
	live.Metrics["rate_limit_primary"] = core.Metric{Used: core.Float64Ptr(40), Window: "7d"}
	live.Raw["rate_limit_source"] = "live"
	cli.Metrics["rate_limit_primary"] = core.Metric{Used: core.Float64Ptr(50), Window: "5h"}
	cli.Metrics["rate_limit_secondary"] = core.Metric{Used: core.Float64Ptr(60), Window: "7d"}
	cli.Resets["rate_limit_primary"] = time.Unix(100, 0)
	cli.Resets["rate_limit_secondary"] = time.Unix(200, 0)
	cli.Metrics["rate_limit_other_primary"] = core.Metric{Used: core.Float64Ptr(70), Window: "5h"}
	cli.Resets["rate_limit_other_primary"] = time.Unix(300, 0)
	cli.Raw["rate_limit_other_primary_bucket"] = "Other pool"
	mergeCodexQuotaSnapshot(&dst, &live, true)
	mergeCodexQuotaSnapshot(&dst, &cli, false)
	if got := *dst.Metrics["rate_limit_primary"].Used; got != 40 {
		t.Fatalf("live window replaced: %v", got)
	}
	if _, ok := dst.Metrics["rate_limit_secondary"]; ok {
		t.Fatal("secondary window borrowed from a different source")
	}
	if _, ok := dst.Resets["rate_limit_primary"]; ok {
		t.Fatal("reset borrowed from a different source")
	}
	if got := dst.Resets["rate_limit_other_primary"].Unix(); got != 300 || dst.Raw["rate_limit_other_primary_bucket"] != "Other pool" {
		t.Fatalf("other pool attribution lost: reset=%d raw=%v", got, dst.Raw)
	}
}

func TestMergeCodexSessionBreakdownCannotRestoreRemoteMissingWindow(t *testing.T) {
	dst := core.NewUsageSnapshot("codex", "account")
	workerBase := core.NewUsageSnapshot("codex", "account")
	release := make(chan struct{})
	done := startCodexSessionBreakdown("/sessions", workerBase, func(_ string, worker *core.UsageSnapshot) error {
		<-release
		worker.Metrics["rate_limit_secondary"] = core.Metric{Used: core.Float64Ptr(90), Window: "5h"}
		worker.Resets["rate_limit_secondary"] = time.Unix(100, 0)
		worker.Raw["rate_limit_secondary_bucket"] = "stale"
		worker.Metrics["model_example_total_tokens"] = core.Metric{Used: core.Float64Ptr(42)}
		return nil
	})
	dst.Metrics["rate_limit_primary"] = core.Metric{Used: core.Float64Ptr(20), Window: "7d"}
	dst.Raw["rate_limit_source"] = "cli_rpc"
	close(release)
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	mergeCodexSessionBreakdown(&dst, &result.snapshot)
	if _, ok := dst.Metrics["rate_limit_secondary"]; ok {
		t.Fatal("worker restored stale session quota")
	}
	if _, ok := dst.Resets["rate_limit_secondary"]; ok || dst.Raw["rate_limit_secondary_bucket"] != "" {
		t.Fatal("worker restored stale quota attribution")
	}
	if _, ok := dst.Metrics["model_example_total_tokens"]; !ok {
		t.Fatal("worker analytics were lost")
	}
}
