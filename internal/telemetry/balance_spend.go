package telemetry

import (
	"context"
	"time"
)

// WindowedSpendResult is the outcome of deriving spend over a window from the
// observed balance series.
type WindowedSpendResult struct {
	Spend float64 // money spent within the window, in the metric's unit
	// TopUps is the total of positive balance increases within the window
	// (deposits), only meaningful for balance-semantics metrics.
	TopUps float64
	// Partial is true when our observation history does not cover the full
	// window (we began observing after `since`), so Spend reflects only the
	// portion we have data for.
	Partial bool
	// Since is the earliest observation we actually used; callers surface it
	// when Partial so the user knows the real coverage.
	Since time.Time
	// Unit is the metric's currency/unit, carried through for display.
	Unit string
	// OK is false when no spend figure could be derived (no data, or a
	// limit-semantics metric that carries no spend signal).
	OK bool
}

// WindowedSpend derives true spend over [since, now] from our own observed
// balance series, independent of whatever window (if any) the provider's API
// exposes.
//
//   - cumulative: spend = used(latest) − used(anchor at-or-before since). When
//     no observation predates `since`, anchor on the earliest known row and mark
//     the result Partial.
//   - balance: walk observations from `since` forward and sum each step's drop
//     in remaining; rises are top-ups (excluded from spend, summed into TopUps).
//   - limit: no spend signal → OK=false.
func (s *Store) WindowedSpend(ctx context.Context, providerID, accountID, metricKey, semantics string, since time.Time) (WindowedSpendResult, error) {
	switch semantics {
	case balanceSemanticsCumulative:
		return s.windowedSpendCumulative(ctx, providerID, accountID, metricKey, since)
	case balanceSemanticsBalance:
		return s.windowedSpendBalance(ctx, providerID, accountID, metricKey, since)
	default:
		return WindowedSpendResult{}, nil
	}
}

func (s *Store) windowedSpendCumulative(ctx context.Context, providerID, accountID, metricKey string, since time.Time) (WindowedSpendResult, error) {
	latest, ok, err := s.latestBalanceObservation(ctx, providerID, accountID, metricKey)
	if err != nil || !ok || latest.Used == nil {
		return WindowedSpendResult{}, err
	}

	anchor, ok, err := s.balanceObservationAtOrBefore(ctx, providerID, accountID, metricKey, since)
	if err != nil {
		return WindowedSpendResult{}, err
	}
	partial := false
	if !ok || anchor.Used == nil {
		// No reading at/before the window start — fall back to the earliest
		// known row and report partial coverage.
		anchor, ok, err = s.earliestBalanceObservation(ctx, providerID, accountID, metricKey)
		if err != nil || !ok || anchor.Used == nil {
			return WindowedSpendResult{}, err
		}
		partial = anchor.ObservedAt.After(since)
	}

	spend := *latest.Used - *anchor.Used
	if spend < 0 {
		// Counter reset (e.g. new billing cycle wiped total_usage). Treat the
		// post-reset cumulative as the spend we can attribute to the window.
		spend = *latest.Used
		partial = true
	}
	return WindowedSpendResult{
		Spend:   spend,
		Partial: partial,
		Since:   anchor.ObservedAt,
		Unit:    latest.Unit,
		OK:      true,
	}, nil
}

func (s *Store) windowedSpendBalance(ctx context.Context, providerID, accountID, metricKey string, since time.Time) (WindowedSpendResult, error) {
	series, err := s.balanceObservationsSince(ctx, providerID, accountID, metricKey, since)
	if err != nil {
		return WindowedSpendResult{}, err
	}
	if len(series) == 0 {
		return WindowedSpendResult{}, nil
	}

	// Anchor the walk on the last reading at/before the window start so spend
	// that happened between that reading and the first in-window reading is
	// counted. When none exists, the walk starts at the first in-window row and
	// the result is partial.
	partial := false
	var prev *float64
	if anchor, ok, aerr := s.balanceObservationAtOrBefore(ctx, providerID, accountID, metricKey, since); aerr == nil && ok && anchor.Remaining != nil {
		prev = anchor.Remaining
	} else {
		partial = series[0].ObservedAt.After(since)
	}

	var spend, topups float64
	earliest := series[0].ObservedAt
	for _, o := range series {
		if o.Remaining == nil {
			continue
		}
		if prev != nil {
			if delta := *prev - *o.Remaining; delta > 0 {
				spend += delta
			} else if delta < 0 {
				topups += -delta
			}
		}
		r := *o.Remaining
		prev = &r
	}
	return WindowedSpendResult{
		Spend:   spend,
		TopUps:  topups,
		Partial: partial,
		Since:   earliest,
		Unit:    series[len(series)-1].Unit,
		OK:      true,
	}, nil
}
