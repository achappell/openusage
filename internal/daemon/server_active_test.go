package daemon

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/active"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func TestActiveSelectsMostRecentEventProvider(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
		core.AccountConfig{ID: "claude-default", Provider: "claude_code"},
	)
	srv := newActiveTestService(t)
	ctx := context.Background()

	seedActiveEvent(t, srv, "codex", "codex-default", time.Now().Add(-2*time.Hour), "codex-old")
	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now().Add(-5*time.Minute), "claude-new")

	sel, err := srv.computeActive(ctx)
	if err != nil {
		t.Fatalf("computeActive: %v", err)
	}
	if sel.Selected != "claude_code:claude-default" {
		t.Errorf("selected = %q, want claude_code:claude-default", sel.Selected)
	}
	if sel.Source != "events" {
		t.Errorf("source = %q, want events", sel.Source)
	}
	if sel.Status != "ok" {
		t.Errorf("status = %q, want ok", sel.Status)
	}
}

func TestActivePinHoldsThenAutoReleases(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
		core.AccountConfig{ID: "claude-default", Provider: "claude_code"},
	)
	srv := newActiveTestService(t)
	ctx := context.Background()

	seedActiveEvent(t, srv, "codex", "codex-default", time.Now().Add(-2*time.Hour), "codex-old")
	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now().Add(-1*time.Hour), "claude-old")

	if err := srv.setPin(ctx, "codex:codex-default"); err != nil {
		t.Fatalf("setPin: %v", err)
	}
	sel, err := srv.computeActive(ctx)
	if err != nil {
		t.Fatalf("computeActive while pinned: %v", err)
	}
	if sel.Selected != "codex:codex-default" || !sel.Pinned {
		t.Fatalf("pin not honored: %+v", sel)
	}

	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now().Add(1*time.Minute), "claude-new")
	sel, err = srv.computeActive(ctx)
	if err != nil {
		t.Fatalf("computeActive after release: %v", err)
	}
	if sel.Pinned {
		t.Errorf("pin should have auto-released: %+v", sel)
	}
	if sel.Selected != "claude_code:claude-default" {
		t.Errorf("selected = %q, want claude_code:claude-default", sel.Selected)
	}

	raw, _, err := srv.store.MetaGet(ctx, active.PinMetaKey)
	if err != nil {
		t.Fatalf("MetaGet after release: %v", err)
	}
	if raw != "" {
		t.Errorf("released pin persisted as %q, want empty", raw)
	}
}

func TestActiveNoDataStatus(t *testing.T) {
	configureActiveTestConfig(t)
	srv := newActiveTestService(t)

	sel, err := srv.computeActive(context.Background())
	if err != nil {
		t.Fatalf("computeActive: %v", err)
	}
	if sel.Status != "no_data" {
		t.Errorf("status = %q, want no_data", sel.Status)
	}
}

func TestActiveConfiguredWithoutEventsUsesPriorityFallback(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "openai-default", Provider: "openai"},
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
	)
	srv := newActiveTestService(t)

	sel, err := srv.computeActive(context.Background())
	if err != nil {
		t.Fatalf("computeActive: %v", err)
	}
	if sel.Selected != "codex:codex-default" {
		t.Errorf("selected = %q, want codex:codex-default", sel.Selected)
	}
	if sel.Source != "local" {
		t.Errorf("source = %q, want local", sel.Source)
	}
}

func TestPinSurvivesRestart(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
	)
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	first := openActiveTestService(t, dbPath)
	if err := first.setPin(context.Background(), "codex:codex-default"); err != nil {
		t.Fatalf("setPin: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first service: %v", err)
	}

	second := openActiveTestService(t, dbPath)
	t.Cleanup(func() { _ = second.Close() })
	raw, _, err := second.store.MetaGet(context.Background(), active.PinMetaKey)
	if err != nil {
		t.Fatalf("MetaGet: %v", err)
	}
	state, err := active.DecodePinState(raw)
	if err != nil {
		t.Fatalf("DecodePinState: %v", err)
	}
	if state.Key != "codex:codex-default" {
		t.Errorf("pin after restart = %q, want codex:codex-default", state.Key)
	}
}

func TestActivePinReleasesWhenProviderDisappears(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
		core.AccountConfig{ID: "claude-default", Provider: "claude_code"},
	)
	srv := newActiveTestService(t)
	ctx := context.Background()
	if err := srv.setPin(ctx, "codex:codex-default"); err != nil {
		t.Fatalf("setPin: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load active test config: %v", err)
	}
	cfg.Accounts = []core.AccountConfig{{ID: "claude-default", Provider: "claude_code"}}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save updated active test config: %v", err)
	}
	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now(), "claude-after-disappear")

	sel, err := srv.computeActive(ctx)
	if err != nil {
		t.Fatalf("computeActive: %v", err)
	}
	if sel.Pinned {
		t.Fatalf("missing provider pin still active: %+v", sel)
	}
	if sel.Selected != "claude_code:claude-default" {
		t.Errorf("selected = %q, want claude_code:claude-default", sel.Selected)
	}
	raw, _, err := srv.store.MetaGet(ctx, active.PinMetaKey)
	if err != nil {
		t.Fatalf("MetaGet after missing provider release: %v", err)
	}
	if raw != "" {
		t.Errorf("missing provider pin persisted as %q, want empty", raw)
	}
}

func TestComputeActiveConcurrent(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
		core.AccountConfig{ID: "claude-default", Provider: "claude_code"},
	)
	srv := newActiveTestService(t)
	ctx := context.Background()
	seedActiveEvent(t, srv, "codex", "codex-default", time.Now().Add(-2*time.Hour), "codex-old")
	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now().Add(-1*time.Hour), "claude-old")
	if err := srv.setPin(ctx, "codex:codex-default"); err != nil {
		t.Fatalf("setPin: %v", err)
	}
	seedActiveEvent(t, srv, "claude_code", "claude-default", time.Now().Add(time.Minute), "claude-new")

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := srv.computeActive(ctx); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent computeActive: %v", err)
	}
}

func TestActiveHTTPAPI(t *testing.T) {
	configureActiveTestConfig(t,
		core.AccountConfig{ID: "codex-default", Provider: "codex"},
	)
	srv := newActiveTestService(t)
	seedActiveEvent(t, srv, "codex", "codex-default", time.Now(), "codex-api")

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/active", srv.handleActive)
	mux.HandleFunc("/v1/active/explain", srv.handleActiveExplain)
	mux.HandleFunc("/v1/active/pin", srv.handleActivePin)
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "active.sock"))
	if err != nil {
		t.Fatalf("listen active test socket: %v", err)
	}
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})

	client := NewClient(listener.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sel, err := client.Active(ctx)
	if err != nil {
		t.Fatalf("client Active: %v", err)
	}
	if sel.Selected != "codex:codex-default" || sel.Source != "events" {
		t.Fatalf("active selection = %+v", sel)
	}

	if err := client.SetPin(ctx, "codex:codex-default"); err != nil {
		t.Fatalf("client SetPin: %v", err)
	}
	sel, err = client.Active(ctx)
	if err != nil {
		t.Fatalf("client Active after pin: %v", err)
	}
	if !sel.Pinned || sel.Source != "pinned" {
		t.Fatalf("pinned selection = %+v", sel)
	}

	explanation, err := client.ActiveExplain(ctx)
	if err != nil {
		t.Fatalf("client ActiveExplain: %v", err)
	}
	if !strings.Contains(explanation, "pinned") {
		t.Errorf("explanation = %q, want pinned decision", explanation)
	}

	if err := client.SetPin(ctx, ""); err != nil {
		t.Fatalf("client clear pin: %v", err)
	}
}

func configureActiveTestConfig(t *testing.T, accounts ...core.AccountConfig) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.AutoDetect = false
	cfg.Accounts = accounts
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save active test config: %v", err)
	}
}

func newActiveTestService(t *testing.T) *Service {
	t.Helper()
	return openActiveTestService(t, filepath.Join(t.TempDir(), "telemetry.db"))
}

func openActiveTestService(t *testing.T, dbPath string) *Service {
	t.Helper()
	store, err := telemetry.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("open active test store: %v", err)
	}
	srv := &Service{
		cfg:     Config{DBPath: dbPath},
		store:   store,
		rmCache: newReadModelCache(),
	}
	return srv
}

func seedActiveEvent(t *testing.T, srv *Service, providerID, accountID string, occurredAt time.Time, messageID string) {
	t.Helper()
	_, err := srv.store.Ingest(context.Background(), telemetry.IngestRequest{
		SourceSystem:        telemetry.SourceSystem(providerID),
		SourceChannel:       telemetry.SourceChannelHook,
		SourceSchemaVersion: "v1",
		OccurredAt:          occurredAt,
		ProviderID:          providerID,
		AccountID:           accountID,
		AgentName:           providerID,
		EventType:           telemetry.EventTypeMessageUsage,
		MessageID:           messageID,
		Status:              telemetry.EventStatusOK,
	})
	if err != nil {
		t.Fatalf("seed %s event: %v", providerID, err)
	}
}
