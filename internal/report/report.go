// Package report builds headless usage/cost reports (daily, weekly, monthly,
// session and 5-hour blocks) from a unified stream of usage events. It is the
// data layer behind the `openusage daily|weekly|monthly|session|blocks`
// subcommands and is deliberately free of any TUI dependency.
//
// Events come from two sources:
//   - Claude Code conversation logs, one event per assistant turn (full
//     fidelity: real timestamps, per-model cost, sessions). See FromClaudeCode.
//   - Provider snapshots, rolled up from each provider's daily cost/token
//     series (or a single point when only a current total is known). See
//     FromSnapshots. These carry day-level granularity only.
//
// daily/weekly/monthly aggregate every event; session/blocks require the
// real sub-day timestamps that only conversation events provide.
package report

import (
	"sort"
	"strings"
	"time"
)

// Kind identifies a report shape.
type Kind string

const (
	KindDaily   Kind = "daily"
	KindWeekly  Kind = "weekly"
	KindMonthly Kind = "monthly"
	KindSession Kind = "session"
	KindBlocks  Kind = "blocks"
)

// DefaultBlockHours is the Claude Code billing-window length.
const DefaultBlockHours = 5.0

// Event is a single usage record in the unified stream.
type Event struct {
	Time        time.Time
	Provider    string
	Model       string // raw model id; "" or "(total)" for snapshot rollups
	Project     string
	Session     string
	Input       int
	Output      int
	CacheRead   int
	CacheCreate int
	Reasoning   int
	Cost        float64
	// Synthetic marks day-level rollups that lack real sub-day timestamps.
	// session/blocks reports exclude these.
	Synthetic bool
}

// TotalTokens returns the sum of every token bucket on the event.
func (e Event) TotalTokens() int {
	return e.Input + e.Output + e.CacheRead + e.CacheCreate + e.Reasoning
}

// Row is one line in a report (a date, week, month, session or block) or one
// per-model sub-line when breakdown is requested. It is the internal
// representation; JSON output goes through the stable view in render.go.
type Row struct {
	Key         string // sort/identity key (date, ISO week, month, session id, block start)
	Label       string // human label
	Provider    string
	Models      []string
	Input       int
	Output      int
	CacheRead   int
	CacheCreate int
	Reasoning   int
	TotalTokens int
	Cost        float64

	// ModelRows holds the per-model breakdown when Options.Breakdown is set.
	ModelRows []Row

	// Block / session extras (zero unless relevant).
	Start                time.Time
	End                  time.Time
	Active               bool
	TimeRemaining        time.Duration
	TimeRemainingSeconds float64
	BurnRateUSDPerHour   float64
	ProjectedCost        float64
	LastActivity         time.Time
}

func (r *Row) add(e Event) {
	r.Input += e.Input
	r.Output += e.Output
	r.CacheRead += e.CacheRead
	r.CacheCreate += e.CacheCreate
	r.Reasoning += e.Reasoning
	r.TotalTokens += e.TotalTokens()
	r.Cost += e.Cost
}

// Report is the aggregated result.
type Report struct {
	Kind   Kind   `json:"kind"`
	Rows   []Row  `json:"rows"`
	Totals Row    `json:"totals"`
	Note   string `json:"note,omitempty"`
}

// Options configures aggregation.
type Options struct {
	Kind            Kind
	Since           time.Time // inclusive lower bound; zero = unbounded
	Until           time.Time // inclusive upper bound; zero = unbounded
	Breakdown       bool      // emit per-model sub-rows
	Provider        string    // filter to one provider id; empty = all
	Project         string    // filter to one project label; empty = all
	WeekStartMonday bool      // weekly bucketing start (default Monday when true)
	Now             time.Time // reference "now" for blocks; zero = time.Now()
	BlockHours      float64   // block length; <=0 = DefaultBlockHours
	TopModels       int       // cap per-row model rows; <=0 = unlimited
}

// Build aggregates the events into a Report according to opts.Kind.
func Build(events []Event, opts Options) Report {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.BlockHours <= 0 {
		opts.BlockHours = DefaultBlockHours
	}

	switch opts.Kind {
	case KindSession:
		return buildSessions(filterEvents(events, opts, true), opts)
	case KindBlocks:
		return buildBlocks(events, opts)
	default:
		return buildPeriodic(filterEvents(events, opts, false), opts)
	}
}

// filterEvents applies the since/until/provider/project filters. When
// requireReal is set, synthetic (day-level) events are dropped because the
// report needs true sub-day timestamps.
func filterEvents(events []Event, opts Options, requireReal bool) []Event {
	out := make([]Event, 0, len(events))
	for _, e := range events {
		if requireReal && e.Synthetic {
			continue
		}
		if opts.Provider != "" && e.Provider != opts.Provider {
			continue
		}
		if opts.Project != "" && !strings.EqualFold(e.Project, opts.Project) {
			continue
		}
		if !opts.Since.IsZero() && e.Time.Before(opts.Since) {
			continue
		}
		if !opts.Until.IsZero() && e.Time.After(opts.Until) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// buildPeriodic groups events into date/week/month buckets.
func buildPeriodic(events []Event, opts Options) Report {
	buckets := map[string]*Row{}
	order := []string{}
	// modelAgg[bucketKey][rawModel] accumulates per-model breakdown.
	modelAgg := map[string]map[string]*Row{}

	for _, e := range events {
		key, label := periodKey(e.Time, opts.Kind, opts.WeekStartMonday)
		row, ok := buckets[key]
		if !ok {
			row = &Row{Key: key, Label: label}
			buckets[key] = row
			order = append(order, key)
			modelAgg[key] = map[string]*Row{}
		}
		row.add(e)
		addModel(row, e.Model)

		if opts.Breakdown {
			m := strings.TrimSpace(e.Model)
			if m == "" {
				m = "(unknown)"
			}
			mr, ok := modelAgg[key][m]
			if !ok {
				mr = &Row{Key: m, Label: m}
				modelAgg[key][m] = mr
			}
			mr.add(e)
		}
	}

	sort.Strings(order)
	rep := Report{Kind: opts.Kind}
	for _, key := range order {
		row := *buckets[key]
		sort.Strings(row.Models)
		if opts.Breakdown {
			row.ModelRows = sortedModelRows(modelAgg[key], opts.TopModels)
		}
		rep.Rows = append(rep.Rows, row)
		rep.Totals.add(eventFromRow(row))
	}
	finalizeTotals(&rep)
	return rep
}

// buildSessions groups conversation events by session id.
func buildSessions(events []Event, opts Options) Report {
	buckets := map[string]*Row{}
	order := []string{}
	modelAgg := map[string]map[string]*Row{}

	for _, e := range events {
		sid := e.Session
		if sid == "" {
			continue
		}
		row, ok := buckets[sid]
		if !ok {
			label := sid
			if len(label) > 12 {
				label = label[:12]
			}
			if e.Project != "" {
				label = label + " · " + e.Project
			}
			row = &Row{Key: sid, Label: label, Provider: e.Provider}
			buckets[sid] = row
			order = append(order, sid)
			modelAgg[sid] = map[string]*Row{}
		}
		row.add(e)
		addModel(row, e.Model)
		if e.Time.After(row.LastActivity) {
			row.LastActivity = e.Time
		}
		if opts.Breakdown {
			m := strings.TrimSpace(e.Model)
			if m == "" {
				m = "(unknown)"
			}
			mr, ok := modelAgg[sid][m]
			if !ok {
				mr = &Row{Key: m, Label: m}
				modelAgg[sid][m] = mr
			}
			mr.add(e)
		}
	}

	rep := Report{Kind: KindSession}
	rows := make([]Row, 0, len(order))
	for _, sid := range order {
		row := *buckets[sid]
		sort.Strings(row.Models)
		if opts.Breakdown {
			row.ModelRows = sortedModelRows(modelAgg[sid], opts.TopModels)
		}
		rows = append(rows, row)
	}
	// Most recent activity last (ascending), matching the periodic reports.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].LastActivity.Before(rows[j].LastActivity) })
	for _, row := range rows {
		rep.Rows = append(rep.Rows, row)
		rep.Totals.add(eventFromRow(row))
	}
	finalizeTotals(&rep)
	return rep
}

// periodKey returns the bucket key and human label for a timestamp.
func periodKey(t time.Time, kind Kind, weekStartMonday bool) (string, string) {
	switch kind {
	case KindWeekly:
		start := startOfWeek(t, weekStartMonday)
		key := start.Format("2006-01-02")
		return key, key + " (wk)"
	case KindMonthly:
		key := t.Format("2006-01")
		return key, key
	default: // daily
		key := t.Format("2006-01-02")
		return key, key
	}
}

func startOfWeek(t time.Time, monday bool) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	wd := int(day.Weekday()) // Sunday=0
	var back int
	if monday {
		back = (wd + 6) % 7 // Monday=0
	} else {
		back = wd
	}
	return day.AddDate(0, 0, -back)
}

func addModel(row *Row, model string) {
	m := strings.TrimSpace(model)
	if m == "" {
		return
	}
	for _, existing := range row.Models {
		if existing == m {
			return
		}
	}
	row.Models = append(row.Models, m)
}

func sortedModelRows(agg map[string]*Row, top int) []Row {
	rows := make([]Row, 0, len(agg))
	for _, r := range agg {
		rows = append(rows, *r)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Cost != rows[j].Cost {
			return rows[i].Cost > rows[j].Cost
		}
		return rows[i].TotalTokens > rows[j].TotalTokens
	})
	if top > 0 && len(rows) > top {
		rows = rows[:top]
	}
	return rows
}

// eventFromRow adapts an aggregated row back into an Event so totals can reuse
// Row.add without duplicating the field list.
func eventFromRow(r Row) Event {
	return Event{
		Input:       r.Input,
		Output:      r.Output,
		CacheRead:   r.CacheRead,
		CacheCreate: r.CacheCreate,
		Reasoning:   r.Reasoning,
		Cost:        r.Cost,
	}
}

func finalizeTotals(rep *Report) {
	rep.Totals.Label = "TOTAL"
	rep.Totals.Key = "total"
}
