package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

const codexCreditUsageDailySeriesKey = "codex_credit_usage"

const codexCreditUsageDailyPath = "/wham/usage/daily-workspace-user-credit-usage"

type dailyCreditUsagePayload struct {
	Data               []dailyCreditUsageDay `json:"data"`
	DataFreshnessStamp string                `json:"data_freshness_ts,omitempty"`
}

type dailyCreditUsageDay struct {
	Date   string         `json:"date"`
	Values map[string]any `json:"values"`
}

type dailyCreditUsageCache struct {
	periodStartDay string
	today          string
	totals         map[string]float64
	fetchedAt      time.Time
	freshness      string
}

// fetchDailyCreditUsage adds a complete current-period account-credit series to
// the snapshot. The endpoint is an optional enrichment: callers should record
// an error and keep the normal cumulative-quota path when it is unavailable.
func (p *Provider) fetchDailyCreditUsage(
	ctx context.Context,
	acct core.AccountConfig,
	configDir string,
	snap *core.UsageSnapshot,
) error {
	if p == nil || snap == nil {
		return nil
	}

	metric, ok := snap.Metrics["codex_credit_limit"]
	if !ok || metric.Used == nil {
		return nil
	}
	resetAt, ok := snap.Resets["codex_credit_limit"]
	if !ok || !resetAt.After(snap.Timestamp) {
		return nil
	}
	periodStart, ok := inferCreditPeriodStart(resetAt, snap.Timestamp)
	if !ok {
		return nil
	}

	authPath := filepath.Join(configDir, "auth.json")
	if override := acct.Hint("auth_file", ""); override != "" {
		authPath = override
	}
	authData, err := os.ReadFile(authPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("codex: reading daily credit auth: %w", err)
	}
	var auth authFile
	if err := json.Unmarshal(authData, &auth); err != nil {
		return fmt.Errorf("codex: parsing daily credit auth: %w", err)
	}
	if strings.TrimSpace(auth.Tokens.AccessToken) == "" {
		return nil
	}

	location := time.Local
	startDay := startOfCodexDay(periodStart, location)
	today := startOfCodexDay(snap.Timestamp, location)
	if today.Before(startDay) {
		return nil
	}

	baseURL := resolveChatGPTBaseURL(acct, configDir)
	cacheKey := dailyCreditUsageCacheKey(acct, auth, authPath, baseURL)
	dailyTotals, fetchedAt, dataFreshness, cacheHit := p.loadDailyCreditUsageCache(
		cacheKey,
		formatCodexDay(startDay),
		formatCodexDay(today),
	)
	if !cacheHit {
		endpoint := dailyCreditUsageURLForBase(baseURL)
		components, err := url.Parse(endpoint)
		if err != nil {
			return fmt.Errorf("codex: building daily credit URL: %w", err)
		}
		components.RawQuery = url.Values{
			"start_date": []string{formatCodexDay(startDay)},
			"end_date":   []string{formatCodexDay(today)},
			"breakdown":  []string{"product"},
		}.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, components.String(), nil)
		if err != nil {
			return fmt.Errorf("codex: creating daily credit request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+auth.Tokens.AccessToken)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Pragma", "no-cache")
		if accountID := core.FirstNonEmpty(auth.Tokens.AccountID, auth.AccountID, acct.Hint("account_id", "")); accountID != "" {
			req.Header.Set("ChatGPT-Account-Id", accountID)
		}
		if cliVersion := snap.Raw["cli_version"]; cliVersion != "" {
			req.Header.Set("User-Agent", "codex-cli/"+cliVersion)
		} else {
			req.Header.Set("User-Agent", "codex-cli")
		}

		resp, err := p.Client().Do(req)
		if err != nil {
			return fmt.Errorf("codex: daily credit request failed: %w", err)
		}
		defer resp.Body.Close()

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return fmt.Errorf("codex: reading daily credit response: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("codex: daily credit HTTP %d: %s", resp.StatusCode, truncateForError(string(body), maxHTTPErrorBodySize))
		}

		var payload dailyCreditUsagePayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return fmt.Errorf("codex: parsing daily credit response: %w", err)
		}

		dailyTotals = make(map[string]float64)
		for day := startDay; !day.After(today); day = day.AddDate(0, 0, 1) {
			dailyTotals[formatCodexDay(day)] = 0
		}

		for _, point := range payload.Data {
			pointDay, err := parseCodexDay(point.Date, location)
			if err != nil {
				return fmt.Errorf("codex: parsing daily credit date %q: %w", point.Date, err)
			}
			if pointDay.Before(startDay) || pointDay.After(today) {
				continue
			}
			credits, ok := parseFlexibleNumber(point.Values["codex"])
			if !ok || math.IsNaN(credits) || math.IsInf(credits, 0) || credits < 0 {
				credits = 0
			}
			key := formatCodexDay(pointDay)
			dailyTotals[key] += credits
		}

		fetchedAt = time.Now()
		dataFreshness = payload.DataFreshnessStamp
		p.storeDailyCreditUsageCache(cacheKey, dailyCreditUsageCache{
			periodStartDay: formatCodexDay(startDay),
			today:          formatCodexDay(today),
			totals:         dailyTotals,
			fetchedAt:      fetchedAt,
			freshness:      dataFreshness,
		})
	}

	historicalCredits := 0.0
	for day, credits := range dailyTotals {
		if day < formatCodexDay(today) {
			historicalCredits += credits
		}
	}
	// The daily endpoint can lag behind the live quota response. Today's
	// account-authoritative value is therefore the live cumulative total minus
	// the server-reported historical days, never the daily endpoint's today row.
	usedCredits := *metric.Used
	if math.IsNaN(usedCredits) || math.IsInf(usedCredits, 0) || usedCredits < 0 {
		usedCredits = 0
	}
	dailyTotals[formatCodexDay(today)] = maxFloat(usedCredits - historicalCredits)

	snap.EnsureMaps()
	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	snap.DailySeries[codexCreditUsageDailySeriesKey] = core.SortedTimePoints(dailyTotals)
	snap.Raw["credit_daily_usage_source"] = "account"
	snap.Raw["credit_daily_usage_complete"] = "true"
	snap.Raw["credit_daily_usage_period_start"] = periodStart.UTC().Format(time.RFC3339)
	snap.Raw["credit_daily_usage_fetched_at"] = fetchedAt.UTC().Format(time.RFC3339)
	if dataFreshness != "" {
		snap.Raw["credit_daily_usage_data_freshness"] = dataFreshness
	}
	if cacheHit {
		snap.Raw["credit_daily_usage_cache"] = "hit"
	}
	return nil
}

func dailyCreditUsageCacheKey(acct core.AccountConfig, auth authFile, authPath, baseURL string) string {
	accountKey := core.FirstNonEmpty(acct.ID, auth.Tokens.AccountID, auth.AccountID, acct.Hint("account_id", ""))
	return strings.Join([]string{accountKey, authPath, baseURL}, "\x00")
}

func (p *Provider) loadDailyCreditUsageCache(key, periodStartDay, today string) (map[string]float64, time.Time, string, bool) {
	if p == nil {
		return nil, time.Time{}, "", false
	}
	p.creditDailyMu.Lock()
	defer p.creditDailyMu.Unlock()
	entry, ok := p.creditDaily[key]
	if !ok || entry.periodStartDay != periodStartDay || entry.today != today {
		return nil, time.Time{}, "", false
	}
	totals := make(map[string]float64, len(entry.totals))
	for day, credits := range entry.totals {
		totals[day] = credits
	}
	return totals, entry.fetchedAt, entry.freshness, true
}

func (p *Provider) storeDailyCreditUsageCache(key string, entry dailyCreditUsageCache) {
	if p == nil {
		return
	}
	totals := make(map[string]float64, len(entry.totals))
	for day, credits := range entry.totals {
		totals[day] = credits
	}
	entry.totals = totals
	p.creditDailyMu.Lock()
	defer p.creditDailyMu.Unlock()
	if p.creditDaily == nil {
		p.creditDaily = make(map[string]dailyCreditUsageCache)
	}
	p.creditDaily[key] = entry
}

func dailyCreditUsageURLForBase(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.Contains(baseURL, "/backend-api") {
		return baseURL + codexCreditUsageDailyPath
	}
	return baseURL + "/api/codex/usage" + codexCreditUsageDailyPath[len("/wham/usage"):]
}

func startOfCodexDay(value time.Time, location *time.Location) time.Time {
	local := value.In(location)
	year, month, day := local.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}

func formatCodexDay(value time.Time) string {
	return value.Format("2006-01-02")
}

func parseCodexDay(value string, location *time.Location) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(value), location)
}

func maxFloat(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
