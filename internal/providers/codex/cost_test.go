package codex

import (
	"context"
	"math"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/pricing"
)

func TestEstimateUsageCost_UsesResolver(t *testing.T) {
	prev := priceLookup
	priceLookup = func(_ context.Context, _ string, _ int) (*pricing.Price, error) {
		return &pricing.Price{
			ModelID:              "stub",
			Source:               pricing.SourceHardcoded,
			InputCostPerMillion:  2.0,
			OutputCostPerMillion: 8.0,
		}, nil
	}
	t.Cleanup(func() { priceLookup = prev })

	// 1M input @ $2 + 100k output @ $8 = 2.0 + 0.8 = 2.8
	delta := tokenUsage{
		InputTokens:  1_000_000,
		OutputTokens: 100_000,
		TotalTokens:  1_100_000,
	}
	got := estimateUsageCost("gpt-5-codex", delta)
	want := 2.8
	if math.Abs(got-want) > 0.001 {
		t.Errorf("estimateUsageCost = %.4f, want %.4f", got, want)
	}
}

func TestEstimateUsageCost_ResolverErrorReturnsZero(t *testing.T) {
	// TestMain installs an erroring stub already, so this just confirms the
	// no-price path returns 0 instead of crashing.
	delta := tokenUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150}
	if got := estimateUsageCost("anything", delta); got != 0 {
		t.Errorf("estimateUsageCost on resolver error = %f, want 0", got)
	}
}

func TestEstimateUsageCost_ZeroDeltaReturnsZero(t *testing.T) {
	if got := estimateUsageCost("anything", tokenUsage{}); got != 0 {
		t.Errorf("estimateUsageCost on zero delta = %f, want 0", got)
	}
}

// TestEstimateUsageCost_PassesContextLenForTier locks in the cost-accuracy fix:
// the per-message context length is input_tokens (which already includes the
// cached slice), not input+cached. See issue #360.
func TestEstimateUsageCost_PassesContextLenForTier(t *testing.T) {
	prev := priceLookup
	var gotCtx int
	aboveInput := 0.5
	priceLookup = func(_ context.Context, _ string, ctxLen int) (*pricing.Price, error) {
		gotCtx = ctxLen
		return &pricing.Price{
			Source:               pricing.SourceHardcoded,
			InputCostPerMillion:  1.0,
			OutputCostPerMillion: 2.0,
			Tiers:                pricing.TierOverrides{Above200k: &pricing.TierRates{InputCostPerMillion: &aboveInput}},
		}, nil
	}
	t.Cleanup(func() { priceLookup = prev })

	delta := tokenUsage{InputTokens: 250_000, CachedInputTokens: 50_000, TotalTokens: 300_000}
	got := estimateUsageCost("gpt-5-codex", delta)
	if gotCtx != 250_000 {
		t.Fatalf("ctxLen passed = %d, want 250000", gotCtx)
	}
	// Only the uncached slice (200k) is billed at the input rate; the 50k
	// cached slice is billed at cache-read (0 here). With ctxLen 250k the
	// >200k tier (0.5/M) applies to the uncached 200k.
	want := 200_000 * aboveInput / 1_000_000
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("estimateUsageCost = %.6f, want %.6f", got, want)
	}
}

func TestEstimateUsageCost_DoesNotDoubleBillCachedInput(t *testing.T) {
	prev := priceLookup
	priceLookup = func(_ context.Context, _ string, _ int) (*pricing.Price, error) {
		return &pricing.Price{
			Source:                  pricing.SourceHardcoded,
			InputCostPerMillion:     1.10,
			CacheReadCostPerMillion: 0.55,
			OutputCostPerMillion:    4.40,
		}, nil
	}
	t.Cleanup(func() { priceLookup = prev })

	// 1M input of which 900k is cached, 100k output — the repo's own
	// fixture shape where input includes cached (total = input+output).
	delta := tokenUsage{InputTokens: 1_000_000, CachedInputTokens: 900_000, OutputTokens: 100_000, TotalTokens: 1_100_000}
	got := estimateUsageCost("o3-mini", delta)
	// expected: 100k uncached @1.10 + 900k cached @0.55 + 100k output @4.40
	want := 100_000*1.10/1_000_000 + 900_000*0.55/1_000_000 + 100_000*4.40/1_000_000
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("double-bill: estimateUsageCost = %.6f, want %.6f", got, want)
	}
}
