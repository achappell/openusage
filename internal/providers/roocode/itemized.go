package roocode

import (
	"github.com/janekbaraniewski/openusage/internal/core"
)

// ItemizedExtension returns one event per recorded API call across every task
// directory for the given VS Code extension. It is shared by the Roo Code and
// Kilo Code providers (which differ only in extension subdir / client).
func ItemizedExtension(providerID, extensionSubdir, client string) ([]core.UsageEvent, error) {
	var out []core.UsageEvent
	for _, taskDir := range FindTaskDirs(extensionSubdir) {
		evt, err := ParseTaskDir(taskDir, client)
		if err != nil || evt == nil {
			continue
		}
		for _, c := range evt.Calls {
			model := c.Model
			if model == "" {
				model = evt.Model
			}
			out = append(out, core.UsageEvent{
				Time:                c.Timestamp,
				ProviderID:          providerID,
				Model:               model,
				Session:             evt.TaskID,
				InputTokens:         int(c.TokensIn),
				OutputTokens:        int(c.TokensOut),
				CacheReadTokens:     int(c.CacheReads),
				CacheCreationTokens: int(c.CacheWrites),
				CostUSD:             c.Cost,
				HasCost:             true,
			})
		}
	}
	return out, nil
}

// ItemizedUsage implements core.ItemizedUsageProvider for Roo Code.
func (p *Provider) ItemizedUsage() ([]core.UsageEvent, error) {
	return ItemizedExtension(p.ID(), RooExtensionSubdir, ClientRooCode)
}
