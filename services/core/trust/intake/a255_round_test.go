package intake

import (
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

// The second round of noema#572 (rulings A224, A225, A254, A255, interim; the Security Reviewer's notes 5 and 7). REQ-TRU-002, REQ-TRU-003, REQ-TRU-006, REQ-RES-002; INV-03, INV-16,
// INV-21. All data is SYNTHETIC.

// ---- T10: a frozen target (A225, A254) ----

// A225: reads continue on a frozen target; only a write the freeze blocks is refused, and as a typed refusal (TARGET_FROZEN), not an internal error.
func TestT10AFrozenTargetRefusesAWriteTheFreezeBlocksAsATypedRefusal(t *testing.T) {
	f := &fake{rec: Reconciliation{Classification: reconcile.ContradictoryState, Frozen: true, AllowedUnderFreeze: []string{tCreateCap}}}
	d, trace := run(t, newEv(t, f).withCapability(tWriteCap), decide.T10Reconciliation)
	stopped(t, "frozen, write not allowed under the freeze", d, trace, decide.T10Reconciliation, decide.Reject, []string{"TARGET_FROZEN"}, escalateAtT10)
}

func TestT10AReadContinuesOnAFrozenTarget(t *testing.T) {
	f := &fake{rec: Reconciliation{Classification: reconcile.ContradictoryState, Frozen: true, AllowedUnderFreeze: []string{tCreateCap}}}
	_, trace := run(t, newEv(t, f), decide.T10Reconciliation)
	wentOn(t, "frozen target, no capability named (a read)", trace, decide.T10Reconciliation)
}

// ---- T12: a breached clock the pack does not route (A225, A254) ----

// A225: if the pack's routing omits a breached clock, T12 escalates by itself with the core code, never the pack's.
func TestT12ABreachedClockThePackDoesNotRouteEscalatesByItself(t *testing.T) {
	set := ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{clockAt("fin.clock.breached", 0)}}
	f := &fake{set: set, routing: ClockRouting{Escalate: false}}
	d, trace := run(t, newEv(t, f), decide.T12Clocks)
	stopped(t, "breached, unrouted", d, trace, decide.T12Clocks, decide.Escalate, []string{"CLOCK_BREACHED_UNROUTED"}, escalateAtT12)
}

func TestT12AClockThatIsNotBreachedStillGoesOnWhenThePackDoesNotRouteIt(t *testing.T) {
	set := ClockSet{AtRisk: quarterOf, Clocks: []ClockInstance{clockAt("fin.clock.a", 100)}}
	f := &fake{set: set, routing: ClockRouting{Escalate: false}}
	ev := newEv(t, f)
	_, trace := run(t, ev, decide.T12Clocks)
	wentOn(t, "running clock, unrouted", trace, decide.T12Clocks)
	if got := f.gotStatuses; len(got) != 1 || got[0].Phase == clocks.Breached {
		t.Fatalf("the test needs a clock that is not breached: %+v", got)
	}
}

// ---- T14: the role of the session at each step (A255) ----

// A255 (the safer reading): the registry is asked about the role of the session for every step; a step the role may not use is PLAN_INVALID and nothing is recorded.
func TestT14TheRegistryIsAskedForTheRoleOfTheSessionAtEachStep(t *testing.T) {
	f := newFake13()
	f.plan = writePlan(stepOn("fin.cap.create_escalation_case", "txn_synth0001"), stepOn("fin.cap.route_to_internal_owner", "txn_synth0001"))
	f.rec = RecordResult{DecisionID: "dec_synth0001", GrantIDs: []string{"grt_synth0001", "grt_synth0002"}}
	run13(t, newEval13(t, f))
	if !reflect.DeepEqual(f.roles, []string{"customer", "customer"}) {
		t.Errorf("the registry was asked about roles %v, want the session's role for each of the two steps", f.roles)
	}
}

func TestT14AStepTheRoleMayNotUseIsPlanInvalidAndNothingIsRecorded(t *testing.T) {
	f := newFake13()
	c := f.caps["fin.cap.create_escalation_case"]
	c.RoleAllowed = false
	f.caps["fin.cap.create_escalation_case"] = c
	res := run13(t, newEval13(t, f))
	planRefused(t, "a step the role may not use", f, res)
}

// ---- after T14: the dispatch budget (A224, A254) ----

// A224: when a key's dispatches have reached the ledger's limit, Trust itself decides ESCALATE (DISPATCH_BUDGET_EXHAUSTED); the runner's refusal is only the backstop. Nothing is recorded.
func TestT14AStepWhoseDispatchBudgetIsSpentEscalatesAndNothingIsRecorded(t *testing.T) {
	f := newFake13()
	f.dispatch = DispatchCount{Used: 1, Limit: 1}
	res := run13(t, newEval13(t, f))
	if res.Decision.Outcome() != decide.Escalate || !reflect.DeepEqual(codes13(res.Decision), []string{"DISPATCH_BUDGET_EXHAUSTED"}) {
		t.Fatalf("%s %v, want ESCALATE DISPATCH_BUDGET_EXHAUSTED", res.Decision.Outcome(), codes13(res.Decision))
	}
	if got := lastTrace(res); got.Step != decide.T14Plan || got.Result != decide.StepTerminal {
		t.Errorf("trace ends %v, want T14 TERMINAL", got)
	}
	if f.called("Recorder.Record") != 0 || res.DecisionID != "" || len(res.GrantIDs) != 0 {
		t.Errorf("an escalation was recorded as a write: %v %q %v", f.calls, res.DecisionID, res.GrantIDs)
	}
	want := DispatchKey{TenantID: t13Tenant, CorrelationID: "corr_synth0001", CapabilityID: "fin.cap.create_escalation_case", TargetRef: "txn_synth0001"}
	if len(f.dispatchKey) != 1 || f.dispatchKey[0] != want {
		t.Errorf("the ledger was asked about %+v, want %+v", f.dispatchKey, want)
	}
}

func TestT14AStepWithBudgetLeftIsAllowedAndAReadAsksNothing(t *testing.T) {
	f := newFake13()
	f.dispatch = DispatchCount{Used: 0, Limit: 1}
	if res := run13(t, newEval13(t, f)); res.Decision.Outcome() != decide.Allow {
		t.Fatalf("%s %v with budget left", res.Decision.Outcome(), codes13(res.Decision))
	}
	r := newFake13()
	r.plan = Plan{Hash: tPlanHash}
	ev := newEval13(t, r)
	ev.late.needsWrite = false
	run13(t, ev)
	if r.called("Dispatches.Count") != 0 {
		t.Errorf("a read asked the dispatch ledger: %v", r.calls)
	}
}

func TestT14ALedgerThatCannotAnswerFailsClosed(t *testing.T) {
	f := newFake13()
	f.dispatchErr = errInjected
	res := run13(t, newEval13(t, f))
	wantUnknown(t, "the dispatch ledger failed", res, "INTERNAL_ERROR", decide.T14Plan, decide.StepFailed)
	if f.called("Recorder.Record") != 0 {
		t.Errorf("recorded after the ledger failed: %v", f.calls)
	}
}

// ---- T15: the Recorder reads a sealed copy of the basis (the Security Reviewer's note 5) ----

func TestT15TheRecorderIsHandedASealedCopyOfTheBuilderNotTheLiveOne(t *testing.T) {
	f := newFake13()
	ev := newEval13(t, f)
	run13(t, ev)
	got := rec0(t, f).Evaluated
	if got == ev.bb {
		t.Fatal("the Recorder was handed the live builder of the evaluation")
	}
	a, errA := got.Digest()
	b, errB := ev.bb.Digest()
	if errA != nil || errB != nil || a != b {
		t.Errorf("the copy's digest %q (%v) is not the builder's %q (%v)", a, errA, b, errB)
	}
	if err := got.RecordPolicy(nil); err == nil {
		t.Error("the Recorder could add to the evaluated set it was handed")
	}
}

// ---- T13 to T15: the record request follows the inputs, and only the inputs (the Security Reviewer's note 7) ----

// INV-21: a test that can fail. One input is changed at a time and the record request must change in exactly the field that input feeds: a request that ignored the plan, hard-wired
// a step or dropped the confirmation would pass a test that only compares two identical runs.
func TestT13ToT15TheRecordRequestFollowsTheInputsAndOnlyTheInputs(t *testing.T) {
	base := func() *f13 { return newFake13() }
	run := func(mutate func(*f13)) RecordRequest {
		f := base()
		mutate(f)
		run13(t, newEval13(t, f))
		return rec0(t, f)
	}
	ref := run(func(*f13) {})
	other := "sha256:" + "ab"[:1] + ref.PlanHash[8:]
	cases := []struct {
		name   string
		mutate func(*f13)
		field  string
	}{
		{"the plan hash", func(f *f13) { f.plan.Hash = other }, "PlanHash"},
		{"the step's target", func(f *f13) { f.plan.Steps[0].TargetRef = "txn_synth0001" }, ""},
		{"the step's parameters", func(f *f13) { f.plan.Steps[0].ParamsHash = other }, "Steps"},
		{"the confirmation", func(f *f13) {
			f.conf = Confirmation{Status: ConfirmationGiven, EventID: "cfe_synth0009", PlanHash: tPlanHash}
		}, "ConfirmationEventID"},
	}
	for _, c := range cases {
		got := run(c.mutate)
		changed := changedFields(ref, got)
		if c.field == "" {
			if len(changed) != 0 {
				t.Errorf("%s: an input that did not change changed %v", c.name, changed)
			}
			continue
		}
		if len(changed) == 0 || changed[0] != c.field {
			t.Errorf("%s: the request changed in %v, want %s", c.name, changed, c.field)
		}
		for _, other := range changed[1:] {
			if other != "ConfirmationPlanHash" && other != "PlanHash" {
				t.Errorf("%s: the request also changed %s", c.name, other)
			}
		}
	}
}

func changedFields(a, b RecordRequest) []string {
	var out []string
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	for i := 0; i < av.NumField(); i++ {
		name := av.Type().Field(i).Name
		if name == "Evaluated" || name == "Decision" {
			continue
		}
		if !reflect.DeepEqual(av.Field(i).Interface(), bv.Field(i).Interface()) {
			out = append(out, name)
		}
	}
	return out
}
