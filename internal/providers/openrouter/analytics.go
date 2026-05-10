package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// fetchAnalytics is the orchestrator for OpenRouter's activity analytics.
// It splits into four phases:
//
//  1. discoverActivityEndpoint — try each known activity endpoint until one
//     returns 200 with a parseable body.
//  2. aggregateActivity — fold the rows into per-date / per-model /
//     per-provider / per-endpoint totals.
//  3. emit*Metrics — translate each aggregate slice into snapshot metrics
//     and daily-series.
//
// Each phase is testable in isolation; before the split this was a single
// 380-line function.
func (p *Provider) fetchAnalytics(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	analytics, endpoint, cachedAt, err := p.discoverActivityEndpoint(ctx, baseURL, apiKey)
	if err != nil {
		return err
	}

	snap.Raw["activity_endpoint"] = endpoint
	if cachedAt != "" {
		snap.Raw["activity_cached_at"] = cachedAt
	}

	agg := aggregateActivity(analytics.Data, p.now().UTC())
	emitActivityRawCounts(snap, len(analytics.Data), agg)
	emitActivityDailySeries(snap, agg)
	emitActivityWindowMetrics(snap, agg)
	emitActivityCardinalityMetrics(snap, agg)
	emitActivityBreakdowns(snap, agg)
	emitActivityBYOKWindows(snap, agg)
	return nil
}

// discoverActivityEndpoint walks OpenRouter's documented activity endpoints
// in fallback order and returns the first one that succeeds with a body we
// can parse. The 403-on-/activity case is special: it usually means the user
// has only a non-management key, and we surface the underlying message.
func (p *Provider) discoverActivityEndpoint(ctx context.Context, baseURL, apiKey string) (analyticsResponse, string, string, error) {
	yesterdayUTC := p.now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	candidates := []string{
		"/activity",
		"/activity?date=" + yesterdayUTC,
		"/analytics/user-activity",
		"/api/internal/v1/transaction-analytics?window=1mo",
	}

	forbiddenMsg := ""
	for _, endpoint := range candidates {
		body, status, err := p.getActivityEndpoint(ctx, baseURL+"", endpoint, apiKey)
		if err != nil {
			return analyticsResponse{}, "", "", err
		}

		if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound {
			if endpoint == "/activity" && status == http.StatusForbidden {
				msg := parseAPIErrorMessage(body)
				if msg == "" {
					msg = "activity endpoint requires management key"
				}
				forbiddenMsg = msg
			}
			continue
		}
		if status != http.StatusOK {
			continue
		}

		parsed, cachedAt, ok, err := parseAnalyticsBody(body)
		if err != nil || !ok {
			continue
		}
		return parsed, endpoint, cachedAt, nil
	}

	if forbiddenMsg != "" {
		return analyticsResponse{}, "", "", fmt.Errorf("openrouter: %s (HTTP 403)", forbiddenMsg)
	}
	return analyticsResponse{}, "", "", fmt.Errorf("openrouter: analytics endpoint not available (HTTP 404)")
}

// getActivityEndpoint performs the HTTP GET; returns body + status. URL
// construction is funnelled through analyticsEndpointURL.
func (p *Provider) getActivityEndpoint(ctx context.Context, baseURL, endpoint, apiKey string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, analyticsEndpointURL(baseURL, endpoint), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache, no-store, max-age=0")
	req.Header.Set("Pragma", "no-cache")

	resp, err := p.Client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// activityAggregates is the bag of every aggregate the activity loop
// produces. Held together so each emit* function takes a single argument
// and the loop stays readable.
type activityAggregates struct {
	costByDate             map[string]float64
	tokensByDate           map[string]float64
	requestsByDate         map[string]float64
	byokCostByDate         map[string]float64
	reasoningTokensByDate  map[string]float64
	cachedTokensByDate     map[string]float64
	providerTokensByDate   map[string]map[string]float64
	providerRequestsByDate map[string]map[string]float64

	modelCost            map[string]float64
	modelByokCost        map[string]float64
	modelInputTokens     map[string]float64
	modelOutputTokens    map[string]float64
	modelReasoningTokens map[string]float64
	modelCachedTokens    map[string]float64
	modelTotalTokens     map[string]float64
	modelRequests        map[string]float64
	modelByokRequests    map[string]float64

	providerCost            map[string]float64
	providerByokCost        map[string]float64
	providerInputTokens     map[string]float64
	providerOutputTokens    map[string]float64
	providerReasoningTokens map[string]float64
	providerRequests        map[string]float64

	endpointStatsMap map[string]*endpointStats
	models           map[string]struct{}
	providers        map[string]struct{}
	endpoints        map[string]struct{}
	activeDays       map[string]struct{}

	totalCost, totalByok, totalRequests      float64
	totalInput, totalOutput, totalReasoning  float64
	totalCached, totalTokens                 float64
	cost7d, byok7d, requests7d               float64
	input7d, output7d, reasoning7d, cached7d float64
	tokens7d                                 float64
	todayByok, cost7dByok, cost30dByok       float64
	minDate, maxDate                         string
}

func newActivityAggregates() *activityAggregates {
	return &activityAggregates{
		costByDate:             make(map[string]float64),
		tokensByDate:           make(map[string]float64),
		requestsByDate:         make(map[string]float64),
		byokCostByDate:         make(map[string]float64),
		reasoningTokensByDate:  make(map[string]float64),
		cachedTokensByDate:     make(map[string]float64),
		providerTokensByDate:   make(map[string]map[string]float64),
		providerRequestsByDate: make(map[string]map[string]float64),

		modelCost:            make(map[string]float64),
		modelByokCost:        make(map[string]float64),
		modelInputTokens:     make(map[string]float64),
		modelOutputTokens:    make(map[string]float64),
		modelReasoningTokens: make(map[string]float64),
		modelCachedTokens:    make(map[string]float64),
		modelTotalTokens:     make(map[string]float64),
		modelRequests:        make(map[string]float64),
		modelByokRequests:    make(map[string]float64),

		providerCost:            make(map[string]float64),
		providerByokCost:        make(map[string]float64),
		providerInputTokens:     make(map[string]float64),
		providerOutputTokens:    make(map[string]float64),
		providerReasoningTokens: make(map[string]float64),
		providerRequests:        make(map[string]float64),

		endpointStatsMap: make(map[string]*endpointStats),
		models:           make(map[string]struct{}),
		providers:        make(map[string]struct{}),
		endpoints:        make(map[string]struct{}),
		activeDays:       make(map[string]struct{}),
	}
}

// aggregateActivity folds every analytics row into the bag of aggregates.
// Pure: no snap/state side effects, no I/O. The `now` param is passed in so
// tests can pin time without mutating the provider.
func aggregateActivity(rows []analyticsEntry, now time.Time) *activityAggregates {
	agg := newActivityAggregates()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	sevenDaysAgo := now.AddDate(0, 0, -7)
	thirtyDaysAgo := now.AddDate(0, 0, -30)

	for _, entry := range rows {
		if entry.Date == "" {
			continue
		}
		date, entryDate, hasParsedDate := normalizeActivityDate(entry.Date)

		cost := entry.Usage
		if cost == 0 {
			cost = entry.TotalCost
		}
		tokens := float64(entry.TotalTokens)
		if tokens == 0 {
			tokens = float64(entry.PromptTokens + entry.CompletionTokens + entry.ReasoningTokens)
		}
		inputTokens := float64(entry.PromptTokens)
		outputTokens := float64(entry.CompletionTokens)
		requests := float64(entry.Requests)
		byokCost := entry.ByokUsageInference
		byokRequests := float64(entry.ByokRequests)
		reasoningTokens := float64(entry.ReasoningTokens)
		cachedTokens := float64(entry.CachedTokens)
		modelName := normalizeModelName(entry.Model)
		if modelName == "" {
			modelName = normalizeModelName(entry.ModelPermaslug)
		}
		if modelName == "" {
			modelName = "unknown"
		}
		providerName := entry.ProviderName
		if providerName == "" {
			providerName = "unknown"
		}
		endpointID := strings.TrimSpace(entry.EndpointID)
		if endpointID == "" {
			endpointID = "unknown"
		}

		agg.costByDate[date] += cost
		agg.tokensByDate[date] += tokens
		agg.requestsByDate[date] += requests
		agg.byokCostByDate[date] += byokCost
		agg.reasoningTokensByDate[date] += reasoningTokens
		agg.cachedTokensByDate[date] += cachedTokens
		agg.modelCost[modelName] += cost
		agg.modelByokCost[modelName] += byokCost
		agg.modelInputTokens[modelName] += inputTokens
		agg.modelOutputTokens[modelName] += outputTokens
		agg.modelReasoningTokens[modelName] += reasoningTokens
		agg.modelCachedTokens[modelName] += cachedTokens
		agg.modelTotalTokens[modelName] += tokens
		agg.modelRequests[modelName] += requests
		agg.modelByokRequests[modelName] += byokRequests
		agg.providerCost[providerName] += cost
		agg.providerByokCost[providerName] += byokCost
		agg.providerInputTokens[providerName] += inputTokens
		agg.providerOutputTokens[providerName] += outputTokens
		agg.providerReasoningTokens[providerName] += reasoningTokens
		agg.providerRequests[providerName] += requests
		providerClientKey := sanitizeName(strings.ToLower(providerName))
		if agg.providerTokensByDate[providerClientKey] == nil {
			agg.providerTokensByDate[providerClientKey] = make(map[string]float64)
		}
		agg.providerTokensByDate[providerClientKey][date] += inputTokens + outputTokens + reasoningTokens
		if agg.providerRequestsByDate[providerClientKey] == nil {
			agg.providerRequestsByDate[providerClientKey] = make(map[string]float64)
		}
		agg.providerRequestsByDate[providerClientKey][date] += requests

		stats := agg.endpointStatsMap[endpointID]
		if stats == nil {
			stats = &endpointStats{Model: modelName, Provider: providerName}
			agg.endpointStatsMap[endpointID] = stats
		}
		stats.Requests += entry.Requests
		stats.TotalCost += cost
		stats.ByokCost += byokCost
		stats.PromptTokens += entry.PromptTokens
		stats.CompletionTokens += entry.CompletionTokens
		stats.ReasoningTokens += entry.ReasoningTokens

		agg.models[modelName] = struct{}{}
		agg.providers[providerName] = struct{}{}
		if endpointID != "unknown" {
			agg.endpoints[endpointID] = struct{}{}
		}
		agg.activeDays[date] = struct{}{}

		if agg.minDate == "" || date < agg.minDate {
			agg.minDate = date
		}
		if agg.maxDate == "" || date > agg.maxDate {
			agg.maxDate = date
		}

		agg.totalCost += cost
		agg.totalByok += byokCost
		agg.totalRequests += requests
		agg.totalInput += inputTokens
		agg.totalOutput += outputTokens
		agg.totalReasoning += reasoningTokens
		agg.totalCached += cachedTokens
		agg.totalTokens += tokens

		if !hasParsedDate {
			continue
		}
		if !entryDate.Before(todayStart) {
			agg.todayByok += byokCost
		}
		if entryDate.After(sevenDaysAgo) {
			agg.cost7dByok += byokCost
		}
		if entryDate.After(thirtyDaysAgo) {
			agg.cost30dByok += byokCost
		}
		if entryDate.After(sevenDaysAgo) {
			agg.cost7d += cost
			agg.byok7d += byokCost
			agg.requests7d += requests
			agg.input7d += inputTokens
			agg.output7d += outputTokens
			agg.reasoning7d += reasoningTokens
			agg.cached7d += cachedTokens
			agg.tokens7d += tokens
		}
	}
	return agg
}

// emitActivityRawCounts writes the raw count strings (rows, date range,
// distinct model/provider/endpoint counts).
func emitActivityRawCounts(snap *core.UsageSnapshot, rowCount int, agg *activityAggregates) {
	snap.Raw["activity_rows"] = fmt.Sprintf("%d", rowCount)
	if agg.minDate != "" && agg.maxDate != "" {
		snap.Raw["activity_date_range"] = agg.minDate + " .. " + agg.maxDate
	}
	if agg.minDate != "" {
		snap.Raw["activity_min_date"] = agg.minDate
	}
	if agg.maxDate != "" {
		snap.Raw["activity_max_date"] = agg.maxDate
	}
	for _, kv := range []struct {
		key  string
		size int
	}{
		{"activity_models", len(agg.models)},
		{"activity_providers", len(agg.providers)},
		{"activity_endpoints", len(agg.endpoints)},
		{"activity_days", len(agg.activeDays)},
	} {
		if kv.size > 0 {
			snap.Raw[kv.key] = fmt.Sprintf("%d", kv.size)
		}
	}
}

// emitActivityDailySeries writes the per-date time-series slices.
func emitActivityDailySeries(snap *core.UsageSnapshot, agg *activityAggregates) {
	for _, kv := range []struct {
		key string
		m   map[string]float64
	}{
		{"analytics_cost", agg.costByDate},
		{"analytics_tokens", agg.tokensByDate},
		{"analytics_requests", agg.requestsByDate},
		{"analytics_byok_cost", agg.byokCostByDate},
		{"analytics_reasoning_tokens", agg.reasoningTokensByDate},
		{"analytics_cached_tokens", agg.cachedTokensByDate},
	} {
		if len(kv.m) > 0 {
			snap.DailySeries[kv.key] = core.SortedTimePoints(kv.m)
		}
	}
}

// emitActivityWindowMetrics writes the 30d and 7d aggregate metrics.
func emitActivityWindowMetrics(snap *core.UsageSnapshot, agg *activityAggregates) {
	emit := func(key string, value float64, unit, window string) {
		if value > 0 {
			snap.Metrics[key] = core.Metric{Used: &value, Unit: unit, Window: window}
		}
	}
	if agg.totalByok > 0 {
		snap.Raw["byok_in_use"] = "true"
	}
	emit("analytics_30d_cost", agg.totalCost, "USD", "30d")
	emit("analytics_30d_byok_cost", agg.totalByok, "USD", "30d")
	emit("analytics_30d_requests", agg.totalRequests, "requests", "30d")
	emit("analytics_30d_input_tokens", agg.totalInput, "tokens", "30d")
	emit("analytics_30d_output_tokens", agg.totalOutput, "tokens", "30d")
	emit("analytics_30d_reasoning_tokens", agg.totalReasoning, "tokens", "30d")
	emit("analytics_30d_cached_tokens", agg.totalCached, "tokens", "30d")
	emit("analytics_30d_tokens", agg.totalTokens, "tokens", "30d")

	if agg.byok7d > 0 {
		snap.Raw["byok_in_use"] = "true"
	}
	emit("analytics_7d_cost", agg.cost7d, "USD", "7d")
	emit("analytics_7d_byok_cost", agg.byok7d, "USD", "7d")
	emit("analytics_7d_requests", agg.requests7d, "requests", "7d")
	emit("analytics_7d_input_tokens", agg.input7d, "tokens", "7d")
	emit("analytics_7d_output_tokens", agg.output7d, "tokens", "7d")
	emit("analytics_7d_reasoning_tokens", agg.reasoning7d, "tokens", "7d")
	emit("analytics_7d_cached_tokens", agg.cached7d, "tokens", "7d")
	emit("analytics_7d_tokens", agg.tokens7d, "tokens", "7d")
}

// emitActivityCardinalityMetrics writes the count-of-distinct metrics
// (active days, models, providers, endpoints over 30d).
func emitActivityCardinalityMetrics(snap *core.UsageSnapshot, agg *activityAggregates) {
	for _, kv := range []struct {
		key  string
		size int
		unit string
	}{
		{"analytics_active_days", len(agg.activeDays), "days"},
		{"analytics_models", len(agg.models), "models"},
		{"analytics_providers", len(agg.providers), "providers"},
		{"analytics_endpoints", len(agg.endpoints), "endpoints"},
	} {
		if kv.size > 0 {
			v := float64(kv.size)
			snap.Metrics[kv.key] = core.Metric{Used: &v, Unit: kv.unit, Window: "30d"}
		}
	}
}

// emitActivityBreakdowns writes the per-model, per-provider, per-endpoint,
// and client-daily-series metrics. Filters out router-client provider names
// before emission so dashboards don't double-count OpenRouter's own routing.
func emitActivityBreakdowns(snap *core.UsageSnapshot, agg *activityAggregates) {
	emitAnalyticsPerModelMetrics(snap,
		agg.modelCost, agg.modelByokCost,
		agg.modelInputTokens, agg.modelOutputTokens, agg.modelReasoningTokens, agg.modelCachedTokens,
		agg.modelTotalTokens, agg.modelRequests, agg.modelByokRequests)
	filterRouterClientProviders(
		agg.providerCost, agg.providerByokCost,
		agg.providerInputTokens, agg.providerOutputTokens, agg.providerReasoningTokens,
		agg.providerRequests)
	emitAnalyticsPerProviderMetrics(snap,
		agg.providerCost, agg.providerByokCost,
		agg.providerInputTokens, agg.providerOutputTokens, agg.providerReasoningTokens,
		agg.providerRequests)
	emitUpstreamProviderMetrics(snap,
		agg.providerCost,
		agg.providerInputTokens, agg.providerOutputTokens, agg.providerReasoningTokens,
		agg.providerRequests)
	emitAnalyticsEndpointMetrics(snap, agg.endpointStatsMap)

	for name := range agg.providerTokensByDate {
		if isLikelyRouterClientProviderName(name) {
			delete(agg.providerTokensByDate, name)
		}
	}
	for name := range agg.providerRequestsByDate {
		if isLikelyRouterClientProviderName(name) {
			delete(agg.providerRequestsByDate, name)
		}
	}
	emitClientDailySeries(snap, agg.providerTokensByDate, agg.providerRequestsByDate)
	emitModelDerivedToolUsageMetrics(snap, agg.modelRequests, "30d inferred", "inferred_from_model_requests")
}

// emitActivityBYOKWindows writes the today/7d/30d BYOK cost windows.
func emitActivityBYOKWindows(snap *core.UsageSnapshot, agg *activityAggregates) {
	for _, kv := range []struct {
		key    string
		value  float64
		window string
	}{
		{"today_byok_cost", agg.todayByok, "1d"},
		{"7d_byok_cost", agg.cost7dByok, "7d"},
		{"30d_byok_cost", agg.cost30dByok, "30d"},
	} {
		if kv.value > 0 {
			v := kv.value
			snap.Metrics[kv.key] = core.Metric{Used: &v, Unit: "USD", Window: kv.window}
			snap.Raw["byok_in_use"] = "true"
		}
	}
}

func analyticsEndpointURL(baseURL, endpoint string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasPrefix(endpoint, "/api/internal/") && strings.HasSuffix(base, "/api/v1") {
		base = strings.TrimSuffix(base, "/api/v1")
	}
	return base + endpoint
}

func parseAnalyticsBody(body []byte) (analyticsResponse, string, bool, error) {
	var direct analyticsResponse
	if err := json.Unmarshal(body, &direct); err == nil && direct.Data != nil {
		return direct, "", true, nil
	}

	var wrapped analyticsEnvelopeResponse
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Data.Data != nil {
		return analyticsResponse{Data: wrapped.Data.Data}, parseAnalyticsCachedAt(wrapped.Data.CachedAt), true, nil
	}

	return analyticsResponse{}, "", false, fmt.Errorf("unrecognized analytics payload")
}

func parseAnalyticsCachedAt(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}

	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return strings.TrimSpace(str)
	}

	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return s
	}

	sec := int64(n)
	if sec > 1_000_000_000_000 {
		sec /= 1000
	}
	if sec <= 0 {
		return fmt.Sprintf("%.0f", n)
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

func normalizeActivityDate(raw string) (string, time.Time, bool) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			date := t.UTC().Format("2006-01-02")
			return date, t.UTC(), true
		}
	}
	if len(raw) >= 10 && raw[4] == '-' && raw[7] == '-' {
		date := raw[:10]
		if t, err := time.Parse("2006-01-02", date); err == nil {
			return date, t.UTC(), true
		}
		return date, time.Time{}, false
	}
	return raw, time.Time{}, false
}

func emitAnalyticsPerModelMetrics(
	snap *core.UsageSnapshot,
	modelCost, modelByokCost, modelInputTokens, modelOutputTokens, modelReasoningTokens, modelCachedTokens, modelTotalTokens, modelRequests, modelByokRequests map[string]float64,
) {
	modelSet := make(map[string]struct{})
	for model := range modelCost {
		modelSet[model] = struct{}{}
	}
	for model := range modelByokCost {
		modelSet[model] = struct{}{}
	}
	for model := range modelInputTokens {
		modelSet[model] = struct{}{}
	}
	for model := range modelOutputTokens {
		modelSet[model] = struct{}{}
	}
	for model := range modelReasoningTokens {
		modelSet[model] = struct{}{}
	}
	for model := range modelCachedTokens {
		modelSet[model] = struct{}{}
	}
	for model := range modelTotalTokens {
		modelSet[model] = struct{}{}
	}
	for model := range modelRequests {
		modelSet[model] = struct{}{}
	}
	for model := range modelByokRequests {
		modelSet[model] = struct{}{}
	}

	for model := range modelSet {
		safe := sanitizeName(model)
		prefix := "model_" + safe
		rec := core.ModelUsageRecord{RawModelID: model, RawSource: "api", Window: "activity"}

		if v := modelCost[model]; v > 0 {
			snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
			rec.CostUSD = core.Float64Ptr(v)
		}
		if v := modelByokCost[model]; v > 0 {
			snap.Metrics[prefix+"_byok_cost"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if v := modelInputTokens[model]; v > 0 {
			snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
			rec.InputTokens = core.Float64Ptr(v)
		}
		if v := modelOutputTokens[model]; v > 0 {
			snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
			rec.OutputTokens = core.Float64Ptr(v)
		}
		if v := modelReasoningTokens[model]; v > 0 {
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
			rec.ReasoningTokens = core.Float64Ptr(v)
		}
		if v := modelCachedTokens[model]; v > 0 {
			snap.Metrics[prefix+"_cached_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
			rec.CachedTokens = core.Float64Ptr(v)
		}
		if v := modelTotalTokens[model]; v > 0 {
			snap.Metrics[prefix+"_total_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
			rec.TotalTokens = core.Float64Ptr(v)
		}
		if v := modelRequests[model]; v > 0 {
			snap.Metrics[prefix+"_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "activity"}
			snap.Raw[prefix+"_requests"] = fmt.Sprintf("%.0f", v)
			rec.Requests = core.Float64Ptr(v)
		}
		if v := modelByokRequests[model]; v > 0 {
			snap.Metrics[prefix+"_byok_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "activity"}
		}
		if rec.InputTokens != nil || rec.OutputTokens != nil || rec.CostUSD != nil || rec.Requests != nil || rec.ReasoningTokens != nil || rec.CachedTokens != nil || rec.TotalTokens != nil {
			snap.AppendModelUsage(rec)
		}
	}
}

func filterRouterClientProviders(maps ...map[string]float64) {
	for _, metrics := range maps {
		for name := range metrics {
			if isLikelyRouterClientProviderName(name) {
				delete(metrics, name)
			}
		}
	}
}

func emitAnalyticsPerProviderMetrics(
	snap *core.UsageSnapshot,
	providerCost, providerByokCost, providerInputTokens, providerOutputTokens, providerReasoningTokens, providerRequests map[string]float64,
) {
	providerSet := make(map[string]struct{})
	for provider := range providerCost {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerByokCost {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerInputTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerOutputTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerReasoningTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerRequests {
		providerSet[provider] = struct{}{}
	}

	for provider := range providerSet {
		prefix := "provider_" + sanitizeName(strings.ToLower(provider))
		if v := providerCost[provider]; v > 0 {
			snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if v := providerByokCost[provider]; v > 0 {
			snap.Metrics[prefix+"_byok_cost"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if v := providerInputTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerOutputTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerReasoningTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerRequests[provider]; v > 0 {
			snap.Metrics[prefix+"_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "activity"}
		}

		snap.Raw[prefix+"_requests"] = fmt.Sprintf("%.0f", providerRequests[provider])
		snap.Raw[prefix+"_cost"] = fmt.Sprintf("$%.6f", providerCost[provider])
		if providerByokCost[provider] > 0 {
			snap.Raw[prefix+"_byok_cost"] = fmt.Sprintf("$%.6f", providerByokCost[provider])
		}
		snap.Raw[prefix+"_prompt_tokens"] = fmt.Sprintf("%.0f", providerInputTokens[provider])
		snap.Raw[prefix+"_completion_tokens"] = fmt.Sprintf("%.0f", providerOutputTokens[provider])
		if providerReasoningTokens[provider] > 0 {
			snap.Raw[prefix+"_reasoning_tokens"] = fmt.Sprintf("%.0f", providerReasoningTokens[provider])
		}
	}
}

func emitUpstreamProviderMetrics(
	snap *core.UsageSnapshot,
	providerCost, providerInputTokens, providerOutputTokens, providerReasoningTokens, providerRequests map[string]float64,
) {
	providerSet := make(map[string]struct{})
	for provider := range providerCost {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerInputTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerOutputTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerReasoningTokens {
		providerSet[provider] = struct{}{}
	}
	for provider := range providerRequests {
		providerSet[provider] = struct{}{}
	}

	for provider := range providerSet {
		prefix := "upstream_" + sanitizeName(strings.ToLower(provider))
		if v := providerCost[provider]; v > 0 {
			snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if v := providerInputTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerOutputTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerReasoningTokens[provider]; v > 0 {
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if v := providerRequests[provider]; v > 0 {
			snap.Metrics[prefix+"_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "activity"}
		}
	}
}

func emitAnalyticsEndpointMetrics(snap *core.UsageSnapshot, endpointStatsMap map[string]*endpointStats) {
	type endpointEntry struct {
		id    string
		stats *endpointStats
	}

	var entries []endpointEntry
	for id, stats := range endpointStatsMap {
		if id == "unknown" {
			continue
		}
		entries = append(entries, endpointEntry{id: id, stats: stats})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].stats.TotalCost != entries[j].stats.TotalCost {
			return entries[i].stats.TotalCost > entries[j].stats.TotalCost
		}
		return entries[i].stats.Requests > entries[j].stats.Requests
	})

	const maxEndpointMetrics = 8
	if len(entries) > maxEndpointMetrics {
		entries = entries[:maxEndpointMetrics]
	}
	for _, entry := range entries {
		prefix := "endpoint_" + sanitizeName(entry.id)

		if req := float64(entry.stats.Requests); req > 0 {
			snap.Metrics[prefix+"_requests"] = core.Metric{Used: &req, Unit: "requests", Window: "activity"}
		}
		if entry.stats.TotalCost > 0 {
			v := entry.stats.TotalCost
			snap.Metrics[prefix+"_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if entry.stats.ByokCost > 0 {
			v := entry.stats.ByokCost
			snap.Metrics[prefix+"_byok_cost"] = core.Metric{Used: &v, Unit: "USD", Window: "activity"}
		}
		if entry.stats.PromptTokens > 0 {
			v := float64(entry.stats.PromptTokens)
			snap.Metrics[prefix+"_input_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if entry.stats.CompletionTokens > 0 {
			v := float64(entry.stats.CompletionTokens)
			snap.Metrics[prefix+"_output_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if entry.stats.ReasoningTokens > 0 {
			v := float64(entry.stats.ReasoningTokens)
			snap.Metrics[prefix+"_reasoning_tokens"] = core.Metric{Used: &v, Unit: "tokens", Window: "activity"}
		}
		if entry.stats.Provider != "" {
			snap.Raw[prefix+"_provider"] = entry.stats.Provider
		}
		if entry.stats.Model != "" {
			snap.Raw[prefix+"_model"] = entry.stats.Model
		}
	}
}

func parseAPIErrorMessage(body []byte) string {
	var apiErr apiErrorResponse
	if err := json.Unmarshal(body, &apiErr); err != nil {
		return ""
	}
	return strings.TrimSpace(apiErr.Error.Message)
}
