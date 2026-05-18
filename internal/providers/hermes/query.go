package hermes

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// hermesSession is the in-memory representation of one row of the sessions
// table.
type hermesSession struct {
	ID              string
	Model           string
	Provider        string
	StartedAt       time.Time
	MessageCount    int64
	InputTokens     int64
	OutputTokens    int64
	CacheReadTokens int64
	CacheWriteTok   int64
	ReasoningTokens int64
	CostUSD         float64
	HasCost         bool
}

// columnPresence summarises which optional columns are present in the
// sessions table. Older releases of Hermes shipped a smaller schema; we
// probe before SELECTing rather than failing fast on a missing column.
type columnPresence struct {
	HasModel           bool
	HasBillingProvider bool
	HasStartedAt       bool
	HasMessageCount    bool
	HasInput           bool
	HasOutput          bool
	HasCacheRead       bool
	HasCacheWrite      bool
	HasReasoning       bool
	HasEstimatedCost   bool
	HasActualCost      bool
}

// detectColumns inspects the sessions table via PRAGMA table_info and
// reports which optional columns exist.
func detectColumns(ctx context.Context, db *sql.DB) (columnPresence, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(sessions)`)
	if err != nil {
		return columnPresence{}, fmt.Errorf("hermes: reading sessions schema: %w", err)
	}
	defer rows.Close()

	present := make(map[string]bool)
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return columnPresence{}, fmt.Errorf("hermes: scanning sessions schema: %w", err)
		}
		present[strings.ToLower(strings.TrimSpace(name))] = true
	}
	if err := rows.Err(); err != nil {
		return columnPresence{}, fmt.Errorf("hermes: iterating sessions schema: %w", err)
	}

	return columnPresence{
		HasModel:           present["model"],
		HasBillingProvider: present["billing_provider"],
		HasStartedAt:       present["started_at"],
		HasMessageCount:    present["message_count"],
		HasInput:           present["input_tokens"],
		HasOutput:          present["output_tokens"],
		HasCacheRead:       present["cache_read_tokens"],
		HasCacheWrite:      present["cache_write_tokens"],
		HasReasoning:       present["reasoning_tokens"],
		HasEstimatedCost:   present["estimated_cost_usd"],
		HasActualCost:      present["actual_cost_usd"],
	}, nil
}

// queryHermesSessions returns all non-empty sessions in the database.
// A session is "empty" when every token category is zero or NULL and there
// is no positive cost recorded.
func queryHermesSessions(ctx context.Context, dbPath string) ([]hermesSession, error) {
	db, err := openReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	if err := pingContext(ctx, db); err != nil {
		return nil, err
	}

	cols, err := detectColumns(ctx, db)
	if err != nil {
		return nil, err
	}
	if !cols.HasModel || !cols.HasStartedAt {
		// Without model + started_at we can't surface useful records.
		return nil, nil
	}

	selectParts := []string{
		"id",
		"model",
		columnOrNull("billing_provider", cols.HasBillingProvider),
		"started_at",
		columnOrNull("message_count", cols.HasMessageCount),
		columnOrNull("input_tokens", cols.HasInput),
		columnOrNull("output_tokens", cols.HasOutput),
		columnOrNull("cache_read_tokens", cols.HasCacheRead),
		columnOrNull("cache_write_tokens", cols.HasCacheWrite),
		columnOrNull("reasoning_tokens", cols.HasReasoning),
		columnOrNull("estimated_cost_usd", cols.HasEstimatedCost),
		columnOrNull("actual_cost_usd", cols.HasActualCost),
	}

	query := fmt.Sprintf(
		`SELECT %s FROM sessions WHERE model IS NOT NULL AND TRIM(model) != ''`,
		strings.Join(selectParts, ", "),
	)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("hermes: querying sessions: %w", err)
	}
	defer rows.Close()

	var out []hermesSession
	for rows.Next() {
		var (
			id           string
			model        string
			billing      sql.NullString
			startedAtRaw sql.NullFloat64
			messageCount sql.NullInt64
			input        sql.NullInt64
			output       sql.NullInt64
			cacheRead    sql.NullInt64
			cacheWrite   sql.NullInt64
			reasoning    sql.NullInt64
			estCost      sql.NullFloat64
			actCost      sql.NullFloat64
		)
		if err := rows.Scan(
			&id,
			&model,
			&billing,
			&startedAtRaw,
			&messageCount,
			&input,
			&output,
			&cacheRead,
			&cacheWrite,
			&reasoning,
			&estCost,
			&actCost,
		); err != nil {
			return nil, fmt.Errorf("hermes: scanning session row: %w", err)
		}

		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}

		in := nonNegativeInt64(input)
		outTok := nonNegativeInt64(output)
		cr := nonNegativeInt64(cacheRead)
		cw := nonNegativeInt64(cacheWrite)
		reason := nonNegativeInt64(reasoning)

		// Prefer actual_cost_usd; fall back to estimated.
		cost := 0.0
		hasCost := false
		if actCost.Valid && actCost.Float64 > 0 {
			cost = actCost.Float64
			hasCost = true
		} else if estCost.Valid && estCost.Float64 > 0 {
			cost = estCost.Float64
			hasCost = true
		}

		// Skip rows with no tokens AND no cost — they're dead weight.
		if in == 0 && outTok == 0 && cr == 0 && cw == 0 && reason == 0 && !hasCost {
			continue
		}

		ts, ok := convertStartedAt(startedAtRaw)
		if !ok {
			continue
		}

		out = append(out, hermesSession{
			ID:              strings.TrimSpace(id),
			Model:           model,
			Provider:        strings.TrimSpace(billing.String),
			StartedAt:       ts,
			MessageCount:    nonNegativeInt64(messageCount),
			InputTokens:     in,
			OutputTokens:    outTok,
			CacheReadTokens: cr,
			CacheWriteTok:   cw,
			ReasoningTokens: reason,
			CostUSD:         cost,
			HasCost:         hasCost,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hermes: iterating session rows: %w", err)
	}

	return out, nil
}

func columnOrNull(name string, present bool) string {
	if present {
		return name
	}
	return "NULL AS " + name
}

func nonNegativeInt64(v sql.NullInt64) int64 {
	if v.Valid && v.Int64 >= 0 {
		return v.Int64
	}
	return 0
}

// convertStartedAt accepts the REAL Unix timestamp Hermes stores in
// started_at. Hermes stores seconds (often fractional), but we defensively
// treat any value > 1e12 as already in milliseconds so external imports or
// future schema tweaks don't trip us up.
func convertStartedAt(v sql.NullFloat64) (time.Time, bool) {
	if !v.Valid {
		return time.Time{}, false
	}
	raw := v.Float64
	if raw <= 0 {
		return time.Time{}, false
	}
	var ms int64
	if raw > 1e12 {
		ms = int64(raw)
	} else {
		ms = int64(raw * 1000)
	}
	return time.UnixMilli(ms).UTC(), true
}
