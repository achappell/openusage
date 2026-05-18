package roocode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

// ID is the canonical provider identifier registered in the providers
// registry.
const ID = "roocode"

// DefaultAccountID is the account ID used by the auto-detector when it
// registers a local Roo Code install.
const DefaultAccountID = "roocode"

const allTimeWindow = "all-time"

// Provider implements core.UsageProvider for Roo Code. It reads per-task
// JSON event logs from the VS Code globalStorage subdirectory the
// extension writes to. No network calls or auth required.
type Provider struct {
	providerbase.Base
	clock core.Clock
}

// New constructs a Roo Code provider with default widget metadata.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Roo Code",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens", "cost_estimation"},
				DocURL:       "https://github.com/RooCodeInc/Roo-Code",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install the Roo Code VS Code extension and run at least one task.",
					"openusage discovers the extension's task logs from VS Code globalStorage; no configuration required.",
				},
			},
			Dashboard: dashboardWidget(core.DashboardColorRolePeach),
		}),
		clock: core.SystemClock{},
	}
}

// DetailWidget returns the standard coding-tool detail layout.
func (p *Provider) DetailWidget() core.DetailWidget {
	return detailWidget()
}

// HasChanged stat()s the per-task root for the Roo Code extension and
// returns true if its mtime is after `since`. The directory mtime ticks
// whenever a new task is added or an existing task's ui_messages.json is
// rewritten, which is the only signal we care about for re-polling.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	return ExtensionChanged(RooExtensionSubdir, since), nil
}

// Fetch enumerates per-task directories for the Roo Code extension across
// every detected VS Code variant, parses each one, dedups across variants,
// and writes the aggregate usage into a UsageSnapshot.
//
// Missing extension data is not an error: we emit a friendly "no data"
// snapshot so the tile shows the provider as detected-but-quiet.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	clock := p.clock
	if clock == nil {
		clock = core.SystemClock{}
	}
	return FetchExtension(ctx, p.ID(), acct, RooExtensionSubdir, ClientRooCode, "Roo Code", clock)
}

// FetchExtension is the shared Fetch implementation Roo Code and Kilo Code
// both use. It enumerates the per-task subdirectories under the given
// extension's globalStorage path across every VS Code variant we know
// about, parses each task with ParseTaskDir, dedups cross-variant
// duplicates, and aggregates the result into a UsageSnapshot.
//
// Exposed as exported so sibling provider packages (currently
// internal/providers/kilocode) can call into it without duplicating the
// aggregation logic.
func FetchExtension(ctx context.Context, providerID string, acct core.AccountConfig, extensionSubdir, clientID, displayName string, clock core.Clock) (core.UsageSnapshot, error) {
	if clock == nil {
		clock = core.SystemClock{}
	}
	if strings.TrimSpace(acct.Provider) == "" {
		acct.Provider = providerID
	}

	snap := core.NewUsageSnapshot(providerID, acct.ID)
	snap.Timestamp = clock.Now()
	snap.DailySeries = make(map[string][]core.TimePoint)

	taskDirs := resolveTaskDirs(acct, extensionSubdir)
	if len(taskDirs) == 0 {
		snap.Status = core.StatusUnknown
		snap.Message = fmt.Sprintf("%s extension data not found", displayName)
		return snap, nil
	}
	snap.Raw["task_count_raw"] = fmt.Sprintf("%d", len(taskDirs))

	var allCalls []APICall
	var parsedTaskIDs = make(map[string]struct{}, len(taskDirs))
	var parseErrors int
	for _, taskDir := range taskDirs {
		if err := ctx.Err(); err != nil {
			return snap, err
		}
		evt, err := ParseTaskDir(taskDir, clientID)
		if err != nil {
			if IsNoUIMessages(err) {
				continue
			}
			parseErrors++
			continue
		}
		if evt == nil || len(evt.Calls) == 0 {
			continue
		}
		parsedTaskIDs[evt.TaskID] = struct{}{}
		allCalls = append(allCalls, evt.Calls...)
	}
	if parseErrors > 0 {
		snap.SetDiagnostic("roocode_task_parse_errors", fmt.Sprintf("%d", parseErrors))
	}

	if len(allCalls) == 0 {
		snap.Status = core.StatusOK
		snap.Message = fmt.Sprintf("No %s usage recorded yet", displayName)
		return snap, nil
	}

	deduped := Dedup(allCalls)
	populateSnapshot(&snap, deduped, parsedTaskIDs, clock.Now())
	snap.Status = core.StatusOK
	snap.Message = buildStatusMessage(displayName, snap)
	return snap, nil
}

// populateSnapshot aggregates per-call data into the snapshot's Metrics,
// ModelUsage, and DailySeries fields. Pure function so it's trivially
// unit-testable from the per-provider tests.
func populateSnapshot(snap *core.UsageSnapshot, calls []APICall, taskIDs map[string]struct{}, now time.Time) {
	type modelTotals struct {
		input      int64
		output     int64
		cacheRead  int64
		cacheWrite int64
		cost       float64
		hasCost    bool
		requests   int64
		provider   string
	}

	perModel := make(map[string]*modelTotals)

	var (
		totalInput    int64
		totalOutput   int64
		totalCacheRd  int64
		totalCacheWr  int64
		totalCost     float64
		totalRequests int64
		todayCost     float64
	)

	today := now.UTC().Format("2006-01-02")
	cutoff7d := now.UTC().AddDate(0, 0, -7)
	tasksByDay := make(map[string]map[string]struct{}) // day -> task-id set
	tokensByDay := make(map[string]float64)
	costByDay := make(map[string]float64)

	tasksToday := make(map[string]struct{})
	tasks7d := make(map[string]struct{})

	for _, c := range calls {
		totalInput += c.TokensIn
		totalOutput += c.TokensOut
		totalCacheRd += c.CacheReads
		totalCacheWr += c.CacheWrites
		totalCost += c.Cost
		totalRequests++

		modelKey := strings.TrimSpace(c.Model)
		if modelKey == "" {
			modelKey = "unknown"
		}
		bucket := perModel[modelKey]
		if bucket == nil {
			bucket = &modelTotals{}
			perModel[modelKey] = bucket
		}
		bucket.input += c.TokensIn
		bucket.output += c.TokensOut
		bucket.cacheRead += c.CacheReads
		bucket.cacheWrite += c.CacheWrites
		if c.Cost > 0 {
			bucket.cost += c.Cost
			bucket.hasCost = true
		}
		bucket.requests++
		if bucket.provider == "" && c.Provider != "" {
			bucket.provider = c.Provider
		}

		if c.Timestamp.IsZero() {
			continue
		}
		day := c.Timestamp.UTC().Format("2006-01-02")
		if tasksByDay[day] == nil {
			tasksByDay[day] = make(map[string]struct{})
		}
		if c.TaskID != "" {
			tasksByDay[day][c.TaskID] = struct{}{}
		}
		tokensByDay[day] += float64(c.TokensIn + c.TokensOut)
		costByDay[day] += c.Cost
		if day == today {
			todayCost += c.Cost
			if c.TaskID != "" {
				tasksToday[c.TaskID] = struct{}{}
			}
		}
		if !c.Timestamp.UTC().Before(cutoff7d) && c.TaskID != "" {
			tasks7d[c.TaskID] = struct{}{}
		}
	}

	totalTokens := totalInput + totalOutput + totalCacheRd + totalCacheWr

	setUsedMetric(snap, "total_tasks", float64(len(taskIDs)), "tasks", allTimeWindow)
	setUsedMetric(snap, "tasks_today", float64(len(tasksToday)), "tasks", "today")
	setUsedMetric(snap, "tasks_7d", float64(len(tasks7d)), "tasks", "7d")
	setUsedMetric(snap, "total_requests", float64(totalRequests), "requests", allTimeWindow)
	setUsedMetric(snap, "total_tokens", float64(totalTokens), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_input_tokens", float64(totalInput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_output_tokens", float64(totalOutput), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_read_tokens", float64(totalCacheRd), "tokens", allTimeWindow)
	setUsedMetric(snap, "total_cache_write_tokens", float64(totalCacheWr), "tokens", allTimeWindow)
	if totalCost > 0 {
		setUsedMetric(snap, "total_cost_usd", totalCost, "USD", allTimeWindow)
	}
	if todayCost > 0 {
		setUsedMetric(snap, "today_cost_usd", todayCost, "USD", "today")
	}

	// Daily series. Tasks/day is the cardinality of the per-day task set so
	// repeated calls in the same task don't inflate "tasks".
	if len(tasksByDay) > 0 {
		points := make(map[string]float64, len(tasksByDay))
		for day, set := range tasksByDay {
			points[day] = float64(len(set))
		}
		snap.DailySeries["tasks"] = core.SortedTimePoints(points)
	}
	if len(tokensByDay) > 0 {
		snap.DailySeries["tokens"] = core.SortedTimePoints(tokensByDay)
	}
	if len(costByDay) > 0 {
		snap.DailySeries["cost"] = core.SortedTimePoints(costByDay)
	}

	// ModelUsage. Sort keys for deterministic output across runs.
	modelKeys := make([]string, 0, len(perModel))
	for k := range perModel {
		modelKeys = append(modelKeys, k)
	}
	sort.Strings(modelKeys)
	for _, key := range modelKeys {
		bucket := perModel[key]
		total := bucket.input + bucket.output + bucket.cacheRead + bucket.cacheWrite
		rec := core.ModelUsageRecord{
			RawModelID:   key,
			RawSource:    "jsonl",
			Window:       allTimeWindow,
			InputTokens:  core.Float64Ptr(float64(bucket.input)),
			OutputTokens: core.Float64Ptr(float64(bucket.output)),
			CachedTokens: core.Float64Ptr(float64(bucket.cacheRead)),
			TotalTokens:  core.Float64Ptr(float64(total)),
			Requests:     core.Float64Ptr(float64(bucket.requests)),
		}
		if bucket.hasCost {
			rec.CostUSD = core.Float64Ptr(bucket.cost)
		}
		if bucket.provider != "" {
			rec.SetDimension("upstream_provider", bucket.provider)
		}
		snap.AppendModelUsage(rec)
	}
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

// buildStatusMessage returns the short human-readable summary shown in the
// dashboard tile.
func buildStatusMessage(displayName string, snap core.UsageSnapshot) string {
	parts := make([]string, 0, 3)
	if m, ok := snap.Metrics["total_tasks"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCount(*m.Used, "task"))
	}
	if m, ok := snap.Metrics["total_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatTokens(*m.Used))
	}
	if m, ok := snap.Metrics["total_cost_usd"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, formatCostUSD(*m.Used))
	}
	if len(parts) == 0 {
		return displayName + " OK"
	}
	return strings.Join(parts, ", ")
}

func formatCount(v float64, noun string) string {
	if v == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", int64(v), noun)
}

func formatTokens(v float64) string {
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM tokens", v/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%.1fk tokens", v/1_000)
	default:
		return fmt.Sprintf("%d tokens", int64(v))
	}
}

func formatCostUSD(v float64) string {
	if v >= 1 {
		return fmt.Sprintf("$%.2f", v)
	}
	return fmt.Sprintf("$%.4f", v)
}

// ExtensionChanged returns true if any VS Code globalStorage location
// holding the named extension subdir has been modified after `since`.
// Used by HasChanged hooks for both Roo Code and Kilo Code.
func ExtensionChanged(extensionSubdir string, since time.Time) bool {
	roots := VSCodeGlobalStorageRoots()
	for _, root := range roots {
		extDir := filepath.Join(root, extensionSubdir)
		info, err := os.Stat(extDir)
		if err != nil {
			continue
		}
		if info.ModTime().After(since) {
			return true
		}
		// Also walk the tasks root one level — the parent dir mtime can lag
		// behind newly-modified child task files on some filesystems.
		tasksRoot := filepath.Join(extDir, "tasks")
		entries, err := os.ReadDir(tasksRoot)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			fi, err := entry.Info()
			if err != nil {
				continue
			}
			if fi.ModTime().After(since) {
				return true
			}
		}
	}
	return false
}
