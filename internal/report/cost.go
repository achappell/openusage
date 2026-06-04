package report

import (
	"context"
	"time"

	"github.com/janekbaraniewski/openusage/internal/pricing"
)

// CostFunc resolves a USD cost for a single usage record. It is injected so the
// report layer stays testable and so callers can pick offline vs live pricing.
type CostFunc func(model string, input, output, cacheRead, cacheCreate, reasoning int) float64

// pricingLookupTimeout bounds a single dynamic pricing query.
const pricingLookupTimeout = 2 * time.Second

// PricingCost returns a CostFunc backed by the shared pricing resolver. It
// forwards the request context length (input + cache tokens) so long-context
// tier rates apply. When offline is set it skips network lookups entirely and
// returns 0 (the resolver has no embedded table for non-Claude models), so
// token-only providers report $0 offline rather than stalling.
func PricingCost(offline bool) CostFunc {
	if offline {
		return func(string, int, int, int, int, int) float64 { return 0 }
	}
	return func(model string, input, output, cacheRead, cacheCreate, reasoning int) float64 {
		if model == "" {
			return 0
		}
		ctxLen := input + cacheRead + cacheCreate
		ctx, cancel := context.WithTimeout(context.Background(), pricingLookupTimeout)
		defer cancel()
		p, err := pricing.DefaultResolver().Lookup(ctx, model, ctxLen)
		if err != nil || p == nil {
			return 0
		}
		return pricing.Estimate(p, ctxLen, pricing.Usage{
			InputTokens:      input,
			OutputTokens:     output,
			CacheReadTokens:  cacheRead,
			CacheWriteTokens: cacheCreate,
			ReasoningTokens:  reasoning,
		})
	}
}
