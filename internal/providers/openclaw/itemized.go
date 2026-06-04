package openclaw

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// ItemizedUsage returns one event per recorded assistant turn, reusing the
// same transcript parsing as Fetch. It powers the headless session/blocks
// reports.
func (p *Provider) ItemizedUsage() ([]core.UsageEvent, error) {
	entries, err := readAllEntries(context.Background(), resolveAgentsDirs(core.AccountConfig{}))
	if err != nil {
		return nil, err
	}
	out := make([]core.UsageEvent, 0, len(entries))
	for _, e := range entries {
		out = append(out, core.UsageEvent{
			Time:                e.Timestamp,
			ProviderID:          p.ID(),
			Model:               e.Model,
			Session:             e.SessionID,
			InputTokens:         int(e.Input),
			OutputTokens:        int(e.Output),
			CacheReadTokens:     int(e.CacheRead),
			CacheCreationTokens: int(e.CacheWrite),
			CostUSD:             e.CostUSD,
			HasCost:             e.HasCost,
		})
	}
	return out, nil
}
