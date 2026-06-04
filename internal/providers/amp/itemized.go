package amp

import (
	"github.com/janekbaraniewski/openusage/internal/core"
)

// ItemizedUsage returns one event per reconciled assistant message, reusing the
// same thread + ledger reconciliation as Fetch. Cost is the ledger credit cost
// (amp's own unit), so HasCost is always set to keep the report layer from
// recomputing it from token rates.
func (p *Provider) ItemizedUsage() ([]core.UsageEvent, error) {
	dataDir := resolveDataDir(ampDataDirOverride(core.AccountConfig{}))
	if dataDir == "" {
		return nil, nil
	}
	threadsDir := resolveThreadsDir(core.AccountConfig{}, dataDir)
	ledgerPath := resolveLedgerPath(core.AccountConfig{}, dataDir)

	threadFiles, err := listThreadFiles(threadsDir)
	if err != nil {
		return nil, err
	}
	ledger, _, _ := loadLedgerRecords(ledgerPath)

	var allEvents []ampEvent
	for _, path := range threadFiles {
		events, err := parseAmpThreadFile(path)
		if err != nil {
			continue
		}
		allEvents = append(allEvents, reconcileEventsOnly(events, ledger)...)
	}
	deduped := dedupAndMerge(allEvents)

	out := make([]core.UsageEvent, 0, len(deduped))
	for _, e := range deduped {
		t := e.Tokens.normalised()
		out = append(out, core.UsageEvent{
			Time:                e.Timestamp,
			ProviderID:          p.ID(),
			Model:               e.Model,
			Session:             e.ThreadID,
			InputTokens:         int(t.Input),
			OutputTokens:        int(t.Output),
			CacheReadTokens:     int(t.CacheRead),
			CacheCreationTokens: int(t.CacheWrite),
			CostUSD:             e.CreditCost,
			HasCost:             true,
		})
	}
	return out, nil
}
