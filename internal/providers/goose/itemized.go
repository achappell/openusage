package goose

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// ItemizedUsage returns one event per Goose session, reusing the same SQLite
// query as Fetch.
func (p *Provider) ItemizedUsage() ([]core.UsageEvent, error) {
	dbPath := resolveDBPath(core.AccountConfig{})
	if dbPath == "" {
		return nil, nil
	}
	res, err := queryGooseSessions(context.Background(), dbPath)
	if err != nil {
		return nil, err
	}
	out := make([]core.UsageEvent, 0, len(res.Sessions))
	for _, s := range res.Sessions {
		out = append(out, core.UsageEvent{
			Time:            s.CreatedAt,
			ProviderID:      p.ID(),
			Model:           s.Model,
			Session:         s.ID,
			InputTokens:     int(s.InputTokens),
			OutputTokens:    int(s.OutputTokens),
			ReasoningTokens: int(s.ReasoningTokens),
			CostUSD:         s.AccumulatedCost,
			HasCost:         s.HasCost,
		})
	}
	return out, nil
}
