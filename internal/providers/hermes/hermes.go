// Package hermes implements a local-data provider that reads usage telemetry
// from the upstream Hermes Agent's SQLite state.db.
//
// The provider makes no network calls and requires no authentication. It
// reads sessions out of state.db using SQLite's read-only, immutable file
// URI so it never blocks the live agent. The schema documented here is
// derived from the upstream project's public source.
package hermes

import (
	"context"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// ID is the canonical provider identifier registered in the providers
// registry.
const ID = "hermes"

// DefaultAccountID is the account ID used by the auto-detector when it
// registers a local install.
const DefaultAccountID = "hermes"

const allTimeWindow = "all-time"

// Provider is a thin wrapper around providerbase.Base.
type Provider struct {
	providerbase.Base
	clock core.Clock
}

// New constructs a Hermes provider with sensible widget defaults.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Hermes Agent",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens", "cost_estimation"},
				DocURL:       "https://hermes-agent.nousresearch.com/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Hermes Agent and start at least one session so state.db is created.",
					"openusage auto-detects the database at ~/.hermes/state.db (or $HERMES_HOME/state.db); no configuration required.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
		clock: core.SystemClock{},
	}
}

// DetailWidget returns the standard coding-tool detail layout.
func (p *Provider) DetailWidget() core.DetailWidget {
	return detailWidget()
}

func (p *Provider) now() time.Time {
	if p != nil && p.clock != nil {
		return p.clock.Now()
	}
	return time.Now()
}

// HasChanged reports whether the state.db file has been modified since the
// given time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	dbPath := resolveDBPath(acct)
	if dbPath == "" {
		return false, nil
	}
	return shared.AnyPathModifiedAfter([]string{dbPath}, since), nil
}

// Fetch reads state.db (if present) and produces a UsageSnapshot.
//
// Missing-file is not an error: we return an OK-ish snapshot with no metrics
// and a friendly message so the dashboard shows the provider as
// detected-but-quiet rather than failing.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	if strings.TrimSpace(acct.Provider) == "" {
		acct.Provider = p.ID()
	}

	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.Timestamp = p.now()
	snap.DailySeries = make(map[string][]core.TimePoint)

	dbPath := resolveDBPath(acct)
	if dbPath == "" {
		snap.Status = core.StatusUnknown
		snap.Message = "Hermes state.db not found"
		return snap, nil
	}
	snap.Raw["db_path"] = dbPath

	sessions, err := queryHermesSessions(ctx, dbPath)
	if err != nil {
		snap.SetDiagnostic("query_error", err.Error())
		snap.Status = core.StatusError
		snap.Message = "Failed to read Hermes state.db"
		return snap, err
	}

	if len(sessions) == 0 {
		snap.Status = core.StatusOK
		snap.Message = "No Hermes sessions recorded"
		return snap, nil
	}

	populateSnapshot(&snap, sessions, p.now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(snap)
	return snap, nil
}

// populateSnapshot aggregates the per-session records into snapshot
// metrics, per-model usage records, and daily series.
func populateSnapshot(snap *core.UsageSnapshot, sessions []hermesSession, now time.Time) {
	type modelTotals struct {
		input      int64
		output     int64
		cacheRead  int64
		cacheWrite int64
		reasoning  int64
		cost       float64
		hasCost    bool
		sessions   int64
		messages   int64
	}

	perModel := make(map[string]*modelTotals)
	perProvider := make(map[string]string)

	var (
		totalInput      int64
		totalOutput     int64
		totalCacheRead  int64
		totalCacheWrite int64
		totalReasoning  int64
		totalMessages   int64
		totalCost       float64
		hasAnyCost      bool
	)

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	var sessionsToday, sessions7d int64
	sessionsByDay := make(map[string]float64)
	tokensByDay := make(map[string]float64)
	costByDay := make(map[string]float64)

	for _, s := range sessions {
		bucket, ok := perModel[s.Model]
		if !ok {
			bucket = &modelTotals{}
			perModel[s.Model] = bucket
		}
		bucket.input += s.InputTokens
		bucket.output += s.OutputTokens
		bucket.cacheRead += s.CacheReadTokens
		bucket.cacheWrite += s.CacheWriteTok
		bucket.reasoning += s.ReasoningTokens
		bucket.messages += s.MessageCount
		bucket.sessions++
		if s.HasCost {
			bucket.cost += s.CostUSD
			bucket.hasCost = true
		}
		if perProvider[s.Model] == "" && s.Provider != "" {
			perProvider[s.Model] = s.Provider
		}

		totalInput += s.InputTokens
		totalOutput += s.OutputTokens
		totalCacheRead += s.CacheReadTokens
		totalCacheWrite += s.CacheWriteTok
		totalReasoning += s.ReasoningTokens
		totalMessages += s.MessageCount
		if s.HasCost {
			totalCost += s.CostUSD
			hasAnyCost = true
		}

		day := s.StartedAt.UTC().Format("2006-01-02")
		sessionsByDay[day]++
		tokensByDay[day] += float64(s.InputTokens + s.OutputTokens + s.ReasoningTokens)
		if s.HasCost {
			costByDay[day] += s.CostUSD
		}
		if day == today {
			sessionsToday++
		}
		if !s.StartedAt.Before(cutoff7d) {
			sessions7d++
		}
	}

	totalTokens := totalInput + totalOutput + totalReasoning

	setUsedMetric(snap, "total_sessions", float64(len(sessions)), "sessions", allTimeWindow)
	setUsedMetric(snap, "sessions_today", float64(sessionsToday), "sessions", "today")
	setUsedMetric(snap, "sessions_7d", float64(sessions7d), "sessions", "7d")
	setUsedMetric(snap, "total_tokens", float64(totalTokens), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_read", float64(totalCacheRead), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_write", float64(totalCacheWrite), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_reasoning_tokens", float64(totalReasoning), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_messages", float64(totalMessages), "messages", allTimeWindow)
	if hasAnyCost {
		setUsedMetric(snap, "total_cost_usd", totalCost, "USD", allTimeWindow)
	}

	if len(sessionsByDay) > 0 {
		snap.DailySeries["sessions"] = core.SortedTimePoints(sessionsByDay)
	}
	if len(tokensByDay) > 0 {
		snap.DailySeries["tokens"] = core.SortedTimePoints(tokensByDay)
	}
	if len(costByDay) > 0 {
		snap.DailySeries["cost"] = core.SortedTimePoints(costByDay)
	}

	for model, bucket := range perModel {
		rec := core.ModelUsageRecord{
			RawModelID:      model,
			RawSource:       "sqlite",
			Window:          allTimeWindow,
			InputTokens:     core.Float64Ptr(float64(bucket.input)),
			OutputTokens:    core.Float64Ptr(float64(bucket.output)),
			CachedTokens:    core.Float64Ptr(float64(bucket.cacheRead)),
			ReasoningTokens: core.Float64Ptr(float64(bucket.reasoning)),
			TotalTokens:     core.Float64Ptr(float64(bucket.input + bucket.output + bucket.cacheRead + bucket.cacheWrite + bucket.reasoning)),
			Requests:        core.Float64Ptr(float64(bucket.sessions)),
		}
		if bucket.hasCost {
			rec.CostUSD = core.Float64Ptr(bucket.cost)
		}
		if hint := perProvider[model]; hint != "" {
			rec.SetDimension("upstream_provider", hint)
		}
		snap.AppendModelUsage(rec)
	}
}

func buildStatusMessage(snap core.UsageSnapshot) string {
	parts := make([]string, 0, 3)
	if m, ok := snap.Metrics["total_sessions"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCount(*m.Used, "session"))
	}
	if m, ok := snap.Metrics["total_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, shared.FormatTokenCount(int(*m.Used))+" tokens")
	}
	if m, ok := snap.Metrics["total_cost_usd"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCostUSD(*m.Used))
	}
	if len(parts) == 0 {
		return "OK"
	}
	return strings.Join(parts, ", ")
}

func setUsedMetric(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	if value <= 0 {
		return
	}
	v := value
	snap.Metrics[key] = core.Metric{
		Used:   &v,
		Unit:   unit,
		Window: window,
	}
}

func formatCount(v float64, noun string) string {
	if v == 1 {
		return "1 " + noun
	}
	return shared.FormatTokenCount(int(v)) + " " + noun + "s"
}

func formatCostUSD(v float64) string {
	if v >= 1 {
		return fmt.Sprintf("$%.2f", v)
	}
	return fmt.Sprintf("$%.4f", v)
}
