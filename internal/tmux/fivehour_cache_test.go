package tmux

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/claude_code"
)

func setTempHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"oauthAccount":{"accountUuid":"user-id","organizationUuid":"org-id"}}`), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileFiveHourScopedFreshCache(t *testing.T) {
	setTempHome(t)
	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(time.Hour)
	claude_code.WriteFiveHourCache("a", 13, now, reset)
	c := &Context{Now: now, Provider: "claude_code", Snapshot: core.UsageSnapshot{ProviderID: "claude_code", AccountID: "a", Status: core.StatusOK, Attributes: map[string]string{"usage_api_observed_at": now.Format(time.RFC3339Nano)}}}
	reconcileFiveHourUsage(c)
	if got := c.Snapshot.Metrics["usage_five_hour"].Used; got == nil || *got != 13 {
		t.Fatalf("expected scoped cached quota, got %v", got)
	}
}

func TestReconcileFiveHourRejectsFailureStaleAndOtherAccount(t *testing.T) {
	setTempHome(t)
	now := time.Now().UTC().Truncate(time.Second)
	claude_code.WriteFiveHourCache("a", 13, now, now.Add(time.Hour))
	for _, tc := range []struct {
		account  string
		status   core.Status
		observed time.Time
	}{
		{"b", core.StatusOK, now}, {"a", core.StatusError, now}, {"a", core.StatusOK, now.Add(-11 * time.Minute)}, {"", core.StatusOK, now},
	} {
		c := &Context{Now: now, Provider: "claude_code", Snapshot: core.UsageSnapshot{ProviderID: "claude_code", AccountID: tc.account, Status: tc.status, Attributes: map[string]string{"usage_api_observed_at": tc.observed.Format(time.RFC3339Nano)}}}
		reconcileFiveHourUsage(c)
		if _, ok := c.Snapshot.Metrics["usage_five_hour"]; ok {
			t.Fatalf("injected unsafe cached quota: %+v", tc)
		}
	}
}

func TestReconcileFiveHourDropsExpiredSnapshot(t *testing.T) {
	now := time.Now().UTC()
	c := &Context{Now: now, Provider: "claude_code", Snapshot: core.UsageSnapshot{
		ProviderID: "claude_code", AccountID: "a", Status: core.StatusOK,
		Attributes: map[string]string{"usage_api_observed_at": now.Format(time.RFC3339Nano)},
		Metrics:    map[string]core.Metric{"usage_five_hour": {Used: new(100.0), Unit: "%", Window: "5h"}},
		Resets:     map[string]time.Time{"usage_five_hour": now},
	}}
	reconcileFiveHourUsage(c)
	if _, ok := c.Snapshot.Metrics["usage_five_hour"]; ok {
		t.Fatal("expired quota retained")
	}
}
