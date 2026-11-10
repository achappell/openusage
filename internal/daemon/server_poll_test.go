package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func TestPollSnapshotsForIngestExcludesUnchangedCachedResults(t *testing.T) {
	results := []pollProviderResult{
		{
			accountID:    "cached",
			snapshot:     core.UsageSnapshot{ProviderID: "codex", AccountID: "cached"},
			shouldIngest: false,
		},
		{
			accountID:    "changed",
			snapshot:     core.UsageSnapshot{ProviderID: "openai", AccountID: "changed"},
			shouldIngest: true,
		},
	}

	got := pollSnapshotsForIngest(results)
	if _, ok := got["cached"]; ok {
		t.Fatal("unchanged cached snapshot should not be re-ingested")
	}
	if _, ok := got["changed"]; !ok {
		t.Fatal("changed snapshot should be ingested")
	}
}

type pollingTestProvider struct {
	core.UsageProvider
	snapshots []core.UsageSnapshot
	fetches   int
	detects   int
}

func (p *pollingTestProvider) Fetch(_ context.Context, _ core.AccountConfig) (core.UsageSnapshot, error) {
	snap := p.snapshots[p.fetches]
	p.fetches++
	return snap, nil
}

func (p *pollingTestProvider) HasChanged(_ core.AccountConfig, _ time.Time) (bool, error) {
	p.detects++
	return false, nil
}

func TestClaudePollFetchesUnchangedLocalDataAndPersistsFreshObservations(t *testing.T) {
	configureActiveTestConfig(t, core.AccountConfig{ID: "claude", Provider: "claude_code"})
	store, err := telemetry.OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := time.Date(2026, 11, 9, 12, 0, 0, 100, time.UTC)
	reset := observed.Add(time.Hour)
	snap := core.UsageSnapshot{
		ProviderID: "claude_code", AccountID: "claude", Status: core.StatusOK,
		Timestamp: observed, Metrics: map[string]core.Metric{"usage_five_hour": {Used: ptr(20)}},
		Resets:     map[string]time.Time{"five_hour": reset},
		Attributes: map[string]string{"usage_api_observed_at": observed.Format(time.RFC3339Nano)},
	}
	second := snap
	second.Timestamp = observed.Add(time.Second)
	second.Attributes = map[string]string{"usage_api_observed_at": second.Timestamp.Format(time.RFC3339Nano)}
	second.Resets = map[string]time.Time{"five_hour": reset.Add(time.Hour)}
	cached := second
	cached.Attributes = nil
	p := &pollingTestProvider{snapshots: []core.UsageSnapshot{snap, second, cached}}
	s := &Service{
		quotaIngest:   telemetry.NewQuotaSnapshotIngestor(store),
		providerByID:  map[string]core.UsageProvider{"claude_code": p},
		pollScheduler: newPollScheduler(0), pollState: make(map[string]*providerPollState),
	}
	s.pollProviders(context.Background())
	s.pollScheduler.states["claude"].lastPollAt = time.Now().Add(-time.Minute)
	s.pollProviders(context.Background())
	s.pollScheduler.states["claude"].lastPollAt = time.Now().Add(-time.Minute)
	s.pollProviders(context.Background())
	if p.fetches != 3 || p.detects != 0 {
		t.Fatalf("fetches=%d detector calls=%d, want 3 and 0", p.fetches, p.detects)
	}
	if s.dataVersion.Load() != 2 || s.pollState["claude"].needsIngest {
		t.Fatalf("fresh unchanged quota not ingested: version=%d state=%+v", s.dataVersion.Load(), s.pollState["claude"])
	}
}

func TestPollKeepsLocalChangeDetectionForOtherProviders(t *testing.T) {
	configureActiveTestConfig(t, core.AccountConfig{ID: "other", Provider: "other"})
	store, err := telemetry.OpenStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := &pollingTestProvider{snapshots: []core.UsageSnapshot{{
		ProviderID: "other", AccountID: "other", Timestamp: time.Now().UTC(), Status: core.StatusOK,
	}}}
	s := &Service{
		quotaIngest:   telemetry.NewQuotaSnapshotIngestor(store),
		providerByID:  map[string]core.UsageProvider{"other": p},
		pollScheduler: newPollScheduler(0), pollState: make(map[string]*providerPollState),
	}
	s.pollProviders(context.Background())
	s.pollProviders(context.Background())
	if p.fetches != 1 || p.detects != 1 || s.dataVersion.Load() != 1 {
		t.Fatalf("fetches=%d detector calls=%d version=%d", p.fetches, p.detects, s.dataVersion.Load())
	}
}

func TestPollRetriesFailedIngestWithoutRefetch(t *testing.T) {
	configureActiveTestConfig(t, core.AccountConfig{ID: "claude", Provider: "claude_code"})
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := telemetry.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	p := &pollingTestProvider{snapshots: []core.UsageSnapshot{{
		ProviderID: "claude_code", AccountID: "claude", Timestamp: now, Status: core.StatusOK,
		Attributes: map[string]string{"usage_api_observed_at": now.Format(time.RFC3339Nano)},
	}}}
	s := &Service{
		quotaIngest:   telemetry.NewQuotaSnapshotIngestor(store),
		providerByID:  map[string]core.UsageProvider{"claude_code": p},
		pollScheduler: newPollScheduler(time.Hour), pollState: make(map[string]*providerPollState),
	}
	s.pollProviders(context.Background())
	if !s.pollState["claude"].needsIngest || s.dataVersion.Load() != 0 {
		t.Fatal("failed ingest was not retained as pending")
	}
	store, err = telemetry.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s.quotaIngest = telemetry.NewQuotaSnapshotIngestor(store)
	s.pollProviders(context.Background())
	if p.fetches != 1 || s.pollState["claude"].needsIngest || s.dataVersion.Load() != 1 {
		t.Fatalf("retry: fetches=%d pending=%t version=%d", p.fetches, s.pollState["claude"].needsIngest, s.dataVersion.Load())
	}
}
