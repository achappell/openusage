package roocode

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseTaskDir_HappyPath uses the checked-in fixture to verify that a
// realistic ui_messages.json/api_conversation_history.json pair produces
// the expected per-call records, with cost/token normalisation applied and
// the last `<model>` tag selected.
func TestParseTaskDir_HappyPath(t *testing.T) {
	taskDir := filepath.Join("testdata", "task-sample")
	evt, err := ParseTaskDir(taskDir, ClientRooCode)
	if err != nil {
		t.Fatalf("ParseTaskDir: %v", err)
	}
	if evt == nil {
		t.Fatal("ParseTaskDir returned nil event")
	}
	// The fixture has three api_req_started entries: two valid, one with a
	// non-JSON text payload (should be skipped silently).
	if got, want := len(evt.Calls), 2; got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}

	// Last <model> tag is "claude-sonnet-4-5".
	if got, want := evt.Model, "claude-sonnet-4-5"; got != want {
		t.Errorf("model = %q, want %q", got, want)
	}

	// Provider split: "bedrock/anthropic" should reduce to "bedrock" on its
	// own call but the dominant task-level provider depends on tally.
	first := evt.Calls[0]
	if first.Provider != "anthropic" {
		t.Errorf("call0 provider = %q, want anthropic", first.Provider)
	}
	if first.Cost != 0.0123 {
		t.Errorf("call0 cost = %v, want 0.0123", first.Cost)
	}
	if first.TokensIn != 1000 || first.TokensOut != 500 {
		t.Errorf("call0 tokens = (%d/%d), want (1000/500)", first.TokensIn, first.TokensOut)
	}

	second := evt.Calls[1]
	if second.Provider != "bedrock" {
		t.Errorf("call1 provider = %q, want bedrock", second.Provider)
	}

	// Client attribution flows through.
	for _, c := range evt.Calls {
		if c.Client != ClientRooCode {
			t.Errorf("call client = %q, want %q", c.Client, ClientRooCode)
		}
		if c.TaskID != "task-sample" {
			t.Errorf("call TaskID = %q, want task-sample", c.TaskID)
		}
	}
}

// TestParseTaskDir_MissingUIMessages verifies that a task directory
// without ui_messages.json returns the IsNoUIMessages sentinel error so
// callers can skip silently.
func TestParseTaskDir_MissingUIMessages(t *testing.T) {
	dir := t.TempDir()
	_, err := ParseTaskDir(dir, ClientRooCode)
	if err == nil {
		t.Fatal("expected error for missing ui_messages.json")
	}
	if !IsNoUIMessages(err) {
		t.Errorf("err = %v, want IsNoUIMessages true", err)
	}
}

// TestParseUIMessages_MalformedEntriesSkipped builds a synthetic
// ui_messages.json with a mix of valid/invalid entries and asserts the
// parser tolerates malformed payloads instead of failing the whole task.
func TestParseUIMessages_MalformedEntriesSkipped(t *testing.T) {
	doc := `[
{"say":"api_req_started","text":"{\"cost\":0.5,\"tokensIn\":10,\"tokensOut\":5,\"apiProtocol\":\"anthropic\"}"},
{"say":"api_req_started","text":"definitely not json"},
{"say":"api_req_started","text":""},
{"say":"something_else","text":"{\"cost\":99}"},
{"say":"api_req_started","text":"{\"cost\":-3,\"tokensIn\":-7,\"tokensOut\":8,\"apiProtocol\":\"openai/chat\"}"}
]`
	calls, err := parseUIMessages([]byte(doc))
	if err != nil {
		t.Fatalf("parseUIMessages: %v", err)
	}
	if got, want := len(calls), 2; got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
	if calls[0].Cost != 0.5 {
		t.Errorf("call0 cost = %v, want 0.5", calls[0].Cost)
	}
	// Negative cost / tokens must clamp to zero.
	if calls[1].Cost != 0 {
		t.Errorf("call1 cost = %v, want 0 (negative clamped)", calls[1].Cost)
	}
	if calls[1].TokensIn != 0 {
		t.Errorf("call1 tokens in = %v, want 0 (negative clamped)", calls[1].TokensIn)
	}
	// Provider is split on the first `/`.
	if calls[1].Provider != "openai" {
		t.Errorf("call1 provider = %q, want openai", calls[1].Provider)
	}
}

// TestParseUIMessages_EntryTypeFallback verifies the discriminator
// fallback to `entry_type` / `type` for older schema variants.
func TestParseUIMessages_EntryTypeFallback(t *testing.T) {
	doc := `[
{"entry_type":"api_req_started","text":"{\"cost\":0.1,\"tokensIn\":10,\"apiProtocol\":\"anthropic\"}"},
{"type":"api_req_started","text":"{\"cost\":0.2,\"tokensIn\":20,\"apiProtocol\":\"openai\"}"}
]`
	calls, err := parseUIMessages([]byte(doc))
	if err != nil {
		t.Fatalf("parseUIMessages: %v", err)
	}
	if got, want := len(calls), 2; got != want {
		t.Fatalf("calls = %d, want %d", got, want)
	}
}

// TestParseUIMessages_NumericTimestamps verifies the ms-vs-s heuristic
// for the `ts` field by feeding both forms.
func TestParseUIMessages_NumericTimestamps(t *testing.T) {
	// 1716033600000 is ms epoch for 2024-05-18 12:00:00 UTC.
	doc := `[
{"say":"api_req_started","ts":1716033600000,"text":"{\"cost\":0.1,\"apiProtocol\":\"anthropic\"}"},
{"say":"api_req_started","ts":1716033600,"text":"{\"cost\":0.1,\"apiProtocol\":\"anthropic\"}"}
]`
	calls, err := parseUIMessages([]byte(doc))
	if err != nil {
		t.Fatalf("parseUIMessages: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if calls[0].Timestamp.Year() != 2024 {
		t.Errorf("ms ts year = %d, want 2024", calls[0].Timestamp.Year())
	}
	if calls[1].Timestamp.Year() != 2024 {
		t.Errorf("s ts year = %d, want 2024", calls[1].Timestamp.Year())
	}
}

// TestReadLastModelFromHistory_PrefersLastOccurrence ensures we surface
// the most recent <model> tag from the conversation file.
func TestReadLastModelFromHistory_PrefersLastOccurrence(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, APIConversationHistoryFile)
	body := `[{"content":"<model>old-model</model>"},{"content":"<model>new-model</model>"}]`
	if err := os.WriteFile(historyPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := readLastModelFromHistory(historyPath), "new-model"; got != want {
		t.Errorf("model = %q, want %q", got, want)
	}
}

// TestReadLastModelFromHistory_FallsBackToSlugAndName covers the two
// fallback tags we probe when no <model> is present.
func TestReadLastModelFromHistory_FallsBackToSlugAndName(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"slug-only", `[{"content":"<slug>my-slug</slug>"}]`, "my-slug"},
		{"name-only", `[{"content":"<name>My Model</name>"}]`, "My Model"},
		{"no-tags", `[{"content":"plain text"}]`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, APIConversationHistoryFile)
			if err := os.WriteFile(p, []byte(c.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := readLastModelFromHistory(p); got != c.want {
				t.Errorf("model = %q, want %q", got, c.want)
			}
		})
	}
}

// TestReadLastModelFromHistory_MissingFile returns the empty string.
func TestReadLastModelFromHistory_MissingFile(t *testing.T) {
	if got := readLastModelFromHistory(filepath.Join(t.TempDir(), "absent.json")); got != "" {
		t.Errorf("model = %q, want empty", got)
	}
}

// TestParseUIMessages_BOMTolerated ensures we strip a leading UTF-8 BOM
// before unmarshaling.
func TestParseUIMessages_BOMTolerated(t *testing.T) {
	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`[{"say":"api_req_started","text":"{\"cost\":0.1,\"apiProtocol\":\"anthropic\"}"}]`)...)
	calls, err := parseUIMessages(body)
	if err != nil {
		t.Fatalf("parseUIMessages with BOM: %v", err)
	}
	if len(calls) != 1 {
		t.Errorf("calls = %d, want 1", len(calls))
	}
}

// TestProviderFromProtocol_SplitsOnDelimiters covers the API protocol
// normalisation that produces the per-call Provider value.
func TestProviderFromProtocol_SplitsOnDelimiters(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"anthropic":         "anthropic",
		"bedrock/anthropic": "bedrock",
		"vertex:anthropic":  "vertex",
		"  openai  ":        "openai",
	}
	for in, want := range cases {
		if got := providerFromProtocol(in); got != want {
			t.Errorf("providerFromProtocol(%q) = %q, want %q", in, got, want)
		}
	}
}
