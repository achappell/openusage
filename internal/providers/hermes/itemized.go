package hermes

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// ItemizedUsage returns one event per Hermes session, reusing the same SQLite
// query as Fetch.
func (p *Provider) ItemizedUsage() ([]core.UsageEvent, error) {
	dbPath := resolveDBPath(core.AccountConfig{})
	if dbPath == "" {
		return nil, nil
	}
	sessions, err := queryHermesSessions(context.Background(), dbPath)
	if err != nil {
		return nil, err
	}
	out := make([]core.UsageEvent, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, core.UsageEvent{
			Time:                s.StartedAt,
			ProviderID:          p.ID(),
			Model:               s.Model,
			Session:             s.ID,
			InputTokens:         int(s.InputTokens),
			OutputTokens:        int(s.OutputTokens),
			CacheReadTokens:     int(s.CacheReadTokens),
			CacheCreationTokens: int(s.CacheWriteTokens),
			ReasoningTokens:     int(s.ReasoningTokens),
			CostUSD:             s.CostUSD,
			HasCost:             s.HasCost,
		})
	}
	return out, nil
}
