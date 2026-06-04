package report

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// FromTelemetry maps a provider's per-turn telemetry events into report events.
// It is the uniform itemized path for every provider that implements
// shared.TelemetrySource (Collect reads the local logs and returns one event
// per assistant turn with a timestamp, model, session and token usage), which
// unlocks session/blocks/breakdown for them without bespoke parsing.
//
// Cost comes from the event's own CostUSD when the source recorded one;
// otherwise it is computed from tokens via cost (nil cost leaves it at 0).
func FromTelemetry(events []shared.TelemetryEvent, providerID string, cost CostFunc) []Event {
	out := make([]Event, 0, len(events))
	for _, e := range events {
		if e.EventType != shared.TelemetryEventTypeMessageUsage {
			continue
		}
		in := derefInt(e.InputTokens)
		out0 := derefInt(e.OutputTokens)
		cr := derefInt(e.CacheReadTokens)
		cc := derefInt(e.CacheWriteTokens)
		re := derefInt(e.ReasoningTokens)

		c := 0.0
		if e.CostUSD != nil {
			c = *e.CostUSD
		} else if cost != nil {
			c = cost(e.ModelRaw, in, out0, cr, cc, re)
		}

		if in+out0+cr+cc+re == 0 && c == 0 {
			continue
		}

		pid := providerID
		if pid == "" {
			pid = e.ProviderID
		}
		out = append(out, Event{
			Time:        e.OccurredAt,
			Provider:    pid,
			Model:       e.ModelRaw,
			Project:     e.WorkspaceID,
			Session:     e.SessionID,
			Input:       in,
			Output:      out0,
			CacheRead:   cr,
			CacheCreate: cc,
			Reasoning:   re,
			Cost:        c,
		})
	}
	return out
}

func derefInt(p *int64) int {
	if p == nil {
		return 0
	}
	return int(*p)
}

// FromItemized maps a provider's itemized usage events (from
// core.ItemizedUsageProvider) into report events. Cost comes from the event
// when the source recorded one; otherwise it is computed from tokens via cost.
func FromItemized(events []core.UsageEvent, cost CostFunc) []Event {
	out := make([]Event, 0, len(events))
	for _, e := range events {
		c := e.CostUSD
		if !e.HasCost {
			if cost != nil {
				c = cost(e.Model, e.InputTokens, e.OutputTokens, e.CacheReadTokens, e.CacheCreationTokens, e.ReasoningTokens)
			} else {
				c = 0
			}
		}
		if e.InputTokens+e.OutputTokens+e.CacheReadTokens+e.CacheCreationTokens+e.ReasoningTokens == 0 && c == 0 {
			continue
		}
		out = append(out, Event{
			Time:        e.Time,
			Provider:    e.ProviderID,
			Model:       e.Model,
			Project:     e.Project,
			Session:     e.Session,
			Input:       e.InputTokens,
			Output:      e.OutputTokens,
			CacheRead:   e.CacheReadTokens,
			CacheCreate: e.CacheCreationTokens,
			Reasoning:   e.ReasoningTokens,
			Cost:        c,
		})
	}
	return out
}
