package decide

import "context"

// StepFunc is one step of the evaluation order. The caller supplies it; the step does its own reads through the ports it
// was built with, so Evaluate itself reads nothing (refuse before read is a property of the order, REQ-TRU-002).
type StepFunc func(ctx context.Context) StepResult

// Steps holds the fifteen step functions, T1 at index 0. A nil entry is a defect and fails closed at that step.
type Steps [T15DecisionRecorded]StepFunc

type stepKind int

const (
	kindContinue stepKind = iota + 1
	kindStop
	kindFail
)

// StepResult is what a step returns: continue to the next step, stop with a decision, or fail. The zero value is invalid.
type StepResult struct {
	kind     stepKind
	decision Decision
	err      error
}

// Continue lets the evaluation go on to the next step.
func Continue() StepResult { return StepResult{kind: kindContinue} }

// Stop ends the evaluation with d. Evaluate accepts d only if this step may produce its outcome (§23.3).
func Stop(d Decision) StepResult { return StepResult{kind: kindStop, decision: d} }

// FailStep reports that the step could not run (a dependency failure). The evaluation ends with UNKNOWN, never ALLOW (INV-16).
func FailStep(err error) StepResult { return StepResult{kind: kindFail, err: err} }

// Err is the step's error for a failed result, or nil.
func (r StepResult) Err() error { return r.err }

// TraceResult is what happened at one step. The trace holds step IDs and these values only, never free text (REQ-TRU-002).
type TraceResult string

// The trace results.
const (
	StepPassed        TraceResult = "PASSED"
	StepTerminal      TraceResult = "TERMINAL"
	StepFailed        TraceResult = "FAILED"
	StepPanicked      TraceResult = "PANICKED"
	StepDeadline      TraceResult = "DEADLINE"
	StepInvalidResult TraceResult = "INVALID_RESULT"
)

// TraceEntry is one entry of the evaluation trace.
type TraceEntry struct {
	Step   GateStep
	Result TraceResult
}

// stepOutcomes is §23.3's "terminal result" column: the outcomes each step may end an evaluation with. UNKNOWN is accepted
// at every step, because it never authorises anything; ALLOW only at T15, after the Decision Record is written.
var stepOutcomes = map[GateStep][]Outcome{
	T1AdmittedIntent:     {Reject},
	T2Session:            {Reject},
	T3WritesEnabled:      {},
	T4Capability:         {Reject},
	T5Scope:              {Reject},
	T6TechnicalAuthority: {Reject},
	T7TargetResolution:   {Clarify, Escalate},
	T8BusinessOwnership:  {Reject},
	T9Evidence:           {Escalate},
	T10Reconciliation:    {Escalate},
	T11Policy:            {Reject, Escalate, Confirm, Wait},
	T12Clocks:            {Escalate},
	T13Confirmation:      {Confirm},
	T14Plan:              {},
	T15DecisionRecorded:  {Allow},
}

// Evaluate runs T1 to T15 in the fixed order of §23.3 and stops at the first terminal result (REQ-TRU-002). It returns
// exactly one decision and the trace of the steps it reached. Every way a step can go wrong ends the evaluation with
// UNKNOWN INTERNAL_ERROR at that step: a panic, an error, a done context before the step or when it returns, a missing step function, an
// outcome the step may not produce, a decision for another intent version or one no constructor built, and a T15 that
// does not decide. None of them can yield ALLOW (INV-16, REQ-NFR-002).
func Evaluate(ctx context.Context, intentVersion int, steps Steps) (Decision, []TraceEntry) {
	trace := make([]TraceEntry, 0, len(steps))
	for i, fn := range steps {
		step := GateStep(i + 1)
		if ctx.Err() != nil {
			return internalError(intentVersion), append(trace, TraceEntry{step, StepDeadline})
		}
		if fn == nil {
			return internalError(intentVersion), append(trace, TraceEntry{step, StepInvalidResult})
		}
		res, panicked := runStep(ctx, fn)
		if ctx.Err() != nil {
			// The deadline passed while the step ran: whatever it returned, even a valid ALLOW, is not trusted (INV-16).
			return internalError(intentVersion), append(trace, TraceEntry{step, StepDeadline})
		}
		switch {
		case panicked:
			return internalError(intentVersion), append(trace, TraceEntry{step, StepPanicked})
		case res.kind == kindFail:
			return internalError(intentVersion), append(trace, TraceEntry{step, StepFailed})
		case res.kind == kindContinue && step != T15DecisionRecorded:
			trace = append(trace, TraceEntry{step, StepPassed})
		case res.kind == kindStop && accepts(step, res.decision, intentVersion):
			return res.decision, append(trace, TraceEntry{step, StepTerminal})
		default:
			return internalError(intentVersion), append(trace, TraceEntry{step, StepInvalidResult})
		}
	}
	return internalError(intentVersion), trace
}

// runStep calls fn and turns a panic into a flag. This is the Trust boundary handler of REQ-TRU-002.
func runStep(ctx context.Context, fn StepFunc) (res StepResult, panicked bool) {
	defer func() {
		if recover() != nil {
			res, panicked = StepResult{}, true
		}
	}()
	return fn(ctx), false
}

func accepts(step GateStep, d Decision, intentVersion int) bool {
	if d.intentVersion != intentVersion || len(d.reasons) == 0 && d.outcome != Allow && d.outcome != Confirm {
		return false
	}
	if d.outcome == Unknown {
		return true
	}
	for _, o := range stepOutcomes[step] {
		if o == d.outcome {
			return true
		}
	}
	return false
}

// internalError is the decision of the boundary handler (§23.3, INTERNAL_ERROR). It is built directly, not through
// build, because it must exist even when nothing else could be built; no authority layer was evaluated.
func internalError(intentVersion int) Decision {
	return Decision{
		outcome:       Unknown,
		reasons:       []ReasonCode{{code: "INTERNAL_ERROR"}},
		authority:     Authority{Technical: NotEvaluated, Policy: NotEvaluated, Business: NotEvaluated},
		intentVersion: intentVersion,
	}
}
