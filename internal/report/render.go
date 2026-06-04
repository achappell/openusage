package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// WriteJSON encodes the report as indented JSON via a stable view that omits
// zero-value timestamps and internal fields.
func (rep Report) WriteJSON(w io.Writer) error {
	view := reportView{
		Kind:   string(rep.Kind),
		Rows:   make([]rowView, 0, len(rep.Rows)),
		Totals: toRowView(rep.Totals),
		Note:   rep.Note,
	}
	for _, r := range rep.Rows {
		view.Rows = append(view.Rows, toRowView(r))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(view)
}

type reportView struct {
	Kind   string    `json:"kind"`
	Rows   []rowView `json:"rows"`
	Totals rowView   `json:"totals"`
	Note   string    `json:"note,omitempty"`
}

type rowView struct {
	Key                  string    `json:"key"`
	Label                string    `json:"label"`
	Provider             string    `json:"provider,omitempty"`
	Models               []string  `json:"models,omitempty"`
	Input                int       `json:"input_tokens"`
	Output               int       `json:"output_tokens"`
	CacheRead            int       `json:"cache_read_tokens"`
	CacheCreate          int       `json:"cache_creation_tokens"`
	Reasoning            int       `json:"reasoning_tokens,omitempty"`
	TotalTokens          int       `json:"total_tokens"`
	Cost                 float64   `json:"cost_usd"`
	ModelRows            []rowView `json:"model_breakdown,omitempty"`
	Start                string    `json:"start,omitempty"`
	End                  string    `json:"end,omitempty"`
	Active               bool      `json:"active,omitempty"`
	TimeRemainingSeconds float64   `json:"time_remaining_seconds,omitempty"`
	BurnRate             float64   `json:"burn_rate_usd_per_hour,omitempty"`
	ProjectedCost        float64   `json:"projected_cost_usd,omitempty"`
	LastActivity         string    `json:"last_activity,omitempty"`
}

func toRowView(r Row) rowView {
	v := rowView{
		Key:                  r.Key,
		Label:                r.Label,
		Provider:             r.Provider,
		Models:               r.Models,
		Input:                r.Input,
		Output:               r.Output,
		CacheRead:            r.CacheRead,
		CacheCreate:          r.CacheCreate,
		Reasoning:            r.Reasoning,
		TotalTokens:          r.TotalTokens,
		Cost:                 r.Cost,
		Active:               r.Active,
		TimeRemainingSeconds: r.TimeRemainingSeconds,
		BurnRate:             r.BurnRateUSDPerHour,
		ProjectedCost:        r.ProjectedCost,
		Start:                timeOrEmpty(r.Start),
		End:                  timeOrEmpty(r.End),
		LastActivity:         timeOrEmpty(r.LastActivity),
	}
	for _, m := range r.ModelRows {
		v.ModelRows = append(v.ModelRows, toRowView(m))
	}
	return v
}

func timeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// WriteTable renders the report as an aligned text table.
func (rep Report) WriteTable(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	switch rep.Kind {
	case KindBlocks:
		writeBlocksTable(tw, rep)
	case KindSession:
		writeSessionTable(tw, rep)
	default:
		writePeriodicTable(tw, rep)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if rep.Note != "" {
		fmt.Fprintf(w, "\nnote: %s\n", rep.Note)
	}
	return nil
}

func writePeriodicTable(tw *tabwriter.Writer, rep Report) {
	header := strings.ToUpper(string(rep.Kind))
	fmt.Fprintf(tw, "%s\tMODELS\tINPUT\tOUTPUT\tCACHE W\tCACHE R\tTOTAL\tCOST\n", header)
	for _, r := range rep.Rows {
		writeTokenRow(tw, r.Label, modelsLabel(r.Models), r)
		for _, m := range r.ModelRows {
			writeTokenRow(tw, "  └ "+shortModel(m.Label), "", m)
		}
	}
	writeTotalsSeparator(tw, 8)
	writeTokenRow(tw, rep.Totals.Label, "", rep.Totals)
}

func writeSessionTable(tw *tabwriter.Writer, rep Report) {
	fmt.Fprintf(tw, "SESSION\tMODELS\tINPUT\tOUTPUT\tTOTAL\tCOST\tLAST ACTIVITY\n")
	for _, r := range rep.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Label, modelsLabel(r.Models),
			fmtTokens(r.Input), fmtTokens(r.Output), fmtTokens(r.TotalTokens),
			fmtCost(r.Cost), fmtTime(r.LastActivity))
		for _, m := range r.ModelRows {
			fmt.Fprintf(tw, "  └ %s\t\t%s\t%s\t%s\t%s\t\n",
				shortModel(m.Label), fmtTokens(m.Input), fmtTokens(m.Output),
				fmtTokens(m.TotalTokens), fmtCost(m.Cost))
		}
	}
	fmt.Fprintf(tw, "%s\t\t%s\t%s\t%s\t%s\t\n", rep.Totals.Label,
		fmtTokens(rep.Totals.Input), fmtTokens(rep.Totals.Output),
		fmtTokens(rep.Totals.TotalTokens), fmtCost(rep.Totals.Cost))
}

func writeBlocksTable(tw *tabwriter.Writer, rep Report) {
	fmt.Fprintf(tw, "BLOCK START\tSTATE\tINPUT\tOUTPUT\tTOTAL\tCOST\tBURN $/h\tPROJECTED\n")
	for _, r := range rep.Rows {
		state := "done"
		projected := ""
		if r.Active {
			state = "ACTIVE " + fmtDurationShort(r.TimeRemaining) + " left"
			projected = fmtCost(r.ProjectedCost)
		}
		burn := ""
		if r.BurnRateUSDPerHour > 0 {
			burn = fmtCost(r.BurnRateUSDPerHour)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Label, state, fmtTokens(r.Input), fmtTokens(r.Output),
			fmtTokens(r.TotalTokens), fmtCost(r.Cost), burn, projected)
	}
	writeTotalsSeparator(tw, 8)
	fmt.Fprintf(tw, "%s\t\t%s\t%s\t%s\t%s\t\t\n", rep.Totals.Label,
		fmtTokens(rep.Totals.Input), fmtTokens(rep.Totals.Output),
		fmtTokens(rep.Totals.TotalTokens), fmtCost(rep.Totals.Cost))
}

func writeTokenRow(tw *tabwriter.Writer, label, models string, r Row) {
	fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		label, models,
		fmtTokens(r.Input), fmtTokens(r.Output),
		fmtTokens(r.CacheCreate), fmtTokens(r.CacheRead),
		fmtTokens(r.TotalTokens), fmtCost(r.Cost))
}

func writeTotalsSeparator(tw *tabwriter.Writer, cols int) {
	cells := make([]string, cols)
	for i := range cells {
		cells[i] = "---"
	}
	fmt.Fprintln(tw, strings.Join(cells, "\t"))
}

// --- formatting helpers ---

func fmtTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return strconv.Itoa(n)
	}
}

func fmtCost(c float64) string {
	switch {
	case c == 0:
		return "$0.00"
	case c < 0.01:
		return fmt.Sprintf("$%.4f", c)
	default:
		return fmt.Sprintf("$%.2f", c)
	}
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func fmtDurationShort(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func modelsLabel(models []string) string {
	if len(models) == 0 {
		return "-"
	}
	short := make([]string, 0, len(models))
	for _, m := range models {
		short = append(short, shortModel(m))
	}
	if len(short) <= 2 {
		return strings.Join(short, ",")
	}
	return fmt.Sprintf("%s,+%d", strings.Join(short[:2], ","), len(short)-2)
}

// shortModel trims provider prefixes and trailing date stamps for display.
func shortModel(m string) string {
	m = strings.TrimSpace(m)
	if m == "" {
		return "unknown"
	}
	if i := strings.LastIndex(m, "/"); i >= 0 && i < len(m)-1 {
		m = m[i+1:]
	}
	// Drop an 8-digit date suffix (e.g. -20250114).
	parts := strings.Split(m, "-")
	if n := len(parts); n > 1 && len(parts[n-1]) == 8 && isAllDigits(parts[n-1]) {
		m = strings.Join(parts[:n-1], "-")
	}
	return m
}

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
