package decide

// GateStep is a step of the fail-closed evaluation order that comes before target resolution (PERD §23.3, T1 to T6).
type GateStep int

// Steps T1 to T6. Each can end an evaluation, and none reads evidence about a target.
const (
	T1AdmittedIntent GateStep = iota + 1
	T2Session
	T3WritesEnabled
	T4Capability
	T5Scope
	T6TechnicalAuthority
)

type gateResult struct {
	outcome Outcome
	reason  string
	layers  Authority
}

var gateTable = map[GateStep]gateResult{
	T1AdmittedIntent:     {Reject, "INTENT_INVALID", Authority{NotEvaluated, NotEvaluated, NotEvaluated}},
	T2Session:            {Reject, "UNAUTHENTICATED", Authority{NotEvaluated, NotEvaluated, NotEvaluated}},
	T3WritesEnabled:      {Unknown, "WRITES_DISABLED", Authority{NotEvaluated, NotEvaluated, NotEvaluated}},
	T4Capability:         {Reject, "PROHIBITED_CAPABILITY", Authority{NotEvaluated, NotEvaluated, NotEvaluated}},
	T5Scope:              {Reject, "OUT_OF_SCOPE", Authority{NotEvaluated, NotEvaluated, NotEvaluated}},
	T6TechnicalAuthority: {Reject, "NOT_AUTHORIZED", Authority{Fail, NotEvaluated, NotEvaluated}},
}

// FailAtGate builds the one decision a failed gate step produces: the outcome and the core reason code are fixed by the
// step (PERD §23.3), so a caller cannot choose another. Gates T1 to T6 run before target resolution and come first in the
// fixed order, which is why they are not part of the resolved or unresolved intent types (REQ-TRU-002: refuse before read).
func FailAtGate(step GateStep, intentVersion int) (Decision, error) {
	g, ok := gateTable[step]
	if !ok {
		return Decision{}, ErrBadGateStep
	}
	r, err := Core(g.reason)
	if err != nil {
		return Decision{}, err
	}
	return build(g.outcome, intentVersion, g.layers, []ReasonCode{r})
}
