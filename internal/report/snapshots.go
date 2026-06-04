package report

import (
	"sort"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// FromSnapshots converts provider usage snapshots into day-level synthetic
// events for the daily/weekly/monthly reports. Each provider contributes one
// event per day from its daily cost series; providers that only expose a
// current total contribute a single event dated at the snapshot time.
//
// Events produced here are marked Synthetic, so the session and blocks reports
// (which need real sub-day timestamps) ignore them.
func FromSnapshots(snaps []core.UsageSnapshot) []Event {
	var out []Event
	for _, snap := range snaps {
		out = append(out, eventsFromSnapshot(snap)...)
	}
	return out
}

func eventsFromSnapshot(snap core.UsageSnapshot) []Event {
	// Prefer a real daily series so the time axis is accurate. Providers use
	// different keys for cost and tokens, so probe the known aliases.
	costSeries := firstSeries(snap, "cost_usd", "cost", "spend", "analytics_cost", "credits")
	tokenSeries := firstSeries(snap, "tokens_total", "tokens")
	if len(costSeries) > 0 || len(tokenSeries) > 0 {
		costByDate := seriesByDate(costSeries)
		tokenByDate := seriesByDate(tokenSeries)
		dates := unionDates(costSeries, tokenSeries)
		out := make([]Event, 0, len(dates))
		for _, d := range dates {
			ts := parseSeriesDate(d)
			if ts.IsZero() {
				continue
			}
			out = append(out, Event{
				Time:      ts,
				Provider:  snap.ProviderID,
				Model:     "(total)",
				Cost:      costByDate[d],
				Input:     int(tokenByDate[d]),
				Synthetic: true,
			})
		}
		if len(out) > 0 {
			return out
		}
	}

	// No usable daily series: fall back to a single lifetime-total event so the
	// provider still appears in the unified spend view.
	summary := core.ExtractAnalyticsCostSummary(snap)
	if summary.TotalCostUSD <= 0 {
		return nil
	}
	ts := snap.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	return []Event{{
		Time:      ts,
		Provider:  snap.ProviderID,
		Model:     "(total)",
		Cost:      summary.TotalCostUSD,
		Synthetic: true,
	}}
}

// unionDates returns the sorted union of dates present in either series.
func unionDates(a, b []core.TimePoint) []string {
	seen := map[string]bool{}
	var dates []string
	for _, s := range [][]core.TimePoint{a, b} {
		for _, p := range s {
			if !seen[p.Date] {
				seen[p.Date] = true
				dates = append(dates, p.Date)
			}
		}
	}
	sort.Strings(dates)
	return dates
}

func firstSeries(snap core.UsageSnapshot, keys ...string) []core.TimePoint {
	for _, k := range keys {
		if s, ok := snap.DailySeries[k]; ok && len(s) > 0 {
			return s
		}
	}
	return nil
}

func seriesByDate(points []core.TimePoint) map[string]float64 {
	m := make(map[string]float64, len(points))
	for _, p := range points {
		m[p.Date] = p.Value
	}
	return m
}

func parseSeriesDate(s string) time.Time {
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}
