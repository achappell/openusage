package report

import (
	"strings"
	"testing"
	"time"
)

func ev(day string, provider, model string, cost float64, in, out int) Event {
	t, _ := time.Parse(time.RFC3339, day)
	return Event{Time: t, Provider: provider, Model: model, Cost: cost, Input: in, Output: out}
}

func TestBuild_DailyBucketsAndTotals(t *testing.T) {
	events := []Event{
		ev("2026-06-01T10:00:00Z", "claude_code", "opus", 1.0, 100, 10),
		ev("2026-06-01T15:00:00Z", "claude_code", "sonnet", 2.0, 200, 20),
		ev("2026-06-02T09:00:00Z", "claude_code", "opus", 4.0, 400, 40),
	}
	rep := Build(events, Options{Kind: KindDaily})
	if len(rep.Rows) != 2 {
		t.Fatalf("got %d daily rows, want 2", len(rep.Rows))
	}
	if rep.Rows[0].Key != "2026-06-01" || rep.Rows[1].Key != "2026-06-02" {
		t.Errorf("row keys = %q,%q", rep.Rows[0].Key, rep.Rows[1].Key)
	}
	if rep.Rows[0].Cost != 3.0 || rep.Rows[0].Input != 300 {
		t.Errorf("day1 cost=%.1f input=%d, want 3.0/300", rep.Rows[0].Cost, rep.Rows[0].Input)
	}
	if rep.Totals.Cost != 7.0 || rep.Totals.Input != 700 {
		t.Errorf("totals cost=%.1f input=%d, want 7.0/700", rep.Totals.Cost, rep.Totals.Input)
	}
	if len(rep.Rows[0].Models) != 2 {
		t.Errorf("day1 models = %v, want 2 distinct", rep.Rows[0].Models)
	}
}

func TestBuild_DailyBreakdownPerModel(t *testing.T) {
	events := []Event{
		ev("2026-06-01T10:00:00Z", "claude_code", "opus", 5.0, 100, 10),
		ev("2026-06-01T11:00:00Z", "claude_code", "sonnet", 1.0, 50, 5),
		ev("2026-06-01T12:00:00Z", "claude_code", "opus", 3.0, 80, 8),
	}
	rep := Build(events, Options{Kind: KindDaily, Breakdown: true})
	row := rep.Rows[0]
	if len(row.ModelRows) != 2 {
		t.Fatalf("got %d model rows, want 2", len(row.ModelRows))
	}
	// sorted by cost desc: opus (8.0) before sonnet (1.0)
	if row.ModelRows[0].Label != "opus" || row.ModelRows[0].Cost != 8.0 {
		t.Errorf("top model = %q $%.1f, want opus $8.0", row.ModelRows[0].Label, row.ModelRows[0].Cost)
	}
}

func TestBuild_WeeklyMondayStart(t *testing.T) {
	// 2026-06-01 is a Monday; 2026-06-07 is the following Sunday (same week).
	events := []Event{
		ev("2026-06-01T10:00:00Z", "claude_code", "opus", 1.0, 1, 1), // Mon
		ev("2026-06-07T10:00:00Z", "claude_code", "opus", 2.0, 1, 1), // Sun (same wk)
		ev("2026-06-08T10:00:00Z", "claude_code", "opus", 4.0, 1, 1), // next Mon
	}
	rep := Build(events, Options{Kind: KindWeekly, WeekStartMonday: true})
	if len(rep.Rows) != 2 {
		t.Fatalf("got %d weekly rows, want 2", len(rep.Rows))
	}
	if rep.Rows[0].Key != "2026-06-01" || rep.Rows[0].Cost != 3.0 {
		t.Errorf("week1 = %q $%.1f, want 2026-06-01 $3.0", rep.Rows[0].Key, rep.Rows[0].Cost)
	}
	if rep.Rows[1].Key != "2026-06-08" || rep.Rows[1].Cost != 4.0 {
		t.Errorf("week2 = %q $%.1f, want 2026-06-08 $4.0", rep.Rows[1].Key, rep.Rows[1].Cost)
	}
}

func TestBuild_MonthlyBuckets(t *testing.T) {
	events := []Event{
		ev("2026-05-31T10:00:00Z", "claude_code", "opus", 1.0, 1, 1),
		ev("2026-06-01T10:00:00Z", "claude_code", "opus", 2.0, 1, 1),
	}
	rep := Build(events, Options{Kind: KindMonthly})
	if len(rep.Rows) != 2 || rep.Rows[0].Key != "2026-05" || rep.Rows[1].Key != "2026-06" {
		t.Fatalf("monthly keys = %+v", rep.Rows)
	}
}

func TestBuild_SessionGrouping(t *testing.T) {
	mk := func(day, sess, model string, cost float64) Event {
		e := ev(day, "claude_code", model, cost, 10, 1)
		e.Session = sess
		e.Project = "proj"
		return e
	}
	events := []Event{
		mk("2026-06-01T10:00:00Z", "s1", "opus", 1.0),
		mk("2026-06-01T11:00:00Z", "s1", "opus", 2.0),
		mk("2026-06-01T12:00:00Z", "s2", "sonnet", 5.0),
	}
	rep := Build(events, Options{Kind: KindSession})
	if len(rep.Rows) != 2 {
		t.Fatalf("got %d session rows, want 2", len(rep.Rows))
	}
	// ordered by last activity ascending: s1 last at 11:00, s2 at 12:00
	if rep.Rows[0].Key != "s1" || rep.Rows[0].Cost != 3.0 {
		t.Errorf("session1 = %q $%.1f, want s1 $3.0", rep.Rows[0].Key, rep.Rows[0].Cost)
	}
	if !strings.Contains(rep.Rows[0].Label, "proj") {
		t.Errorf("session label missing project: %q", rep.Rows[0].Label)
	}
}

func TestBuild_FiltersSinceUntilProviderProject(t *testing.T) {
	mk := func(day, provider, project string) Event {
		e := ev(day, provider, "opus", 1.0, 1, 1)
		e.Project = project
		return e
	}
	events := []Event{
		mk("2026-05-01T10:00:00Z", "claude_code", "a"),
		mk("2026-06-01T10:00:00Z", "claude_code", "a"),
		mk("2026-06-02T10:00:00Z", "codex", "a"),
		mk("2026-06-03T10:00:00Z", "claude_code", "b"),
	}
	since, _ := time.Parse(time.RFC3339, "2026-05-15T00:00:00Z")
	rep := Build(events, Options{Kind: KindDaily, Since: since, Provider: "claude_code", Project: "a"})
	if len(rep.Rows) != 1 || rep.Rows[0].Key != "2026-06-01" {
		t.Fatalf("filtered rows = %+v, want only 2026-06-01", rep.Rows)
	}
}

func TestBuild_SessionSkipsSyntheticEvents(t *testing.T) {
	e1 := ev("2026-06-01T10:00:00Z", "openrouter", "(total)", 9.0, 0, 0)
	e1.Synthetic = true
	rep := Build([]Event{e1}, Options{Kind: KindSession})
	if len(rep.Rows) != 0 {
		t.Errorf("synthetic event produced %d session rows, want 0", len(rep.Rows))
	}
}
