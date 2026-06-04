package report

import (
	"sort"
	"strings"
	"time"
)

// buildBlocks groups conversation events into fixed-length billing windows
// (5 hours by default), mirroring Claude Code's session billing blocks. The
// active block carries a burn rate and an end-of-block projection.
//
// A new block starts when an event is more than one window length past either
// the current block's start or the previous event (an idle gap). Synthetic
// day-level events are excluded because blocks need true sub-day timestamps.
func buildBlocks(events []Event, opts Options) Report {
	dur := time.Duration(opts.BlockHours * float64(time.Hour))

	// Provider/project filter only; since/until is applied to whole blocks
	// afterwards so boundary blocks are computed from the full history.
	src := make([]Event, 0, len(events))
	for _, e := range events {
		if e.Synthetic {
			continue
		}
		if opts.Provider != "" && e.Provider != opts.Provider {
			continue
		}
		if opts.Project != "" && !strings.EqualFold(e.Project, opts.Project) {
			continue
		}
		src = append(src, e)
	}
	sort.SliceStable(src, func(i, j int) bool { return src[i].Time.Before(src[j].Time) })

	rep := Report{Kind: KindBlocks}
	if len(src) == 0 {
		rep.Note = "no per-turn usage events (blocks need a local-log provider such as Claude Code, Codex, Gemini CLI, Cursor or OpenCode)"
		finalizeTotals(&rep)
		return rep
	}

	var (
		cur        *Row
		blockStart time.Time
		lastEntry  time.Time
	)
	flush := func() {
		if cur != nil {
			sort.Strings(cur.Models)
			rep.Rows = append(rep.Rows, *cur)
		}
		cur = nil
	}

	for _, e := range src {
		if cur == nil {
			blockStart = floorHour(e.Time)
		} else if e.Time.Sub(blockStart) >= dur || e.Time.Sub(lastEntry) >= dur {
			flush()
			blockStart = floorHour(e.Time)
		}
		if cur == nil {
			cur = &Row{
				Key:   blockStart.UTC().Format(time.RFC3339),
				Label: blockStart.Format("2006-01-02 15:04"),
				Start: blockStart,
				End:   blockStart.Add(dur),
			}
		}
		cur.add(e)
		addModel(cur, e.Model)
		lastEntry = e.Time
		cur.LastActivity = e.Time
	}
	flush()

	now := opts.Now
	out := rep.Rows[:0]
	for i := range rep.Rows {
		b := rep.Rows[i]
		// Drop blocks fully outside the requested window.
		if !opts.Since.IsZero() && b.End.Before(opts.Since) {
			continue
		}
		if !opts.Until.IsZero() && b.Start.After(opts.Until) {
			continue
		}
		annotateBlock(&b, now)
		out = append(out, b)
		rep.Totals.add(eventFromRow(b))
	}
	rep.Rows = out
	finalizeTotals(&rep)
	return rep
}

// annotateBlock fills in burn rate, active flag and projection for a block.
func annotateBlock(b *Row, now time.Time) {
	active := now.Before(b.End) && !now.Before(b.Start) && now.Sub(b.LastActivity) < b.End.Sub(b.Start)
	b.Active = active

	durMin := b.LastActivity.Sub(b.Start).Minutes()
	if durMin <= 0 {
		// Single-entry block: treat as the elapsed time so the rate is finite.
		durMin = now.Sub(b.Start).Minutes()
	}
	if durMin > 0 && b.Cost > 0 {
		b.BurnRateUSDPerHour = b.Cost / durMin * 60.0
	}
	if active {
		b.TimeRemaining = b.End.Sub(now)
		b.TimeRemainingSeconds = b.TimeRemaining.Seconds()
		remainingMin := b.TimeRemaining.Minutes()
		if remainingMin > 0 && b.BurnRateUSDPerHour > 0 {
			b.ProjectedCost = b.Cost + b.BurnRateUSDPerHour/60.0*remainingMin
		} else {
			b.ProjectedCost = b.Cost
		}
	}
}

func floorHour(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}

// ActiveBlock returns the currently-active block from a blocks report, if any.
// Used by the statusline.
func (rep Report) ActiveBlock() (Row, bool) {
	for _, r := range rep.Rows {
		if r.Active {
			return r, true
		}
	}
	return Row{}, false
}
