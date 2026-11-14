package intake

// REQ-TRU-002, REQ-TRU-004, REQ-TRU-006, REQ-TRU-007, REQ-RES-001, REQ-RES-002, REQ-NFR-002, INV-03, INV-09, INV-16, INV-21: the steps T10 (reconciliation and the
// freeze), T11 (policy) and T12 (clocks) of PERD 23.3. T7 to T9 are not built, so an evaluation cannot reach these steps through Service.Evaluate; each test runs one
// step through the decide runner with the steps before it passing and the steps after it recording that they were reached. Every value is SYNTHETIC.

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/policy"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

const (
	tTenant    = "ten_synth01"
	tIntentID  = "int_synth0001"
	tNode      = "fin.payments.exception.investigate"
	tSnapshot  = "snap_synth01"
	tCreateCap = "fin.cap.create_case"
	tWriteCap  = "fin.cap.adjust_record"
	tPackCode  = "fin.reason.contradictory_state"
)

var (
	t0       = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC) // SYNTHETIC
	tNow     = t0.Unix()
	errFault = errors.New("injected fault")
	errHit   = errors.New("the next step was reached")
)

// fake is every T10 to T12 port in one: it records the calls and what it was asked, and a fault is injected by method name.
type fake struct {
	calls  []string
	fail   map[string]bool
	panics map[string]bool

	rec         Reconciliation
	gotTenant   string
	gotSnapshot string

	pol      PolicyEvaluation
	gotQuery PolicyQuery

	set         ClockSet
	routing     ClockRouting
	gotClockQ   ClockQuery
	gotStatuses []clocks.Status
}

func (f *fake) hit(name string) error {
	f.calls = append(f.calls, name)
	if f.panics[name] {
		panic("injected panic in " + name)
	}
	if f.fail[name] {
		return errFault
	}
	return nil
}

func (f *fake) called(name string) bool {
	for _, c := range f.calls {
		if c == name {
			return true
		}
	}
	return false
}

func (f *fake) Reconcile(_ context.Context, tenantID, snapshotID string) (Reconciliation, error) {
	f.gotTenant, f.gotSnapshot = tenantID, snapshotID
	return f.rec, f.hit("Reconcile")
}

func (f *fake) Evaluate(_ context.Context, q PolicyQuery) (PolicyEvaluation, error) {
	f.gotQuery = q
	return f.pol, f.hit("Evaluate")
}

func (f *fake) Applicable(_ context.Context, q ClockQuery) (ClockSet, error) {
	f.gotClockQ = q
	return f.set, f.hit("Applicable")
}

func (f *fake) Route(_ context.Context, _ ClockQuery, statuses []clocks.Status) (ClockRouting, error) {
	f.gotStatuses = statuses
	return f.routing, f.hit("Route")
}

// newEv is an evaluation at the synthetic clock reading with the intent T1 would have read and no capability.
func newEv(t *testing.T, f *fake) *evaluation {
	t.Helper()
	svc := &Service{Ports: Ports{Reconciler: f, Policies: f, Clocks: f}, Clock: clock.NewManual(t0), PackVersion: "1.0.0"}
	ev, err := svc.newEvaluation(Request{Session: Session{TenantID: tTenant, PrincipalID: "usr_synth01", Role: "customer"}, EvidenceSnapshotID: tSnapshot}, tIntentID, 2)
	if err != nil {
		t.Fatal(err)
	}
	ev.intent = &AdmittedIntent{TenantID: tTenant, IntentID: tIntentID, Version: 2, IntentNode: tNode, View: decide.IntentView{Version: 2}}
	return ev
}

func (e *evaluation) withCapability(id string) *evaluation {
	e.capability = &Capability{ID: id, Registered: true}
	return e
}

// recordEvidence is what T9 will do: a CONFIRM needs the evidence it rests on in its basis.
func recordEvidence(t *testing.T, ev *evaluation) {
	t.Helper()
	if err := ev.bb.RecordEvidence(tSnapshot, []decide.Freshness{{EvidenceID: "evd_synth01", EffectiveAt: tNow - 10, Source: "src.synth", AgeSeconds: 10, EffectiveLimitSeconds: 60,
		LimitSetBy: decide.LimitBySource}}); err != nil {
		t.Fatal(err)
	}
}

func stepOf(ev *evaluation, step decide.GateStep) decide.StepFunc {
	return map[decide.GateStep]decide.StepFunc{decide.T10Reconciliation: ev.t10, decide.T11Policy: ev.t11, decide.T12Clocks: ev.t12}[step]
}

// run runs one step through the decide runner (EvaluateBound, so a CONFIRM's basis is checked as in production). The steps before it pass; the steps after it fail
// with errHit, so "the evaluation went on" is a PASSED entry for the step and a FAILED entry for the next one.
func run(t *testing.T, ev *evaluation, step decide.GateStep) (decide.Decision, []decide.TraceEntry) {
	t.Helper()
	var steps decide.Steps
	for i := range steps {
		switch s := decide.GateStep(i + 1); {
		case s < step:
			steps[i] = func(context.Context) decide.StepResult { return decide.Continue() }
		case s == step:
			steps[i] = stepOf(ev, step)
		default:
			steps[i] = func(context.Context) decide.StepResult { return decide.FailStep(errHit) }
		}
	}
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return decide.EvaluateBound(c, ev.version, steps, ev.bb)
}

func reasonList(d decide.Decision) []string {
	out := []string{}
	for _, r := range d.Reasons() {
		out = append(out, r.String())
	}
	return out
}

// stopped asserts the step ended the evaluation with this outcome, these reasons and these layers, as its own terminal result.
func stopped(t *testing.T, name string, d decide.Decision, trace []decide.TraceEntry, step decide.GateStep, o decide.Outcome, reasons []string, a decide.Authority) {
	t.Helper()
	if d.Outcome() != o || !reflect.DeepEqual(reasonList(d), reasons) {
		t.Errorf("%s: %s %v, want %s %v", name, d.Outcome(), reasonList(d), o, reasons)
	}
	if got := trace[len(trace)-1]; got.Step != step || got.Result != decide.StepTerminal {
		t.Errorf("%s: trace %v, want it to end at %v TERMINAL", name, trace, step)
	}
	if d.Authority() != a {
		t.Errorf("%s: authority %+v, want %+v", name, d.Authority(), a)
	}
}

// failedClosed asserts the step could not run and the evaluation ended UNKNOWN INTERNAL_ERROR at it: never ALLOW, and no later step reached.
func failedClosed(t *testing.T, name string, d decide.Decision, trace []decide.TraceEntry, step decide.GateStep) {
	t.Helper()
	if d.Outcome() == decide.Allow {
		t.Fatalf("%s: ALLOW", name)
	}
	if d.Outcome() != decide.Unknown || !reflect.DeepEqual(reasonList(d), []string{"INTERNAL_ERROR"}) {
		t.Errorf("%s: %s %v, want UNKNOWN INTERNAL_ERROR", name, d.Outcome(), reasonList(d))
	}
	if got := trace[len(trace)-1]; got.Step != step || got.Result == decide.StepPassed || got.Result == decide.StepTerminal {
		t.Errorf("%s: trace %v, want it to end at %v with the step failing", name, trace, step)
	}
}

// wentOn asserts the step passed and the evaluation reached the next step.
func wentOn(t *testing.T, name string, trace []decide.TraceEntry, step decide.GateStep) {
	t.Helper()
	if n := len(trace); n < 2 || trace[n-2] != (decide.TraceEntry{Step: step, Result: decide.StepPassed}) || trace[n-1].Step != step+1 {
		t.Errorf("%s: trace %v, want %v PASSED and the next step reached", name, trace, step)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------
// T10

var escalateAtT10 = decide.Authority{Technical: decide.Pass, Policy: decide.NotEvaluated, Business: decide.Pass}

// REQ-RES-001, PERD 23.3 T10: a classification the pack maps to ESCALATE (a contradiction, an unknown outcome) ends the evaluation with ESCALATE and the pack's reason code.
func TestT10EscalatesWithThePackReasonCodeWhenThePackMapsTheClassification(t *testing.T) {
	for _, c := range []struct {
		class  reconcile.Classification
		code   string
		frozen bool
	}{
		{reconcile.ContradictoryState, tPackCode, true},
		{reconcile.ExecutionOutcomeUnknown, "fin.reason.execution_outcome_unknown", false},
	} {
		f := &fake{rec: Reconciliation{Classification: c.class, Frozen: c.frozen, EscalateReason: c.code}}
		d, trace := run(t, newEv(t, f), decide.T10Reconciliation)
		stopped(t, string(c.class), d, trace, decide.T10Reconciliation, decide.Escalate, []string{c.code}, escalateAtT10)
	}
}

func TestT10AsksTheReconcilerAboutTheTenantAndTheSealedSnapshot(t *testing.T) {
	f := &fake{rec: Reconciliation{Classification: reconcile.SuccessConfirmed}}
	run(t, newEv(t, f), decide.T10Reconciliation)
	if f.gotTenant != tTenant || f.gotSnapshot != tSnapshot {
		t.Fatalf("Reconcile(%q, %q), want (%q, %q)", f.gotTenant, f.gotSnapshot, tTenant, tSnapshot)
	}
}

// A classification that names a settled, understood state and that the pack maps to nothing lets the evaluation go on.
func TestT10GoesOnWhenThePackMapsNothingToASettledClassification(t *testing.T) {
	for _, c := range []reconcile.Classification{reconcile.SuccessConfirmed, reconcile.RejectedConfirmed, reconcile.ReversedConfirmed, reconcile.DebitPostedCreditUnconfirmed,
		reconcile.SettlementPending, reconcile.ReversalPending, reconcile.Partial} {
		f := &fake{rec: Reconciliation{Classification: c}}
		_, trace := run(t, newEv(t, f), decide.T10Reconciliation)
		wentOn(t, string(c), trace, decide.T10Reconciliation)
	}
}

// SR blocking finding on noema#570 (INV-16, REQ-RES-001, "unknown means refuse or hand to the team"): a classification that says the state is contradictory, the outcome
// is unknown, a duplicate is possible, the event order is uncertain or the evidence is insufficient, or a value that is not a classification at all, never goes on just
// because the pack mapped nothing. The pack may add escalations; it is not the only thing between a contradiction and an ALLOW. The step fails closed (UNKNOWN).
// This is the provisional safer reading (AGENT-20) of which classifications may go on unmapped; the Architect confirms or changes the two sets.
func TestT10AClassificationThatMustNotGoOnFailsClosedWhenThePackMapsNothing(t *testing.T) {
	for _, c := range []reconcile.Classification{reconcile.ContradictoryState, reconcile.ExecutionOutcomeUnknown, reconcile.PossibleDuplicate, reconcile.EventOrderUncertain,
		reconcile.InsufficientEvidence, "MADE_UP_BY_AN_ADAPTER", "success_confirmed"} {
		for name, ev := range map[string]func(*evaluation) *evaluation{
			"no capability named":       func(e *evaluation) *evaluation { return e },
			"a write capability named":  func(e *evaluation) *evaluation { return e.withCapability(tWriteCap) },
			"a create capability named": func(e *evaluation) *evaluation { return e.withCapability(tCreateCap) },
		} {
			f := &fake{rec: Reconciliation{Classification: c}}
			d, trace := run(t, ev(newEv(t, f)), decide.T10Reconciliation)
			failedClosed(t, string(c)+", "+name, d, trace, decide.T10Reconciliation)
		}
	}
}

// Every classification the reconcile package declares is in exactly one of the two sets of the step, so a thirteenth one cannot arrive and go on by default.
func TestT10EveryDeclaredClassificationIsEitherSettledOrStopsWhenUnmapped(t *testing.T) {
	declared := declaredClassifications(t)
	if len(declared) != 12 {
		t.Fatalf("reconcile declares %d classifications, want the twelve of PERD 22.3", len(declared))
	}
	for _, c := range declared {
		if goesOnUnmapped(reconcile.Classification(c)) == stopsWhenUnmapped[c] {
			t.Errorf("%s: it is in %v of the two sets of the step; want exactly one", c, map[bool]string{true: "both", false: "neither"}[stopsWhenUnmapped[c]])
		}
	}
}

// stopsWhenUnmapped is the other half of the table, written out again here and not read from the step.
var stopsWhenUnmapped = map[string]bool{"CONTRADICTORY_STATE": true, "EXECUTION_OUTCOME_UNKNOWN": true, "POSSIBLE_DUPLICATE": true, "EVENT_ORDER_UNCERTAIN": true, "INSUFFICIENT_EVIDENCE": true}

// declaredClassifications reads the Classification constants out of reconcile/types.go.
func declaredClassifications(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "../reconcile/types.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, sp := range gd.Specs {
			vs := sp.(*ast.ValueSpec)
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "Classification" {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok {
					t.Fatalf("a Classification constant that is not a string literal: %T", v)
				}
				u, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, u)
			}
		}
	}
	return out
}

// REQ-RES-002: a frozen target and a capability the pack does not allow under the freeze never goes on. With no pack reason to escalate with, the step cannot build an
// ESCALATE, so it fails closed: UNKNOWN.
func TestT10TheFreezeStopsACapabilityThePackDoesNotAllowUnderIt(t *testing.T) {
	f := &fake{rec: Reconciliation{Classification: reconcile.ContradictoryState, Frozen: true, AllowedUnderFreeze: []string{tCreateCap}}}
	d, trace := run(t, newEv(t, f).withCapability(tWriteCap), decide.T10Reconciliation)
	failedClosed(t, "frozen, capability not allowed, no pack mapping", d, trace, decide.T10Reconciliation)
}

func TestT10TheFreezeEscalatesWithThePackReasonWhenThePackMapsIt(t *testing.T) {
	f := &fake{rec: Reconciliation{Classification: reconcile.ContradictoryState, Frozen: true, AllowedUnderFreeze: []string{tCreateCap}, EscalateReason: tPackCode}}
	d, trace := run(t, newEv(t, f).withCapability(tWriteCap), decide.T10Reconciliation)
	stopped(t, "frozen with mapping", d, trace, decide.T10Reconciliation, decide.Escalate, []string{tPackCode}, escalateAtT10)
}

func TestT10TheFreezeLetsThroughACapabilityThePackAllowsUnderIt(t *testing.T) {
	frozen := Reconciliation{Classification: reconcile.ContradictoryState, Frozen: true, AllowedUnderFreeze: []string{tCreateCap}}
	notFrozen := Reconciliation{Classification: reconcile.SuccessConfirmed, AllowedUnderFreeze: []string{tCreateCap}}
	for _, c := range []struct {
		name string
		rec  Reconciliation
		ev   func(*evaluation) *evaluation
	}{
		{"frozen, capability allowed under the freeze", frozen, func(e *evaluation) *evaluation { return e.withCapability(tCreateCap) }},
		{"not frozen, capability not on the freeze list", notFrozen, func(e *evaluation) *evaluation { return e.withCapability(tWriteCap) }},
	} {
		f := &fake{rec: c.rec}
		_, trace := run(t, c.ev(newEv(t, f)), decide.T10Reconciliation)
		wentOn(t, c.name, trace, decide.T10Reconciliation)
	}
}

// SR blocking finding on noema#570: a frozen target with an intent that names no capability asks for no grant, but nothing says what it may do either, and a plan's
// capabilities are only known at T14. With no pack mapping to escalate with, it fails closed here; the plan's own capabilities are checked against the freeze at T14.
func TestT10AFrozenTargetWithNoNamedCapabilityAndNoPackMappingFailsClosed(t *testing.T) {
	f := &fake{rec: Reconciliation{Classification: reconcile.ContradictoryState, Frozen: true, AllowedUnderFreeze: []string{tCreateCap}}}
	d, trace := run(t, newEv(t, f), decide.T10Reconciliation)
	failedClosed(t, "frozen, no capability named, no mapping", d, trace, decide.T10Reconciliation)
}

// INV-16, REQ-NFR-002: everything else is UNKNOWN, never ALLOW and never a guess.
func TestT10FailsClosedOnAnErrorAPanicAndAnInvalidAnswer(t *testing.T) {
	good := Reconciliation{Classification: reconcile.SuccessConfirmed}
	for _, c := range []struct {
		name string
		set  func(f *fake, ev *evaluation)
	}{
		{"an error from the Reconciler", func(f *fake, ev *evaluation) { f.fail = map[string]bool{"Reconcile": true} }},
		{"a panic in the Reconciler", func(f *fake, ev *evaluation) { f.panics = map[string]bool{"Reconcile": true} }},
		{"no classification", func(f *fake, ev *evaluation) { f.rec = Reconciliation{} }},
		{"a mapping that is a core code, not a pack code", func(f *fake, ev *evaluation) { f.rec.EscalateReason = "INTERNAL_ERROR" }},
		{"a mapping that is not a code", func(f *fake, ev *evaluation) { f.rec.EscalateReason = "contradiction!" }},
		{"a mapping outside the pack form", func(f *fake, ev *evaluation) { f.rec.EscalateReason = "fin.contradictory_state" }},
		{"no sealed snapshot", func(f *fake, ev *evaluation) { ev.snapshotID = "" }},
		{"no admitted intent", func(f *fake, ev *evaluation) { ev.intent = nil }},
		{"an intent that is not resolved", func(f *fake, ev *evaluation) {
			ev.intent.View = decide.IntentView{Version: 2, MaterialFlags: []string{"ambiguous_counterparty"}}
			f.rec.EscalateReason = tPackCode
		}},
	} {
		f := &fake{rec: good}
		ev := newEv(t, f)
		c.set(f, ev)
		d, trace := run(t, ev, decide.T10Reconciliation)
		failedClosed(t, c.name, d, trace, decide.T10Reconciliation)
	}
}

func TestT10NoSnapshotMeansNothingIsReconciled(t *testing.T) {
	f := &fake{rec: Reconciliation{Classification: reconcile.SuccessConfirmed}}
	ev := newEv(t, f)
	ev.snapshotID = ""
	run(t, ev, decide.T10Reconciliation)
	if f.called("Reconcile") {
		t.Fatal("the Reconciler was asked about no snapshot")
	}
}

// INV-21: the same inputs give the same decision and trace.
func TestT10IsDeterministic(t *testing.T) {
	var ds []decide.Decision
	var trs [][]decide.TraceEntry
	for i := 0; i < 2; i++ {
		f := &fake{rec: Reconciliation{Classification: reconcile.ContradictoryState, Frozen: true, EscalateReason: tPackCode}}
		d, tr := run(t, newEv(t, f), decide.T10Reconciliation)
		ds, trs = append(ds, d), append(trs, tr)
	}
	if !reflect.DeepEqual(ds[0], ds[1]) || !reflect.DeepEqual(trs[0], trs[1]) {
		t.Fatalf("two runs differ: %v / %v", ds, trs)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------
// T11

var (
	ruleA = decide.PolicyRuleVersion{RuleID: "rule_a", PolicyID: "fin.pol", Version: "3.2"}
	ruleB = decide.PolicyRuleVersion{RuleID: "rule_b", PolicyID: "fin.pol", Version: "3.1"}
	vers  = []decide.PolicyVersionRef{{PolicyID: "fin.pol", Version: "3.1"}, {PolicyID: "fin.pol", Version: "3.2"}}

	rejectLayers  = decide.Authority{Technical: decide.Pass, Policy: decide.Fail, Business: decide.Pass}
	allPassLayers = decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}
	waitLayers    = decide.Authority{Technical: decide.Pass, Policy: decide.NotEvaluated, Business: decide.Pass}
)

func decided(e policy.Effect, code string) PolicyEvaluation {
	return PolicyEvaluation{Effect: e, ReasonCode: code, Rules: []decide.PolicyRuleVersion{ruleA, ruleB}, Versions: vers, PermitMatched: true}
}

// PERD 23.3 T11, 23.4: each rule effect ends the evaluation as its own outcome, with the rule's reason code.
func TestT11EachEffectIsItsOwnOutcomeWithTheRulesReasonCode(t *testing.T) {
	waitAt := tNow + 3600
	for _, c := range []struct {
		name    string
		pol     PolicyEvaluation
		outcome decide.Outcome
		reasons []string
		layers  decide.Authority
		wait    int64
	}{
		{"REJECT", decided(policy.Reject, "fin.reason.policy_denial"), decide.Reject, []string{"fin.reason.policy_denial"}, rejectLayers, 0},
		{"REJECT with a core code", decided(policy.Reject, "RATE_LIMITED"), decide.Reject, []string{"RATE_LIMITED"}, rejectLayers, 0},
		{"ESCALATE", decided(policy.Escalate, tPackCode), decide.Escalate, []string{tPackCode}, rejectLayers, 0},
		{"CONFIRM", decided(policy.Confirm, "fin.reason.needs_approver"), decide.Confirm, []string{"fin.reason.needs_approver"}, allPassLayers, 0},
		{"WAIT", func() PolicyEvaluation {
			p := decided(policy.Wait, "fin.reason.wait_for_clock")
			p.ReevaluateAt = waitAt
			return p
		}(), decide.Wait,
			[]string{"fin.reason.wait_for_clock"}, waitLayers, waitAt},
	} {
		f := &fake{pol: c.pol}
		ev := newEv(t, f)
		recordEvidence(t, ev)
		d, trace := run(t, ev, decide.T11Policy)
		stopped(t, c.name, d, trace, decide.T11Policy, c.outcome, c.reasons, c.layers)
		if d.ReevaluateAt() != c.wait {
			t.Errorf("%s: reevaluate_at %d, want %d", c.name, d.ReevaluateAt(), c.wait)
		}
	}
}

func TestT11APermitGoesOn(t *testing.T) {
	f := &fake{pol: decided(policy.Permit, "")}
	_, trace := run(t, newEv(t, f), decide.T11Policy)
	wentOn(t, "PERMIT", trace, decide.T11Policy)
}

// PERD 23.3, 23.4: default deny. No permitting rule, or a permit that names no rule, or a CONFIRM with no PERMIT matched, or a REJECT that says nothing: REJECT NO_PERMITTING_RULE.
func TestT11DefaultDenyIsRejectNoPermittingRule(t *testing.T) {
	for _, c := range []struct {
		name string
		pol  PolicyEvaluation
	}{
		{"no rule matched (the policy's own default deny)", PolicyEvaluation{Effect: policy.Reject, ReasonCode: policy.NoPermittingRule, Versions: vers}},
		{"a PERMIT that names no deciding rule", PolicyEvaluation{Effect: policy.Permit, Versions: vers, PermitMatched: true}},
		{"a CONFIRM with no PERMIT matched", func() PolicyEvaluation {
			p := decided(policy.Confirm, "fin.reason.needs_approver")
			p.PermitMatched = false
			return p
		}()},
		{"a REJECT with no reason code", decided(policy.Reject, "")},
	} {
		f := &fake{pol: c.pol}
		ev := newEv(t, f)
		recordEvidence(t, ev)
		d, trace := run(t, ev, decide.T11Policy)
		stopped(t, c.name, d, trace, decide.T11Policy, decide.Reject, []string{"NO_PERMITTING_RULE"}, rejectLayers)
	}
}

// PERD 23.1: WAIT carries the time it is re-evaluated, and that time is in the future of the evaluation's clock reading.
func TestT11AWaitCarriesTheReevaluateAtTimeAndNeedsOneInTheFuture(t *testing.T) {
	for _, at := range []int64{tNow + 1, tNow + 86400} {
		p := decided(policy.Wait, "fin.reason.wait_for_clock")
		p.ReevaluateAt = at
		d, _ := run(t, newEv(t, &fake{pol: p}), decide.T11Policy)
		if d.Outcome() != decide.Wait || d.ReevaluateAt() != at {
			t.Errorf("WAIT at %d: %s reevaluate_at %d", at, d.Outcome(), d.ReevaluateAt())
		}
	}
	for _, at := range []int64{0, -1, tNow - 1, tNow} {
		p := decided(policy.Wait, "fin.reason.wait_for_clock")
		p.ReevaluateAt = at
		d, trace := run(t, newEv(t, &fake{pol: p}), decide.T11Policy)
		failedClosed(t, "WAIT with reevaluate_at "+time.Unix(at, 0).UTC().String(), d, trace, decide.T11Policy)
	}
}

// RecordPolicy is called exactly once, with the rules that decided: the basis then holds exactly them, and a second record is refused.
func TestT11RecordsThePolicyRulesOnceForEveryEffect(t *testing.T) {
	waited := decided(policy.Wait, "fin.reason.wait_for_clock")
	waited.ReevaluateAt = tNow + 60
	for _, p := range []PolicyEvaluation{decided(policy.Permit, ""), decided(policy.Reject, "fin.reason.policy_denial"), decided(policy.Escalate, tPackCode),
		decided(policy.Confirm, "fin.reason.needs_approver"), waited,
		{Effect: policy.Reject, ReasonCode: policy.NoPermittingRule, Versions: vers}} {
		ev := newEv(t, &fake{pol: p})
		recordEvidence(t, ev)
		run(t, ev, decide.T11Policy)
		base, err := ev.bb.Basis()
		if err != nil {
			t.Fatal(err)
		}
		if len(base.PolicyRules) != len(p.Rules) || len(p.Rules) != 0 && !reflect.DeepEqual(base.PolicyRules, p.Rules) {
			t.Errorf("%s: recorded %v, want %v", p.Effect, base.PolicyRules, p.Rules)
		}
		if err := ev.bb.RecordPolicy(nil); !errors.Is(err, decide.ErrBasisRecorded) {
			t.Errorf("%s: the policy was not recorded exactly once: a further record gave %v", p.Effect, err)
		}
	}
}

func TestT11RecordsTheDecidingRuleVersionsAndKeepsThePolicyVersionsUsed(t *testing.T) {
	f := &fake{pol: decided(policy.Permit, "")}
	ev := newEv(t, f)
	run(t, ev, decide.T11Policy)
	base, err := ev.bb.Basis()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(base.PolicyRules, []decide.PolicyRuleVersion{ruleA, ruleB}) {
		t.Errorf("basis rules %v, want %v", base.PolicyRules, []decide.PolicyRuleVersion{ruleA, ruleB})
	}
	if !reflect.DeepEqual(ev.policyVersions, vers) {
		t.Errorf("policy versions %v, want %v", ev.policyVersions, vers)
	}
}

// TRU-13: a CONFIRM carries the basis the builder holds, and it checks out.
func TestT11AConfirmCarriesTheEvaluatedBasis(t *testing.T) {
	ev := newEv(t, &fake{pol: decided(policy.Confirm, "fin.reason.needs_approver")})
	recordEvidence(t, ev)
	d, _ := run(t, ev, decide.T11Policy)
	if d.Outcome() != decide.Confirm {
		t.Fatalf("%s", d.Outcome())
	}
	if err := ev.bb.Check(d); err != nil {
		t.Fatalf("the basis does not match the evaluated set: %v", err)
	}
	if !reflect.DeepEqual(d.Basis().PolicyRules, []decide.PolicyRuleVersion{ruleA, ruleB}) {
		t.Errorf("basis rules %v", d.Basis().PolicyRules)
	}
}

// With no evidence recorded (T9 not run), a CONFIRM rests on nothing: fail closed.
func TestT11AConfirmWithNoEvidenceInItsBasisFailsClosed(t *testing.T) {
	ev := newEv(t, &fake{pol: decided(policy.Confirm, "fin.reason.needs_approver")})
	d, trace := run(t, ev, decide.T11Policy)
	failedClosed(t, "CONFIRM with no evidence", d, trace, decide.T11Policy)
}

func TestT11AsksThePolicyAboutTheRequest(t *testing.T) {
	f := &fake{pol: decided(policy.Permit, "")}
	run(t, newEv(t, f).withCapability(tCreateCap), decide.T11Policy)
	want := PolicyQuery{TenantID: tTenant, IntentID: tIntentID, IntentVersion: 2, IntentNode: tNode, CapabilityID: tCreateCap, SnapshotID: tSnapshot, Now: tNow}
	if f.gotQuery != want {
		t.Fatalf("query %+v, want %+v", f.gotQuery, want)
	}
	f2 := &fake{pol: decided(policy.Permit, "")}
	run(t, newEv(t, f2), decide.T11Policy)
	if f2.gotQuery.CapabilityID != "" {
		t.Errorf("capability %q for an intent that names none", f2.gotQuery.CapabilityID)
	}
}

// INV-16: an error or a panic from the port, an effect nobody defined, a reason the step cannot use, and a basis already recorded are UNKNOWN, never ALLOW and never a skipped rule.
func TestT11FailsClosedOnAnErrorAPanicAndAnInvalidAnswer(t *testing.T) {
	good := decided(policy.Permit, "")
	for _, c := range []struct {
		name string
		set  func(f *fake, ev *evaluation)
	}{
		{"an error from the Policies port", func(f *fake, ev *evaluation) { f.fail = map[string]bool{"Evaluate": true} }},
		{"a panic in the Policies port", func(f *fake, ev *evaluation) { f.panics = map[string]bool{"Evaluate": true} }},
		{"an empty effect", func(f *fake, ev *evaluation) { f.pol.Effect = "" }},
		{"an effect nobody defined", func(f *fake, ev *evaluation) { f.pol.Effect = "ALLOW" }},
		{"an ESCALATE with no reason code", func(f *fake, ev *evaluation) { f.pol = decided(policy.Escalate, "") }},
		{"an ESCALATE with a reason that is no code", func(f *fake, ev *evaluation) { f.pol = decided(policy.Escalate, "because") }},
		{"a WAIT with no reason code", func(f *fake, ev *evaluation) { f.pol = decided(policy.Wait, ""); f.pol.ReevaluateAt = tNow + 60 }},
		{"a deciding rule with no ID", func(f *fake, ev *evaluation) {
			f.pol.Rules = []decide.PolicyRuleVersion{{PolicyID: "fin.pol", Version: "1"}}
		}},
		{"a deciding rule with no version", func(f *fake, ev *evaluation) {
			f.pol.Rules = []decide.PolicyRuleVersion{{RuleID: "r", PolicyID: "fin.pol"}}
		}},
		{"a policy already recorded in the basis", func(f *fake, ev *evaluation) {
			if err := ev.bb.RecordPolicy(nil); err != nil {
				t.Fatal(err)
			}
		}},
		{"no basis builder", func(f *fake, ev *evaluation) { ev.bb = nil }},
		{"no admitted intent", func(f *fake, ev *evaluation) { ev.intent = nil }},
		{"an intent that is not resolved", func(f *fake, ev *evaluation) {
			ev.intent.View = decide.IntentView{Version: 2, MaterialFlags: []string{"x"}}
		}},
	} {
		f := &fake{pol: good}
		ev := newEv(t, f)
		c.set(f, ev)
		bb := ev.bb
		var d decide.Decision
		var trace []decide.TraceEntry
		if bb == nil {
			real := newEv(t, f)
			ev.bb = nil
			d, trace = runWithBuilder(t, ev, real.bb)
		} else {
			d, trace = run(t, ev, decide.T11Policy)
		}
		failedClosed(t, c.name, d, trace, decide.T11Policy)
	}
}

// runWithBuilder runs T11 on an evaluation that has no builder, checking the decision against another one (the runner needs one to accept a CONFIRM).
func runWithBuilder(t *testing.T, ev *evaluation, check *decide.BasisBuilder) (decide.Decision, []decide.TraceEntry) {
	t.Helper()
	var steps decide.Steps
	for i := range steps {
		steps[i] = func(context.Context) decide.StepResult { return decide.FailStep(errHit) }
	}
	for i := 0; i < int(decide.T11Policy)-1; i++ {
		steps[i] = func(context.Context) decide.StepResult { return decide.Continue() }
	}
	steps[decide.T11Policy-1] = ev.t11
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return decide.EvaluateBound(c, ev.version, steps, check)
}

// A failed policy evaluation records nothing: the basis stays open for the evaluation's own error path.
func TestT11AFailedEvaluationRecordsNothing(t *testing.T) {
	f := &fake{pol: decided(policy.Permit, ""), fail: map[string]bool{"Evaluate": true}}
	ev := newEv(t, f)
	run(t, ev, decide.T11Policy)
	if err := ev.bb.RecordPolicy(nil); err != nil {
		t.Fatalf("the policy was recorded although the evaluation failed: %v", err)
	}
}

func TestT11IsDeterministic(t *testing.T) {
	var ds []decide.Decision
	var trs [][]decide.TraceEntry
	for i := 0; i < 2; i++ {
		ev := newEv(t, &fake{pol: decided(policy.Escalate, tPackCode)})
		d, tr := run(t, ev, decide.T11Policy)
		ds, trs = append(ds, d), append(trs, tr)
	}
	if !reflect.DeepEqual(ds[0], ds[1]) || !reflect.DeepEqual(trs[0], trs[1]) {
		t.Fatalf("two runs differ: %v / %v", ds, trs)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------
// T12

const (
	clockSeconds = int64(1000)
	clockStart   = int64(1_000_000_000)
)

var escalateAtT12 = decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}

func syntheticClock(id string, duration int64) contracts.ClockV1 {
	return contracts.ClockV1{ClockId: contracts.CommonV1PackId(id), AnchorEvent: "complaint_logged", DurationSeconds: duration, Label: contracts.ClockV1LabelSYNTHETIC}
}

func anchored(at int64) *int64 { return &at }

// clockAt gives the clock whose anchor makes `remaining` seconds remain at the evaluation's clock reading.
func clockAt(id string, remaining int64) ClockInstance {
	return ClockInstance{Clock: syntheticClock(id, clockSeconds), Anchor: anchored(tNow - (clockSeconds - remaining))}
}

var quarterOf = clocks.Ratio{Num: 1, Den: 4}

// REQ-TRU-006, PERD 23.5: the step evaluates every clock at the evaluation's clock reading, with the at-risk fraction the port gave, and hands the statuses to the pack's routing.
func TestT12EvaluatesEveryClockAtTheServiceClockWithThePortsAtRiskFraction(t *testing.T) {
	f := &fake{set: ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{
		{Clock: syntheticClock("fin.clock.not_started", clockSeconds)},
		clockAt("fin.clock.running", 600),
		clockAt("fin.clock.at_risk", 200),
		clockAt("fin.clock.breached", 0),
	}}}
	_, trace := run(t, newEv(t, f), decide.T12Clocks)
	wentOn(t, "no escalation", trace, decide.T12Clocks)
	var got []clocks.Phase
	var ids []string
	for _, s := range f.gotStatuses {
		got, ids = append(got, s.Phase), append(ids, string(s.ClockID))
	}
	if want := []clocks.Phase{clocks.NotStarted, clocks.Running, clocks.AtRisk, clocks.Breached}; !reflect.DeepEqual(got, want) {
		t.Fatalf("phases %v, want %v", got, want)
	}
	if want := []string{"fin.clock.not_started", "fin.clock.running", "fin.clock.at_risk", "fin.clock.breached"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("clock IDs %v, want %v", ids, want)
	}
	if s := f.gotStatuses[0]; s.StartedAt != nil || s.DeadlineAt != nil || s.Remaining != nil {
		t.Errorf("a clock not started has %+v: nothing may be assumed", s)
	}
	if s := f.gotStatuses[1]; *s.Remaining != 600 || s.Elapsed != 400 {
		t.Errorf("a running clock has remaining %d elapsed %d, want 600 and 400 at the service clock's reading", *s.Remaining, s.Elapsed)
	}
	if s := f.gotStatuses[3]; *s.Remaining != 0 || s.Elapsed != clockSeconds {
		t.Errorf("a breached clock has remaining %d elapsed %d", *s.Remaining, s.Elapsed)
	}
}

// The at-risk fraction is the port's: the same remaining time is AT_RISK at one quarter and RUNNING at one tenth.
func TestT12TheAtRiskFractionComesFromThePort(t *testing.T) {
	for _, c := range []struct {
		ratio clocks.Ratio
		phase clocks.Phase
	}{{quarterOf, clocks.AtRisk}, {clocks.Ratio{Num: 1, Den: 10}, clocks.Running}} {
		f := &fake{set: ClockSet{AtRisk: c.ratio, Clocks: []ClockInstance{clockAt("fin.clock.a", 200)}}}
		run(t, newEv(t, f), decide.T12Clocks)
		if len(f.gotStatuses) != 1 || f.gotStatuses[0].Phase != c.phase {
			t.Errorf("at-risk %d/%d: %+v, want %s", c.ratio.Num, c.ratio.Den, f.gotStatuses, c.phase)
		}
	}
}

// The time is the Service.Clock's: moving that clock moves every status.
func TestT12UsesTheEvaluationsClockReading(t *testing.T) {
	f := &fake{set: ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{{Clock: syntheticClock("fin.clock.a", clockSeconds), Anchor: anchored(clockStart)}}}}
	manual := clock.NewManual(time.Unix(clockStart+700, 0))
	svc := &Service{Ports: Ports{Reconciler: f, Policies: f, Clocks: f}, Clock: manual, PackVersion: "1.0.0"}
	ev, err := svc.newEvaluation(Request{Session: Session{TenantID: tTenant}, EvidenceSnapshotID: tSnapshot}, tIntentID, 2)
	if err != nil {
		t.Fatal(err)
	}
	ev.intent = &AdmittedIntent{IntentNode: tNode, View: decide.IntentView{Version: 2}}
	run(t, ev, decide.T12Clocks)
	if len(f.gotStatuses) != 1 || f.gotStatuses[0].Elapsed != 700 || f.gotStatuses[0].Phase != clocks.Running {
		t.Fatalf("%+v, want elapsed 700 and RUNNING at the manual clock", f.gotStatuses)
	}
}

func TestT12AsksThePortsAboutTheRequest(t *testing.T) {
	f := &fake{set: ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{clockAt("fin.clock.a", 600)}}}
	run(t, newEv(t, f), decide.T12Clocks)
	if want := (ClockQuery{TenantID: tTenant, IntentNode: tNode, SnapshotID: tSnapshot}); f.gotClockQ != want {
		t.Fatalf("query %+v, want %+v", f.gotClockQ, want)
	}
}

// PERD 23.3 T12: the step may escalate only when the pack's routing says so, with the pack's reason code.
func TestT12EscalatesOnlyWhenThePackRoutingSaysSo(t *testing.T) {
	set := ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{clockAt("fin.clock.breached", 0)}}
	f := &fake{set: set, routing: ClockRouting{Escalate: true, ReasonCode: "fin.reason.sla_breached"}}
	d, trace := run(t, newEv(t, f), decide.T12Clocks)
	stopped(t, "routing escalates", d, trace, decide.T12Clocks, decide.Escalate, []string{"fin.reason.sla_breached"}, escalateAtT12)
	if len(f.gotStatuses) != 1 || f.gotStatuses[0].Phase != clocks.Breached {
		t.Errorf("the routing saw %+v, want the BREACHED status", f.gotStatuses)
	}
	f = &fake{set: set, routing: ClockRouting{Escalate: false, ReasonCode: "fin.reason.sla_breached"}}
	_, trace = run(t, newEv(t, f), decide.T12Clocks)
	wentOn(t, "routing does not escalate", trace, decide.T12Clocks)
}

// Nothing is recorded about a clock: the decision basis is as it was.
func TestT12RecordsNothingInTheBasis(t *testing.T) {
	f := &fake{set: ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{clockAt("fin.clock.a", 100)}}, routing: ClockRouting{Escalate: true, ReasonCode: "fin.reason.sla_at_risk"}}
	ev := newEv(t, f)
	before, err := ev.bb.Basis()
	if err != nil {
		t.Fatal(err)
	}
	run(t, ev, decide.T12Clocks)
	after, err := ev.bb.Basis()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("the basis changed: %+v then %+v (%v)", before, after, err)
	}
	if err := ev.bb.RecordPolicy(nil); err != nil {
		t.Errorf("T12 recorded a policy: %v", err)
	}
}

func TestT12NoClockAppliesGoesOnWithoutAskingTheRouting(t *testing.T) {
	f := &fake{set: ClockSet{AtRisk: quarterOf}}
	_, trace := run(t, newEv(t, f), decide.T12Clocks)
	wentOn(t, "no clocks", trace, decide.T12Clocks)
	if f.called("Route") {
		t.Error("the routing was asked about no clock")
	}
}

// clocks.ErrInvalid is never guessed: the step fails closed, and the routing is never asked.
func TestT12AnInvalidClockInputIsUnknownAndNothingIsGuessed(t *testing.T) {
	ok := clockAt("fin.clock.ok", 600)
	for _, c := range []struct {
		name string
		set  ClockSet
	}{
		{"a clock with no duration", ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{ok, {Clock: syntheticClock("fin.clock.bad", 0), Anchor: anchored(clockStart)}}}},
		{"an anchor after now", ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{{Clock: syntheticClock("fin.clock.bad", clockSeconds), Anchor: anchored(tNow + 1)}}}},
		{"a clock with no label", ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{{Clock: contracts.ClockV1{ClockId: "fin.clock.bad", DurationSeconds: clockSeconds}}}}},
		{"an at-risk fraction with no denominator", ClockSet{AtRisk: clocks.Ratio{Num: 1}, Clocks: []ClockInstance{ok}}},
		{"an at-risk fraction above one", ClockSet{AtRisk: clocks.Ratio{Num: 5, Den: 4}, Clocks: []ClockInstance{ok}}},
	} {
		f := &fake{set: c.set, routing: ClockRouting{Escalate: true, ReasonCode: "fin.reason.sla_breached"}}
		d, trace := run(t, newEv(t, f), decide.T12Clocks)
		failedClosed(t, c.name, d, trace, decide.T12Clocks)
		if f.called("Route") {
			t.Errorf("%s: the routing was asked after an invalid clock", c.name)
		}
	}
}

func TestT12FailsClosedOnAnErrorAPanicAndAnInvalidAnswer(t *testing.T) {
	set := ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{clockAt("fin.clock.a", 100)}}
	for _, c := range []struct {
		name string
		set  func(f *fake, ev *evaluation)
	}{
		{"an error from Applicable", func(f *fake, ev *evaluation) { f.fail = map[string]bool{"Applicable": true} }},
		{"a panic in Applicable", func(f *fake, ev *evaluation) { f.panics = map[string]bool{"Applicable": true} }},
		{"an error from Route", func(f *fake, ev *evaluation) { f.fail = map[string]bool{"Route": true} }},
		{"a panic in Route", func(f *fake, ev *evaluation) { f.panics = map[string]bool{"Route": true} }},
		{"an error from Route that also says escalate", func(f *fake, ev *evaluation) {
			f.fail = map[string]bool{"Route": true}
			f.routing = ClockRouting{Escalate: true, ReasonCode: "fin.reason.sla_breached"}
		}},
		{"an escalation with no reason code", func(f *fake, ev *evaluation) { f.routing = ClockRouting{Escalate: true} }},
		{"an escalation with a core reason code", func(f *fake, ev *evaluation) { f.routing = ClockRouting{Escalate: true, ReasonCode: "NOT_AUTHORIZED"} }},
		{"an escalation with a malformed reason code", func(f *fake, ev *evaluation) { f.routing = ClockRouting{Escalate: true, ReasonCode: "fin.sla"} }},
		{"no admitted intent", func(f *fake, ev *evaluation) { ev.intent = nil }},
		{"an intent that is not resolved", func(f *fake, ev *evaluation) {
			ev.intent.View = decide.IntentView{Version: 2, MaterialFlags: []string{"x"}}
		}},
	} {
		f := &fake{set: set}
		ev := newEv(t, f)
		c.set(f, ev)
		d, trace := run(t, ev, decide.T12Clocks)
		failedClosed(t, c.name, d, trace, decide.T12Clocks)
	}
}

func TestT12IsDeterministic(t *testing.T) {
	var ds []decide.Decision
	var trs [][]decide.TraceEntry
	for i := 0; i < 2; i++ {
		f := &fake{set: ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{clockAt("fin.clock.a", 100)}}, routing: ClockRouting{Escalate: true, ReasonCode: "fin.reason.sla_at_risk"}}
		d, tr := run(t, newEv(t, f), decide.T12Clocks)
		ds, trs = append(ds, d), append(trs, tr)
	}
	if !reflect.DeepEqual(ds[0], ds[1]) || !reflect.DeepEqual(trs[0], trs[1]) {
		t.Fatalf("two runs differ: %v / %v", ds, trs)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------
// Wiring

// T10 to T12 are in the fixed order's table, not notBuilt: the steps the evaluation runs call their ports. T7 to T9 and T13 to T15 are still not built.
func TestTheEvaluationOrderRunsT10ToT12AndLeavesTheRestNotBuilt(t *testing.T) {
	f := &fake{rec: Reconciliation{Classification: reconcile.SuccessConfirmed}, pol: decided(policy.Permit, ""), set: ClockSet{AtRisk: quarterOf}}
	ev := newEv(t, f)
	steps := ev.steps()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for step, port := range map[decide.GateStep]string{decide.T10Reconciliation: "Reconcile", decide.T11Policy: "Evaluate", decide.T12Clocks: "Applicable"} {
		steps[step-1](ctx)
		if !f.called(port) {
			t.Errorf("%v: %s was not called: the step is not wired", step, port)
		}
	}
	for _, step := range []decide.GateStep{decide.T7TargetResolution, decide.T8BusinessOwnership, decide.T9Evidence, decide.T13Confirmation, decide.T14Plan, decide.T15DecisionRecorded} {
		if err := steps[step-1](ctx).Err(); !errors.Is(err, ErrNotBuilt) {
			t.Errorf("%v: %v, want ErrNotBuilt", step, err)
		}
	}
}

func TestAMissingT10ToT12PortIsAConfigError(t *testing.T) {
	f := &fake{}
	for _, name := range []string{"Reconciler", "Policies", "Clocks"} {
		svc := configuredService(f)
		switch name {
		case "Reconciler":
			svc.Ports.Reconciler = nil
		case "Policies":
			svc.Ports.Policies = nil
		case "Clocks":
			svc.Ports.Clocks = nil
		}
		if err := svc.configured(); !errors.Is(err, ErrConfig) {
			t.Errorf("no %s: %v, want ErrConfig", name, err)
		}
	}
	if err := configuredService(f).configured(); err != nil {
		t.Errorf("a fully wired service: %v", err)
	}
}

type noPorts struct{}

func (noPorts) Get(context.Context, string, string, int) (AdmittedIntent, error) {
	return AdmittedIntent{}, ErrNotFound
}
func (noPorts) LatestVersion(context.Context, string, string) (int, error) { return 0, ErrNotFound }
func (noPorts) Verify(context.Context, AdmittedIntent) error               { return ErrInvalid }
func (noPorts) NeedsWrite(context.Context, AdmittedIntent) (bool, error)   { return false, nil }
func (noPorts) Enabled(context.Context, string) (bool, error)              { return false, nil }
func (noPorts) Requested(context.Context, AdmittedIntent) (Capability, bool, error) {
	return Capability{}, false, nil
}
func (noPorts) InScope(context.Context, string, string) (bool, error)     { return false, nil }
func (noPorts) RoleAllowed(context.Context, string, string) (bool, error) { return false, nil }

func configuredService(f *fake) *Service {
	n := noPorts{}
	return &Service{Ports: Ports{Intents: n, Writes: n, Capabilities: n, Scope: n, Authority: n, Reconciler: f, Policies: f, Clocks: f}, Clock: clock.NewManual(t0), PackVersion: "1.0.0"}
}
