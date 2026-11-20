package intakesources

import (
	"github.com/Noemai-org/noema/services/core/platform/trustlimits"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
)

// AtRiskFromLimits is not built yet (RED stub of the A447 source).
func AtRiskFromLimits(trustlimits.Limits) (clocks.Ratio, error) { return clocks.Ratio{}, nil }
