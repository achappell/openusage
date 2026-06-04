package kiro

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// ItemizedUsage returns one event per Kiro conversation, reusing the same DB
// and file parsing as Fetch. Conversations can appear in both the SQLite store
// and on-disk session files, so they are deduplicated by key. Kiro records no
// cost, so the report layer derives it from tokens.
func (p *Provider) ItemizedUsage() ([]core.UsageEvent, error) {
	ctx := context.Background()
	seen := map[string]bool{}
	var out []core.UsageEvent

	add := func(convs []kiroConversation) {
		for _, c := range convs {
			key := c.Key
			if key == "" {
				key = c.ConversationID
			}
			if key != "" {
				if seen[key] {
					continue
				}
				seen[key] = true
			}
			out = append(out, core.UsageEvent{
				Time:         c.UpdatedAt,
				ProviderID:   p.ID(),
				Model:        c.Model,
				Project:      c.Workspace,
				Session:      c.ConversationID,
				InputTokens:  int(c.InputTokens),
				OutputTokens: int(c.OutputTokens),
			})
		}
	}

	if dir := resolveSessionsDir(core.AccountConfig{}); dir != "" {
		if convs, err := readKiroFileSessions(ctx, dir); err == nil {
			add(convs)
		}
	}
	if dbPath := resolveDBPath(core.AccountConfig{}); dbPath != "" {
		if convs, err := queryKiroConversations(ctx, dbPath); err == nil {
			add(convs)
		}
	}
	return out, nil
}
