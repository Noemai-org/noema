package intake

import (
	"reflect"
	"sort"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// Codex P1 and P2 and C2's note on noema#572 (REQ-TRU-003, REQ-TRU-004, REQ-CAP-002; INV-03, INV-17, INV-18): a plan can only use the capabilities the intent and the tenant's
// policy authorize (pack data, read through Capabilities.Authorized); the dispatch budget is asked for every one of them before T15 can grant; the Recorder's answer names
// each grant once.

const (
	capCase  = "fin.cap.create_escalation_case"
	capRoute = "fin.cap.route_to_internal_owner"
)

// Codex P1: a planner that substitutes another capability, valid and allowed for the role, on the same target, gets no grant: PLAN_INVALID and nothing is recorded.
func TestT14AValidRoleAllowedCapabilityOutsideTheAuthorizedSetIsPlanInvalidAndNothingIsGranted(t *testing.T) {
	f := newFake13()
	f.authorized = []string{capCase}
	f.plan = writePlan(stepOn(capRoute, "txn_synth0001")) // registered, not prohibited, role-allowed (newFake13), target the intent's own
	res := run13(t, newEval13(t, f))
	planRefused(t, "a substituted capability", f, res)
}

// The same plan with both capabilities authorized is recorded: the refusal above is the set, not the registry.
func TestT14ACapabilityInTheAuthorizedSetIsGranted(t *testing.T) {
	f := newFake13()
	f.authorized = []string{capCase, capRoute}
	f.plan = writePlan(stepOn(capCase, "txn_synth0001"), stepOn(capRoute, "txn_synth0001"))
	f.rec = RecordResult{DecisionID: "dec_synth0001", GrantIDs: []string{"grt_synth0001", "grt_synth0002"}}
	res := run13(t, newEval13(t, f))
	if res.Decision.Outcome() != decide.Allow || f.called("Recorder.Record") != 1 {
		t.Errorf("%s %v, Record called %d times; want ALLOW and one Record", res.Decision.Outcome(), codes13(res.Decision), f.called("Recorder.Record"))
	}
}

// The registry's answer is not the authorization: a step that is not authorized is refused even when no other check would stop it, and one authorized set is read for the
// whole evaluation, not once per step.
func TestT14TheAuthorizedSetIsReadOncePerEvaluationAndAnEmptySetAuthorizesNothing(t *testing.T) {
	f := newFake13()
	f.authorized = []string{}
	f.plan = writePlan(stepOn(capCase, "txn_synth0001"), stepOn(capRoute, "txn_synth0001"))
	planRefused(t, "an empty set", f, run13(t, newEval13(t, f)))
	if f.authAsked != 1 {
		t.Errorf("the authorized set was read %d times, want once", f.authAsked)
	}
}

// An authorized set that cannot be read is not "none authorized" and not "all": the evaluation ends UNKNOWN and nothing is recorded.
func TestT14AnAuthorizedSetThatCannotBeReadFailsClosed(t *testing.T) {
	f := newFake13()
	f.authErr = errInjected
	res := run13(t, newEval13(t, f))
	wantUnknown(t, "the set cannot be read", res, "INTERNAL_ERROR", decide.T14Plan, decide.StepFailed)
	if f.called("Recorder.Record") != 0 {
		t.Errorf("a decision was recorded: %v", f.calls)
	}
}

// Codex P2: the budget of every authorized capability is asked at T12, in a stable order; a spent one escalates DISPATCH_BUDGET_EXHAUSTED, so no step of that capability can
// receive an ALLOW grant later. The intent's own capability is always in the set.
func TestT12AnAuthorizedCapabilityWhoseBudgetIsSpentEscalatesAndEveryOneIsAsked(t *testing.T) {
	f := &fake{dispatch: DispatchCount{Used: 0, Limit: 1}, authorized: []string{capRoute, capCase}, spent: map[string]bool{capRoute: true}}
	ev := newEv(t, f).withCapability(tWriteCap)
	ev.intent.TargetRef, ev.intent.CorrelationID = "txn_synth0001", "corr_synth0001"
	d, trace := run(t, ev, decide.T12Clocks)
	stopped(t, "a spent budget of another authorized capability", d, trace, decide.T12Clocks, decide.Escalate, []string{"DISPATCH_BUDGET_EXHAUSTED"}, escalateAtT12)
	var asked []string
	for _, k := range f.gotDispatch {
		if k.TargetRef != "txn_synth0001" || k.CorrelationID != "corr_synth0001" || k.TenantID != tTenant {
			t.Errorf("the ledger was asked about %+v", k)
		}
		asked = append(asked, k.CapabilityID)
	}
	// the authorized set and the intent's own capability, sorted, asked until the first spent one (capRoute)
	all := []string{capCase, capRoute, tWriteCap}
	sort.Strings(all)
	var want []string
	for _, id := range all {
		want = append(want, id)
		if id == capRoute {
			break
		}
	}
	if !reflect.DeepEqual(asked, want) {
		t.Errorf("the ledger was asked about %v, want %v", asked, want)
	}
}

func TestT12EveryAuthorizedCapabilityWithBudgetLeftGoesOn(t *testing.T) {
	f := &fake{dispatch: DispatchCount{Used: 0, Limit: 1}, authorized: []string{capCase, capRoute}}
	ev := newEv(t, f).withCapability(tWriteCap)
	ev.intent.TargetRef, ev.intent.CorrelationID = "txn_synth0001", "corr_synth0001"
	_, trace := run(t, ev, decide.T12Clocks)
	wentOn(t, "budget left for all", trace, decide.T12Clocks)
	if len(f.gotDispatch) != 3 {
		t.Errorf("the ledger was asked %d times, want 3 (the two authorized and the intent's own): %+v", len(f.gotDispatch), f.gotDispatch)
	}
}

func TestT12AnAuthorizedSetThatCannotBeReadFailsClosed(t *testing.T) {
	f := &fake{dispatch: DispatchCount{Used: 0, Limit: 1}, authErr: errInjected}
	ev := newEv(t, f).withCapability(tWriteCap)
	ev.intent.TargetRef, ev.intent.CorrelationID = "txn_synth0001", "corr_synth0001"
	d, trace := run(t, ev, decide.T12Clocks)
	failedClosed(t, "the set cannot be read", d, trace, decide.T12Clocks)
}

// C2's note on noema#572: a Recorder that names one grant twice for two steps is not trusted (one grant for each write step, each its own).
func TestT15ARecorderThatRepeatsAGrantIDIsUnknown(t *testing.T) {
	f := newFake13()
	f.plan = writePlan(stepOn(capCase, "txn_synth0001"), stepOn(capRoute, "txn_synth0001"))
	f.rec = RecordResult{DecisionID: "dec_synth0001", GrantIDs: []string{"grt_synth0001", "grt_synth0001"}}
	wantUnknown(t, "a repeated grant ID", run13(t, newEval13(t, f)), "INTERNAL_ERROR", decide.T15DecisionRecorded, decide.StepFailed)
}
