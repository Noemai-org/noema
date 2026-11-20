package intakesources

import (
	"github.com/Noemai-org/noema/services/core/platform/trustlimits"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
)

// basisPointsPerWhole is the unit of a basis point (the BasisPoints of intent-slot.v2 run from 0 to this number): a fraction in basis points is that number over it. It is a
// unit, not a policy value and not a bound: the bounds of the value are those of ruling A233, held and checked by platform/trustlimits when it loads the document.
const basisPointsPerWhole = 10000

// AtRiskFromLimits gives the at-risk fraction of a clock: clock_at_risk_remaining_bp of the pack's trust-limits document over ten thousand (ruling A447, which corrects A345 S4;
// A416; PERD 23.5, REQ-TRU-006). The value has this one source and no other: there is no setting and no default, and the bounds are not checked again here, because the package that
// owns them, platform/trustlimits, refuses a document outside them when it loads it. A Limits that was not loaded has no value (trustlimits.ErrNotLoaded) and is ErrConfig with
// no fraction: a zero fraction would mean a clock is never AT_RISK.
func AtRiskFromLimits(l trustlimits.Limits) (clocks.Ratio, error) {
	bps, err := l.ClockAtRiskRemainingBP()
	if err != nil {
		return clocks.Ratio{}, wrapped(ErrConfig, err)
	}
	return clocks.Ratio{Num: int64(bps), Den: basisPointsPerWhole}, nil
}
