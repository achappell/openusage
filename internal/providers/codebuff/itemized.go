package codebuff

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// ItemizedUsage returns one event per recorded assistant turn, reusing the
// same chat-file parsing as Fetch. It powers the headless session/blocks
// reports. Cost comes from the recorded credits when present.
func (p *Provider) ItemizedUsage() ([]core.UsageEvent, error) {
	entries, err := readAllChats(context.Background(), resolveDataDirs(core.AccountConfig{}))
	if err != nil {
		return nil, err
	}
	out := make([]core.UsageEvent, 0, len(entries))
	for _, e := range entries {
		out = append(out, core.UsageEvent{
			Time:                e.Timestamp,
			ProviderID:          p.ID(),
			Model:               e.Model,
			Project:             e.Project,
			Session:             e.ChatID,
			InputTokens:         int(e.Input),
			OutputTokens:        int(e.Output),
			CacheReadTokens:     int(e.CacheRead),
			CacheCreationTokens: int(e.CacheWrite),
			CostUSD:             e.Credits,
			HasCost:             e.HasCredits,
		})
	}
	return out, nil
}
