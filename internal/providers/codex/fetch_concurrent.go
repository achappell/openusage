package codex

import (
	"context"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type codexSessionBreakdownResult struct {
	snapshot core.UsageSnapshot
	err      error
}

type codexDailyCreditResult struct {
	snapshot core.UsageSnapshot
	err      error
}

// startCodexSessionBreakdown runs the expensive all-session walk against an
// isolated snapshot. The caller can continue with account/network fetches
// while the local history is being parsed.
func startCodexSessionBreakdown(
	sessionsDir string,
	base core.UsageSnapshot,
	read func(string, *core.UsageSnapshot) error,
) <-chan codexSessionBreakdownResult {
	snapshot := base.DeepClone()
	ensureCodexSnapshotMaps(&snapshot)
	done := make(chan codexSessionBreakdownResult, 1)
	go func() {
		err := read(sessionsDir, &snapshot)
		done <- codexSessionBreakdownResult{snapshot: snapshot, err: err}
	}()
	return done
}

// startCodexDailyCreditFetch keeps the optional daily account request on its
// own snapshot and lifecycle. It must not share the snapshot being populated
// by the main fetch path with the session scanner.
func startCodexDailyCreditFetch(
	ctx context.Context,
	acct core.AccountConfig,
	configDir string,
	base core.UsageSnapshot,
	fetch func(context.Context, core.AccountConfig, string, *core.UsageSnapshot) error,
) <-chan codexDailyCreditResult {
	snapshot := base.DeepClone()
	ensureCodexSnapshotMaps(&snapshot)
	done := make(chan codexDailyCreditResult, 1)
	go func() {
		err := fetch(ctx, acct, configDir, &snapshot)
		done <- codexDailyCreditResult{snapshot: snapshot, err: err}
	}()
	return done
}

func ensureCodexSnapshotMaps(snap *core.UsageSnapshot) {
	if snap == nil {
		return
	}
	snap.EnsureMaps()
	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
}

// mergeCodexSessionBreakdown adds local analytics without allowing stale
// session values to overwrite live or CLI quota values already in dst.
func mergeCodexSessionBreakdown(dst, src *core.UsageSnapshot) {
	if dst == nil || src == nil {
		return
	}
	ensureCodexSnapshotMaps(dst)

	for key, metric := range src.Metrics {
		if _, exists := dst.Metrics[key]; !exists {
			dst.Metrics[key] = metric
		}
	}
	for key, reset := range src.Resets {
		if _, exists := dst.Resets[key]; !exists {
			dst.Resets[key] = reset
		}
	}
	for key, value := range src.Attributes {
		if _, exists := dst.Attributes[key]; !exists {
			dst.Attributes[key] = value
		}
	}
	for key, value := range src.Diagnostics {
		if _, exists := dst.Diagnostics[key]; !exists {
			dst.Diagnostics[key] = value
		}
	}
	for key, value := range src.Raw {
		if _, exists := dst.Raw[key]; !exists {
			dst.Raw[key] = value
		}
	}
	for key, points := range src.DailySeries {
		if _, exists := dst.DailySeries[key]; !exists {
			dst.DailySeries[key] = append([]core.TimePoint(nil), points...)
		}
	}
	if len(dst.ModelUsage) == 0 && len(src.ModelUsage) > 0 {
		clone := src.DeepClone()
		dst.ModelUsage = clone.ModelUsage
	}
}

// mergeCodexDailyCreditSnapshot copies only the optional account-credit
// enrichment. The account result is authoritative for this series and may
// replace a same-poll local value.
func mergeCodexDailyCreditSnapshot(dst, src *core.UsageSnapshot) {
	if dst == nil || src == nil {
		return
	}
	ensureCodexSnapshotMaps(dst)
	if points, ok := src.DailySeries[codexCreditUsageDailySeriesKey]; ok {
		dst.DailySeries[codexCreditUsageDailySeriesKey] = append([]core.TimePoint(nil), points...)
	}
	for key, value := range src.Raw {
		if strings.HasPrefix(key, "credit_daily_usage_") {
			dst.Raw[key] = value
		}
	}
	if value, ok := src.Diagnostics["credit_daily_usage"]; ok {
		dst.Diagnostics["credit_daily_usage"] = value
	}
}
