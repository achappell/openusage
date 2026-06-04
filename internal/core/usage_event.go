package core

import "time"

// UsageEvent is a single assistant turn (or the smallest billable unit a
// provider can attribute to a point in time) extracted from a provider's local
// data. It is the substrate for the headless session/blocks/daily reports.
//
// Providers expose these by implementing ItemizedUsageProvider. Token-only
// providers leave HasCost false; the report layer fills the cost from tokens.
type UsageEvent struct {
	Time       time.Time
	ProviderID string
	Model      string
	Project    string
	Session    string

	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	ReasoningTokens     int

	CostUSD float64
	HasCost bool // true when CostUSD came from the source (vs. needs computing)
}

// ItemizedUsageProvider is an optional capability: a provider that can return
// its usage as a list of timestamped per-turn (or per-session) events. The
// report subcommands use this to build session and blocks reports for
// providers that do not implement the telemetry-source interface.
//
// Implementations read their default on-disk locations and return events
// sorted or unsorted; the report layer sorts as needed.
type ItemizedUsageProvider interface {
	ItemizedUsage() ([]UsageEvent, error)
}
