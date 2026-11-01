package decide_test

// TRU-02: the fail-closed gate order T1 to T15 (PERD §23.3), its evaluation trace, and any panic becoming UNKNOWN
// (REQ-TRU-002, REQ-NFR-002, INV-16). The steps here are test doubles of the step functions: what is under test is the
// runner's order, its stopping rule, its outcome table and its fail-closed handling, not the steps' own logic.

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

const version = 3

func resolved() decide.ResolvedIntent {
	r, ok := decide.Admit(decide.IntentView{Version: version, RequiredSlots: map[string]decide.SlotStatus{"subject": decide.SlotResolved}}).Resolved()
	if !ok {
		panic("fixture: intent not resolved")
	}
	return r
}

func unresolved() decide.UnresolvedIntent {
	u, ok := decide.Admit(decide.IntentView{Version: version, MaterialFlags: []string{"AMBIGUOUS_COUNTERPARTY"}}).Unresolved()
	if !ok {
		panic("fixture: intent not unresolved")
	}
	return u
}

func reason(code string) decide.ReasonCode { return must(decide.Core(code)) }

// decisionFor builds a valid decision of each outcome, so a step double can stop with it.
func decisionFor(o decide.Outcome) decide.Decision {
	r := resolved()
	switch o {
	case decide.Allow:
		return must(r.Allow(allPass))
	case decide.Confirm:
		return must(r.Confirm(allPass))
	case decide.Wait:
		return must(r.Wait(allPass, 1_700_000_000, reason("RATE_LIMITED")))
	case decide.Escalate:
		return must(r.Escalate(allPass, reason("INSUFFICIENT_EVIDENCE")))
	case decide.Reject:
		return must(r.Reject(allPass, reason("NO_PERMITTING_RULE")))
	case decide.Unknown:
		return must(r.Unknown(allPass, reason("PLAN_INVALID")))
	case decide.Clarify:
		return must(unresolved().Clarify(decide.Clarification{}, reason("AMBIGUOUS_COUNTERPARTY")))
	}
	panic("fixture: unknown outcome " + string(o))
}

var outcomes = []decide.Outcome{decide.Allow, decide.Clarify, decide.Confirm, decide.Wait, decide.Escalate, decide.Reject, decide.Unknown}

func continueAll() decide.Steps {
	var s decide.Steps
	for i := range s {
		s[i] = func(context.Context) decide.StepResult { return decide.Continue() }
	}
	s[decide.T15DecisionRecorded-1] = func(context.Context) decide.StepResult { return decide.Stop(decisionFor(decide.Allow)) }
	return s
}

func isInternalError(d decide.Decision) bool {
	rs := d.Reasons()
	return d.Outcome() == decide.Unknown && len(rs) == 1 && rs[0].String() == "INTERNAL_ERROR"
}

// REQ-TRU-002: the order is fixed and the trace lists every step reached, in order, with its result.
func TestEvaluateRunsTheFifteenStepsInOrderAndTracesThem(t *testing.T) {
	var order []decide.GateStep
	s := continueAll()
	for i := range s {
		step, inner := decide.GateStep(i+1), s[i]
		s[i] = func(ctx context.Context) decide.StepResult { order = append(order, step); return inner(ctx) }
	}
	d, trace := decide.Evaluate(context.Background(), version, s)
	if d.Outcome() != decide.Allow {
		t.Fatalf("outcome %s, want ALLOW", d.Outcome())
	}
	want := make([]decide.GateStep, 15)
	for i := range want {
		want[i] = decide.GateStep(i + 1)
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	if len(trace) != 15 {
		t.Fatalf("trace has %d entries, want 15", len(trace))
	}
	for i, e := range trace {
		wantResult := decide.StepPassed
		if i == 14 {
			wantResult = decide.StepTerminal
		}
		if e.Step != decide.GateStep(i+1) || e.Result != wantResult {
			t.Fatalf("trace[%d] = %+v, want {%d %s}", i, e, i+1, wantResult)
		}
	}
}

// REQ-TRU-002 acceptance, US-10: a REJECT at T4 to T6 happens before any evidence read. The read counter stands in for
// EVIDENCE_READ: steps T7 to T15 are the only ones that read (§23.3, "Reads evidence?").
func TestRejectAtT4ToT6HappensBeforeAnyRead(t *testing.T) {
	for _, gate := range []decide.GateStep{decide.T4Capability, decide.T5Scope, decide.T6TechnicalAuthority} {
		reads := 0
		s := continueAll()
		for i := decide.T7TargetResolution; i <= decide.T15DecisionRecorded; i++ {
			s[i-1] = func(context.Context) decide.StepResult { reads++; return decide.Continue() }
		}
		g := gate
		s[g-1] = func(context.Context) decide.StepResult { return decide.Stop(must(decide.FailAtGate(g, version))) }
		d, trace := decide.Evaluate(context.Background(), version, s)
		if d.Outcome() != decide.Reject || reads != 0 {
			t.Fatalf("%d: outcome %s with %d reads, want REJECT with 0", gate, d.Outcome(), reads)
		}
		if last := trace[len(trace)-1]; last.Step != gate || last.Result != decide.StepTerminal || len(trace) != int(gate) {
			t.Fatalf("%d: trace %+v does not end at the gate", gate, trace)
		}
	}
}

// REQ-TRU-002 failure behaviour: a panic in any step is caught at the boundary and becomes UNKNOWN INTERNAL_ERROR.
func TestAPanicInAnyStepBecomesUnknownInternalError(t *testing.T) {
	for step := decide.T1AdmittedIntent; step <= decide.T15DecisionRecorded; step++ {
		s := continueAll()
		s[step-1] = func(context.Context) decide.StepResult { panic("injected") }
		d, trace := decide.Evaluate(context.Background(), version, s)
		if !isInternalError(d) {
			t.Fatalf("panic at %d gave %s %v", step, d.Outcome(), d.Reasons())
		}
		if last := trace[len(trace)-1]; last.Step != step || last.Result != decide.StepPanicked {
			t.Fatalf("panic at %d: trace ends %+v", step, last)
		}
	}
}

// INV-16, REQ-NFR-002: a dependency failure at any step never yields ALLOW.
func TestAnErrorInAnyStepBecomesUnknown(t *testing.T) {
	for step := decide.T1AdmittedIntent; step <= decide.T15DecisionRecorded; step++ {
		s := continueAll()
		s[step-1] = func(context.Context) decide.StepResult { return decide.FailStep(errors.New("dependency down")) }
		d, trace := decide.Evaluate(context.Background(), version, s)
		if !isInternalError(d) {
			t.Fatalf("error at %d gave %s %v", step, d.Outcome(), d.Reasons())
		}
		if last := trace[len(trace)-1]; last.Step != step || last.Result != decide.StepFailed {
			t.Fatalf("error at %d: trace ends %+v", step, last)
		}
	}
}

// A context that is done stops the evaluation before the next step runs; it never runs on to ALLOW.
func TestADoneContextStopsBeforeTheNextStep(t *testing.T) {
	for step := decide.T1AdmittedIntent; step <= decide.T15DecisionRecorded; step++ {
		ctx, cancel := context.WithCancel(context.Background())
		ran := false
		s := continueAll()
		if step > 1 {
			prev := step - 1
			s[prev-1] = func(context.Context) decide.StepResult { cancel(); return decide.Continue() }
		} else {
			cancel()
		}
		inner := s[step-1]
		s[step-1] = func(c context.Context) decide.StepResult { ran = true; return inner(c) }
		d, trace := decide.Evaluate(ctx, version, s)
		cancel()
		if ran || !isInternalError(d) {
			t.Fatalf("cancelled before %d: ran=%v outcome %s", step, ran, d.Outcome())
		}
		if last := trace[len(trace)-1]; last.Step != step || last.Result != decide.StepDeadline {
			t.Fatalf("cancelled before %d: trace ends %+v", step, last)
		}
	}
}

// §23.3: each step may end the evaluation only with the outcomes its row names, or with UNKNOWN; ALLOW only at T15.
// Anything else is a defect and becomes UNKNOWN INTERNAL_ERROR.
var permitted = map[decide.GateStep][]decide.Outcome{
	decide.T1AdmittedIntent:     {decide.Reject},
	decide.T2Session:            {decide.Reject},
	decide.T3WritesEnabled:      {},
	decide.T4Capability:         {decide.Reject},
	decide.T5Scope:              {decide.Reject},
	decide.T6TechnicalAuthority: {decide.Reject},
	decide.T7TargetResolution:   {decide.Clarify, decide.Escalate},
	decide.T8BusinessOwnership:  {decide.Reject},
	decide.T9Evidence:           {decide.Escalate},
	decide.T10Reconciliation:    {decide.Escalate},
	decide.T11Policy:            {decide.Reject, decide.Escalate, decide.Confirm, decide.Wait},
	decide.T12Clocks:            {decide.Escalate},
	decide.T13Confirmation:      {decide.Confirm},
	decide.T14Plan:              {},
	decide.T15DecisionRecorded:  {decide.Allow},
}

func allowedAt(step decide.GateStep, o decide.Outcome) bool {
	if o == decide.Unknown {
		return true
	}
	for _, p := range permitted[step] {
		if p == o {
			return true
		}
	}
	return false
}

func TestEachStepMayStopOnlyWithTheOutcomesOfItsRow(t *testing.T) {
	for step := decide.T1AdmittedIntent; step <= decide.T15DecisionRecorded; step++ {
		for _, o := range outcomes {
			s := continueAll()
			want := decisionFor(o)
			s[step-1] = func(context.Context) decide.StepResult { return decide.Stop(want) }
			d, trace := decide.Evaluate(context.Background(), version, s)
			last := trace[len(trace)-1]
			if allowedAt(step, o) {
				if d.Outcome() != o || last.Result != decide.StepTerminal {
					t.Fatalf("%d stopping with %s gave %s (%s)", step, o, d.Outcome(), last.Result)
				}
				continue
			}
			if !isInternalError(d) || last.Result != decide.StepInvalidResult {
				t.Fatalf("%d stopping with %s must be refused, gave %s (%s)", step, o, d.Outcome(), last.Result)
			}
		}
	}
}

// A run that finishes T15 without a decision is a defect, never an implicit ALLOW.
func TestNoDecisionAfterT15IsUnknown(t *testing.T) {
	s := continueAll()
	s[decide.T15DecisionRecorded-1] = func(context.Context) decide.StepResult { return decide.Continue() }
	if d, _ := decide.Evaluate(context.Background(), version, s); !isInternalError(d) {
		t.Fatalf("no decision gave %s", d.Outcome())
	}
}

// A missing step function is a defect: it fails closed at that step instead of being skipped.
func TestAMissingStepFailsClosed(t *testing.T) {
	for step := decide.T1AdmittedIntent; step <= decide.T15DecisionRecorded; step++ {
		s := continueAll()
		s[step-1] = nil
		d, trace := decide.Evaluate(context.Background(), version, s)
		if !isInternalError(d) || trace[len(trace)-1].Step != step {
			t.Fatalf("nil step %d gave %s", step, d.Outcome())
		}
	}
}

// A zero Decision (never built by a constructor) handed to Stop is refused.
func TestAZeroDecisionIsRefused(t *testing.T) {
	s := continueAll()
	s[decide.T11Policy-1] = func(context.Context) decide.StepResult { return decide.Stop(decide.Decision{}) }
	if d, _ := decide.Evaluate(context.Background(), version, s); !isInternalError(d) {
		t.Fatalf("zero decision gave %s", d.Outcome())
	}
}

// INV-16, REQ-NFR-002 chaos property: for every combination of step behaviours (continue, stop with any outcome, error,
// panic, cancellation), the evaluation is ALLOW only if T1 to T14 all continued and T15 stopped with ALLOW. The trace is
// always a prefix of T1..T15 that ends at the step that decided. testing/quick is the standard library; the seed is fixed
// so the run is reproducible.
type behaviour uint8

func TestChaosPropertyNoAllowUnlessEveryStepPassed(t *testing.T) {
	const kinds = 11 // 0 continue, 1 error, 2 panic, 3 cancel, 4..10 stop with outcomes[k-4]
	property := func(seed [15]behaviour) bool {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var s decide.Steps
		for i := range s {
			b := seed[i] % kinds
			s[i] = func(context.Context) decide.StepResult {
				switch {
				case b == 0:
					return decide.Continue()
				case b == 1:
					return decide.FailStep(errors.New("chaos"))
				case b == 2:
					panic("chaos")
				case b == 3:
					cancel()
					return decide.Continue()
				default:
					return decide.Stop(decisionFor(outcomes[b-4]))
				}
			}
		}
		d, trace := decide.Evaluate(ctx, version, s)
		cleanAllow := true
		for i := 0; i < 14; i++ {
			cleanAllow = cleanAllow && seed[i]%kinds == 0
		}
		cleanAllow = cleanAllow && seed[14]%kinds == 4 // stop with ALLOW
		if (d.Outcome() == decide.Allow) != cleanAllow {
			return false
		}
		for i, e := range trace {
			if e.Step != decide.GateStep(i+1) {
				return false
			}
		}
		return len(trace) >= 1 && len(trace) <= 15
	}
	cfg := &quick.Config{MaxCount: 20000, Rand: rand.New(rand.NewSource(20261101))}
	if err := quick.Check(property, cfg); err != nil {
		t.Fatal(err)
	}
}
