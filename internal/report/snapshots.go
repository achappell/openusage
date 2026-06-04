package report

import (
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
	costSeries := firstSeries(snap, "cost_usd", "cost", "spend")
	if len(costSeries) > 0 {
		tokenByDate := seriesByDate(firstSeries(snap, "tokens_total"))
		out := make([]Event, 0, len(costSeries))
		for _, pt := range costSeries {
			ts := parseSeriesDate(pt.Date)
			if ts.IsZero() {
				continue
			}
			out = append(out, Event{
				Time:      ts,
				Provider:  snap.ProviderID,
				Model:     "(total)",
				Cost:      pt.Value,
				Input:     int(tokenByDate[pt.Date]),
				Synthetic: true,
			})
		}
		return out
	}

	// No daily cost series: fall back to a single lifetime-total event so the
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
