package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteJSON_OmitsZeroTimestamps(t *testing.T) {
	rep := Build([]Event{
		ev("2026-06-01T10:00:00Z", "claude_code", "opus", 1.0, 100, 10),
	}, Options{Kind: KindDaily})

	var buf bytes.Buffer
	if err := rep.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "0001-01-01") {
		t.Errorf("JSON leaked zero-value timestamp:\n%s", out)
	}
	if !strings.Contains(out, `"cost_usd": 1`) {
		t.Errorf("JSON missing cost_usd:\n%s", out)
	}
	if !strings.Contains(out, `"kind": "daily"`) {
		t.Errorf("JSON missing kind:\n%s", out)
	}
}

func TestWriteJSON_BlockIncludesProjectionFields(t *testing.T) {
	events := []Event{
		{Time: at("2026-06-01T12:00:00Z"), Provider: "claude_code", Model: "opus", Cost: 10, Input: 100},
		{Time: at("2026-06-01T13:00:00Z"), Provider: "claude_code", Model: "opus", Cost: 10, Input: 100},
	}
	rep := Build(events, Options{Kind: KindBlocks, Now: at("2026-06-01T13:00:00Z")})

	var buf bytes.Buffer
	if err := rep.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"time_remaining_seconds", "burn_rate_usd_per_hour", "projected_cost_usd", `"active": true`} {
		if !strings.Contains(out, want) {
			t.Errorf("block JSON missing %q:\n%s", want, out)
		}
	}
}

func TestWriteTable_RendersHeaderAndTotals(t *testing.T) {
	rep := Build([]Event{
		ev("2026-06-01T10:00:00Z", "claude_code", "opus", 1.5, 1_500_000, 10),
	}, Options{Kind: KindDaily})

	var buf bytes.Buffer
	if err := rep.WriteTable(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "DAILY") || !strings.Contains(out, "COST") {
		t.Errorf("table missing header:\n%s", out)
	}
	if !strings.Contains(out, "TOTAL") {
		t.Errorf("table missing totals row:\n%s", out)
	}
	if !strings.Contains(out, "1.5M") {
		t.Errorf("table missing humanized tokens (1.5M):\n%s", out)
	}
	if !strings.Contains(out, "$1.50") {
		t.Errorf("table missing formatted cost:\n%s", out)
	}
}

func TestShortModel(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-5-20250114": "claude-opus-4-5",
		"anthropic/claude-sonnet":  "claude-sonnet",
		"opus":                     "opus",
	}
	for in, want := range cases {
		if got := shortModel(in); got != want {
			t.Errorf("shortModel(%q) = %q, want %q", in, got, want)
		}
	}
}
