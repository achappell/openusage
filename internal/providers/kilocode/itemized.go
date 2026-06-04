package kilocode

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/roocode"
)

// ItemizedUsage implements core.ItemizedUsageProvider for Kilo Code, reusing
// the shared Roo Code task parser with the Kilo extension subdir/client.
func (p *Provider) ItemizedUsage() ([]core.UsageEvent, error) {
	return roocode.ItemizedExtension(p.ID(), roocode.KiloExtensionSubdir, roocode.ClientKiloCode)
}
