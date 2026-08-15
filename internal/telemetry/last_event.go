package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// LastEventTimes returns the most recent canonical event timestamp for each
// provider/account pair. Keys are "provider_id:account_id".
func (s *Store) LastEventTimes(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT provider_id, account_id, MAX(occurred_at)
		   FROM usage_events
		  WHERE provider_id IS NOT NULL AND provider_id != ''
		    AND event_type IN ('turn_completed', 'message_usage', 'tool_usage')
		  GROUP BY provider_id, account_id`)
	if err != nil {
		return nil, fmt.Errorf("telemetry: querying last event times: %w", err)
	}
	defer rows.Close()

	out := make(map[string]time.Time)
	for rows.Next() {
		var provider string
		var account sql.NullString
		var occurred sql.NullString
		if err := rows.Scan(&provider, &account, &occurred); err != nil {
			return nil, fmt.Errorf("telemetry: scanning last event time: %w", err)
		}
		if !occurred.Valid || strings.TrimSpace(occurred.String) == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, occurred.String)
		if err != nil {
			continue
		}
		acct := account.String
		if strings.TrimSpace(acct) == "" {
			acct = "default"
		}
		out[provider+":"+acct] = ts.UTC()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: iterating last event times: %w", err)
	}
	return out, nil
}
