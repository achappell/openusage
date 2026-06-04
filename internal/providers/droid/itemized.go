package droid

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// ItemizedUsage returns one event per Droid session, reusing the same parsing
// as Fetch. Droid records no cost, so the report layer derives it from tokens.
func (p *Provider) ItemizedUsage() ([]core.UsageEvent, error) {
	dir := resolveSessionsDir(core.AccountConfig{})
	if dir == "" {
		return nil, nil
	}
	sessions, _, err := readAllSessions(context.Background(), dir)
	if err != nil {
		return nil, err
	}
	out := make([]core.UsageEvent, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, core.UsageEvent{
			Time:                s.Timestamp,
			ProviderID:          p.ID(),
			Model:               s.Model,
			Session:             s.SessionID,
			InputTokens:         int(s.Input),
			OutputTokens:        int(s.Output),
			CacheReadTokens:     int(s.CacheRead),
			CacheCreationTokens: int(s.CacheWrite),
			ReasoningTokens:     int(s.Thinking),
		})
	}
	return out, nil
}
