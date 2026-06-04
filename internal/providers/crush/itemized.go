package crush

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// ItemizedUsage returns one event per Crush session, reusing the same SQLite
// queries as Fetch. Crush stores per-session aggregates, so each event
// represents a whole session (sufficient for the session and daily reports).
func (p *Provider) ItemizedUsage() ([]core.UsageEvent, error) {
	ctx := context.Background()
	var out []core.UsageEvent
	for _, dbPath := range resolveDBPaths(core.AccountConfig{}) {
		sessions, err := querySessions(ctx, dbPath)
		if err != nil {
			continue
		}
		for _, s := range sessions {
			out = append(out, core.UsageEvent{
				Time:         s.CreatedAt,
				ProviderID:   p.ID(),
				Model:        s.Model,
				Session:      s.ID,
				InputTokens:  int(s.PromptTokens),
				OutputTokens: int(s.CompletionTokens),
				CostUSD:      s.Cost,
				HasCost:      s.HasCost,
			})
		}
	}
	return out, nil
}
