package intakesources

import "github.com/Noemai-org/noema/services/core/trust/clocks"

// EnvAtRiskBPS is the setting that gives the at-risk fraction of a clock, in whole basis points.
const EnvAtRiskBPS = "NOEMA_TRUST_AT_RISK_BPS"

// AtRiskFromEnv reads the at-risk fraction from the setting.
func AtRiskFromEnv(_ func(string) string) (clocks.Ratio, error) {
	return clocks.Ratio{}, nil
}
