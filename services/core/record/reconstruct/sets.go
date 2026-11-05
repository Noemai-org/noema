package reconstruct

// The closed sets a reconstruction fact is checked against (contracts/schemas/enums.v1.json). sets_test.go fails if
// they differ from the contract. CON-03 replaces them with generated types.
var (
	decisionOutcomes = setOf("ALLOW", "CLARIFY", "CONFIRM", "WAIT", "ESCALATE", "REJECT", "UNKNOWN")
	executionStates  = setOf("DISPATCHED", "ACKNOWLEDGED", "OUTCOME_UNKNOWN", "REJECTED_BY_TARGET", "REJECTED_STALE_VERSION", "FAILED_TERMINAL")
	verdicts         = setOf("ACHIEVED", "NOT_ACHIEVED", "PARTIALLY_ACHIEVED", "PENDING_EXTERNAL", "UNKNOWN", "COMPENSATED")
	factStatuses     = setOf("KNOWN", "INFERRED", "AMBIGUOUS", "UNAVAILABLE", "STALE", "CONTRADICTORY", "UNKNOWN", "UNAUTHORIZED")
)

func setOf(v ...string) map[string]bool {
	m := make(map[string]bool, len(v))
	for _, s := range v {
		m[s] = true
	}
	return m
}
