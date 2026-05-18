package roocode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

// TestProvider_BasicMetadata sanity-checks the Provider's ID, Describe
// output, and ProviderSpec shape — these are wired into the registry and
// drive the dashboard layout, so the smoke test catches regressions.
func TestProvider_BasicMetadata(t *testing.T) {
	p := New()
	if got, want := p.ID(), ID; got != want {
		t.Errorf("ID = %q, want %q", got, want)
	}
	info := p.Describe()
	if info.Name == "" {
		t.Error("Describe().Name is empty")
	}
	spec := p.Spec()
	if spec.Auth.Type != core.ProviderAuthTypeLocal {
		t.Errorf("auth type = %v, want local", spec.Auth.Type)
	}
	if p.DashboardWidget().IsZero() {
		t.Error("DashboardWidget is zero")
	}
}

// TestProvider_Fetch_NoData ensures a workstation with no extension
// install returns an UNKNOWN snapshot rather than an error.
func TestProvider_Fetch_NoData(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	p := New()
	p.clock = fixedClock{t: time.Date(2025, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "roocode", Provider: "roocode", Auth: "local"}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("status = %v, want UNKNOWN", snap.Status)
	}
	if len(snap.Metrics) != 0 {
		t.Errorf("metrics = %v, want empty", snap.Metrics)
	}
}

// TestProvider_Fetch_OverrideTasksDir uses a synthetic tasks directory to
// drive Fetch end-to-end. We bypass auto-discovery by setting a per-account
// `tasks_dir` path hint pointing at our fixture-shaped temp tree.
func TestProvider_Fetch_OverrideTasksDir(t *testing.T) {
	tasksRoot := t.TempDir()

	// One real task with two valid api_req_started entries and a model tag.
	taskDir := filepath.Join(tasksRoot, "task-abc")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	uiPath := filepath.Join(taskDir, UIMessagesFile)
	if err := os.WriteFile(uiPath, []byte(`[
{"say":"api_req_started","ts":1716033600000,"text":"{\"cost\":0.01,\"tokensIn\":100,\"tokensOut\":50,\"cacheReads\":10,\"cacheWrites\":5,\"apiProtocol\":\"anthropic\"}"},
{"say":"api_req_started","ts":1716033601000,"text":"{\"cost\":0.02,\"tokensIn\":200,\"tokensOut\":100,\"apiProtocol\":\"anthropic\"}"}
]`), 0o600); err != nil {
		t.Fatal(err)
	}
	historyPath := filepath.Join(taskDir, APIConversationHistoryFile)
	if err := os.WriteFile(historyPath, []byte(`[{"content":"<model>claude-sonnet-4-5</model>"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	// An incomplete task with no ui_messages.json must be silently skipped.
	if err := os.MkdirAll(filepath.Join(tasksRoot, "task-empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	p := New()
	p.clock = fixedClock{t: time.Date(2024, 5, 19, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "roocode", Provider: "roocode", Auth: "local"}
	acct.SetPath("tasks_dir", tasksRoot)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v (msg=%q), want OK", snap.Status, snap.Message)
	}

	expect := map[string]float64{
		"total_tasks":              1,
		"total_requests":           2,
		"total_input_tokens":       300,
		"total_output_tokens":      150,
		"total_cache_read_tokens":  10,
		"total_cache_write_tokens": 5,
		"total_tokens":             465,
		"total_cost_usd":           0.03,
	}
	for key, want := range expect {
		m, ok := snap.Metrics[key]
		if !ok {
			t.Errorf("missing metric %s", key)
			continue
		}
		if m.Used == nil || floatNotEqual(*m.Used, want, 1e-9) {
			got := -1.0
			if m.Used != nil {
				got = *m.Used
			}
			t.Errorf("metric %s = %v, want %v", key, got, want)
		}
	}

	if got, want := len(snap.ModelUsage), 1; got != want {
		t.Fatalf("ModelUsage = %d, want %d", got, want)
	}
	rec := snap.ModelUsage[0]
	if rec.RawModelID != "claude-sonnet-4-5" {
		t.Errorf("model = %q, want claude-sonnet-4-5", rec.RawModelID)
	}
	if rec.Requests == nil || *rec.Requests != 2 {
		t.Errorf("requests = %v, want 2", rec.Requests)
	}
	if rec.Dimensions["upstream_provider"] != "anthropic" {
		t.Errorf("upstream_provider = %q, want anthropic", rec.Dimensions["upstream_provider"])
	}
}

// TestProvider_Fetch_MalformedTaskSkipped guards against a single broken
// task tanking the entire Fetch. We write one valid + one corrupt task
// and assert Fetch returns OK with the valid task's metrics.
func TestProvider_Fetch_MalformedTaskSkipped(t *testing.T) {
	tasksRoot := t.TempDir()

	// Valid task.
	good := filepath.Join(tasksRoot, "task-good")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, UIMessagesFile),
		[]byte(`[{"say":"api_req_started","ts":1716033600000,"text":"{\"cost\":0.5,\"tokensIn\":10,\"tokensOut\":5,\"apiProtocol\":\"openai\"}"}]`),
		0o600); err != nil {
		t.Fatal(err)
	}

	// Corrupt task — invalid top-level JSON.
	bad := filepath.Join(tasksRoot, "task-bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, UIMessagesFile),
		[]byte(`{this is not valid json`),
		0o600); err != nil {
		t.Fatal(err)
	}

	p := New()
	p.clock = fixedClock{t: time.Date(2024, 5, 19, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "roocode", Provider: "roocode", Auth: "local"}
	acct.SetPath("tasks_dir", tasksRoot)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("status = %v, want OK", snap.Status)
	}
	if m := snap.Metrics["total_tasks"]; m.Used == nil || *m.Used != 1 {
		t.Errorf("total_tasks = %v, want 1 (corrupt task should be skipped)", m.Used)
	}
	if snap.Diagnostics["roocode_task_parse_errors"] == "" {
		t.Error("expected roocode_task_parse_errors diagnostic")
	}
}

func floatNotEqual(a, b, epsilon float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d > epsilon
}
