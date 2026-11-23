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

// A225, A254: reads continue on a frozen target; a write the freeze blocks ends in a typed ESCALATE (TARGET_FROZEN; PERD 23.3 gives T10 no REJECT), not an internal error.
func TestT10AFrozenTargetEscalatesAWriteTheFreezeBlocksWithATypedReason(t *testing.T) {
	f := &fake{rec: Reconciliation{Classification: reconcile.ContradictoryState, Frozen: true, AllowedUnderFreeze: []string{tCreateCap}}}
	d, trace := run(t, newEv(t, f).withCapability(tWriteCap), decide.T10Reconciliation)
	stopped(t, "frozen, write not allowed under the freeze", d, trace, decide.T10Reconciliation, decide.Escalate, []string{"TARGET_FROZEN"}, escalateAtT10)
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

// ---- the dispatch budget (A224, A254) ----

// A224: when a key's dispatches have reached the ledger's limit, Trust itself decides ESCALATE (DISPATCH_BUDGET_EXHAUSTED); the runner's refusal is only the backstop. It is
// asked where an ESCALATE may end the evaluation (T12; PERD 23.3 gives T14 an empty stop set), for the capability and the target the intent names.
func TestT12AWriteWhoseDispatchBudgetIsSpentEscalates(t *testing.T) {
	f := &fake{dispatch: DispatchCount{Used: 1, Limit: 1}}
	ev := newEv(t, f).withCapability(tWriteCap)
	ev.intent.TargetRef, ev.intent.CorrelationID = "txn_synth0001", "corr_synth0001"
	d, trace := run(t, ev, decide.T12Clocks)
	stopped(t, "dispatch budget spent", d, trace, decide.T12Clocks, decide.Escalate, []string{"DISPATCH_BUDGET_EXHAUSTED"}, escalateAtT12)
	want := DispatchKey{TenantID: tTenant, CorrelationID: "corr_synth0001", CapabilityID: tWriteCap, TargetRef: "txn_synth0001"}
	if len(f.gotDispatch) != 1 || f.gotDispatch[0] != want {
		t.Errorf("the ledger was asked about %+v, want %+v", f.gotDispatch, want)
	}
}

func TestT12AWriteWithBudgetLeftGoesOnAndAReadAsksNothing(t *testing.T) {
	f := &fake{dispatch: DispatchCount{Used: 0, Limit: 1}}
	ev := newEv(t, f).withCapability(tWriteCap)
	ev.intent.TargetRef, ev.intent.CorrelationID = "txn_synth0001", "corr_synth0001"
	_, trace := run(t, ev, decide.T12Clocks)
	wentOn(t, "budget left", trace, decide.T12Clocks)
	r := &fake{dispatch: DispatchCount{Used: 5, Limit: 1}}
	_, trace = run(t, newEv(t, r), decide.T12Clocks)
	wentOn(t, "a read", trace, decide.T12Clocks)
	if len(r.gotDispatch) != 0 {
		t.Errorf("a read asked the dispatch ledger: %+v", r.gotDispatch)
	}
}

func TestT12ALedgerThatCannotAnswerOrAnswersNothingUsableFailsClosed(t *testing.T) {
	for name, f := range map[string]*fake{
		"an error":               {fail: map[string]bool{"Count": true}},
		"a limit of nothing":     {dispatch: DispatchCount{Used: 0, Limit: 0}},
		"a negative use":         {dispatch: DispatchCount{Used: -1, Limit: 1}},
		"the ledger's own panic": {panics: map[string]bool{"Count": true}},
	} {
		ev := newEv(t, f).withCapability(tWriteCap)
		ev.intent.TargetRef, ev.intent.CorrelationID = "txn_synth0001", "corr_synth0001"
		d, trace := run(t, ev, decide.T12Clocks)
		failedClosed(t, name, d, trace, decide.T12Clocks)
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
			f.conf = Confirmation{Status: ConfirmationGiven, EventID: "cfe_synth0009", PlanHash: tPlanHash, RequestID: "cfr_synth0001"}
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
			if other != "ConfirmationPlanHash" && other != "PlanHash" && other != "ConfirmationBindingEvidenceHash" { // the binding's evidence follows a confirmation (A435)
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
