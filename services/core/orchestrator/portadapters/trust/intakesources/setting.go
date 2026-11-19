package intakesources

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/Noemai-org/noema/services/core/platform/ceilings"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
)

// EnvAtRiskBPS is the setting that gives the at-risk fraction of a clock: the share of its duration, in whole basis points, under which the clock is AT_RISK (ruling A345,
// relayed, not committed; PERD 23.5). The package reads no environment: the composition passes its lookup.
const EnvAtRiskBPS = "NOEMA_TRUST_AT_RISK_BPS"

// basisPointsPerWhole is the unit of a basis point (the BasisPoints of intent-slot.v2 run from 0 to this number): a fraction in basis points is that number over it. It is a unit,
// not a policy value; the value of the setting is the operator's, inside the bounds of ruling A233 (platform/ceilings).
const basisPointsPerWhole = 10000

// atRiskForm is a whole positive number: ASCII digits only, no sign, no space, no leading zero.
var atRiskForm = regexp.MustCompile(`\A[1-9][0-9]*\z`)

// AtRiskFromEnv reads the at-risk fraction from the setting, strictly, and gives it as the fraction the Clocks adapter takes. There is no default: a missing, empty or loose value,
// one that overflows, and one outside the bounds of ruling A233 (platform/ceilings: 1000 to 5000) is ErrSetting. The error names the setting and the bounds and never echoes the value.
func AtRiskFromEnv(getenv func(string) string) (clocks.Ratio, error) {
	fail := fmt.Errorf("%w: %s must be a whole number of basis points from %d to %d", ErrSetting, EnvAtRiskBPS, ceilings.MinClockAtRiskRemainingBP, ceilings.MaxClockAtRiskRemainingBP)
	if getenv == nil {
		return clocks.Ratio{}, fail
	}
	raw := getenv(EnvAtRiskBPS)
	if !atRiskForm.MatchString(raw) {
		return clocks.Ratio{}, fail
	}
	bps, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || bps < ceilings.MinClockAtRiskRemainingBP || bps > ceilings.MaxClockAtRiskRemainingBP {
		return clocks.Ratio{}, fail
	}
	return clocks.Ratio{Num: bps, Den: basisPointsPerWhole}, nil
}
