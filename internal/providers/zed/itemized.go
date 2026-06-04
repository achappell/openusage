package zed

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// ItemizedUsage returns one event per Zed thread, reusing the same SQLite
// query as Fetch. Zed records no cost, so HasCost is false and the report
// layer derives cost from tokens via pricing.
func (p *Provider) ItemizedUsage() ([]core.UsageEvent, error) {
	dbPath := resolveDBPath(core.AccountConfig{})
	if dbPath == "" {
		return nil, nil
	}
	threads, err := queryZedThreads(context.Background(), dbPath)
	if err != nil {
		return nil, err
	}
	out := make([]core.UsageEvent, 0, len(threads))
	for _, t := range threads {
		out = append(out, core.UsageEvent{
			Time:                t.Timestamp,
			ProviderID:          p.ID(),
			Model:               t.Model,
			Project:             t.Workspace,
			Session:             t.ThreadID,
			InputTokens:         int(t.Input),
			OutputTokens:        int(t.Output),
			CacheReadTokens:     int(t.CacheRead),
			CacheCreationTokens: int(t.CacheWrite),
			ReasoningTokens:     int(t.Reasoning),
		})
	}
	return out, nil
}
