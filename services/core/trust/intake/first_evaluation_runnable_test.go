package intake

// A511 (interim, relayed, not yet committed; REQ-TRU-004, REQ-PLN-002; INV-03, INV-02): the plan request of a FIRST evaluation of a write carries the capabilities Trust itself says
// may be planned now: the authorized set (T4/T11, the intent's and the tenant's policy) intersected with the Runnable port's answer. An empty intersection is never sent to the
// Planner, where an empty list means "plan the first steps as before": it is UNKNOWN PLAN_INVALID at T14 and nothing is planned or recorded. A port error is never a pass.
// Facts and the evidence packet stay empty on a first evaluation (they come from verified results, which a first evaluation has none of). All data is SYNTHETIC.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func firstEval(t *testing.T, runnable *runFake) (*f13, *capPolicy, *evaluation) {
	t.Helper()
	f := newFake13()
	planFor(f)
	p := &capPolicy{by: map[string]PolicyEvaluation{capCase: permitWith("root", nil), capRoute: permitWith("graph", map[string]string{"owner_queue": tQueue})}}
	ev := policyEval13(t, f, p)
	ev.snapshotID = "evs_synth0001"
	if runnable != nil { // a nil *runFake in the interface would be a port, not no port
		ev.svc.Runnable = runnable
	}
	return f, p, ev
}

// The request names the authorized capabilities that are runnable, sorted, and the Runnable port was asked once with the snapshot and no facts.
func TestA511AFirstEvaluationPlansTheAuthorizedCapabilitiesThatAreRunnable(t *testing.T) {
	for name, c := range map[string]struct{ runnable, want []string }{
		"both runnable":                   {[]string{capRoute, capCase}, []string{capCase, capRoute}},
		"only the root runnable":          {[]string{capCase}, []string{capCase}},
		"one not authorized is dropped":   {[]string{capCase, "fin.cap.not_authorized"}, []string{capCase}},
		"a repeated id is sent once":      {[]string{capCase, capCase}, []string{capCase}},
		"runnable but not authorized all": {[]string{capCase, capRoute, "fin.cap.other"}, []string{capCase, capRoute}},
	} {
		r := &runFake{ids: c.runnable}
		f, _, ev := firstEval(t, r)
		res := ev.run(tctx(t), ev.policySteps())
		if res.Decision.Outcome() != decide.Allow || len(f.planReq) != 1 {
			t.Fatalf("%s: %s %v, Plan called %d times; want ALLOW and one Plan", name, res.Decision.Outcome(), codes13(res.Decision), len(f.planReq))
		}
		if got := f.planReq[0].Capabilities; !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Capabilities %v, want %v", name, got, c.want)
		}
		if got := f.planReq[0]; got.Facts != nil || got.EvidencePacket != "" {
			t.Errorf("%s: a first evaluation carried facts %v and packet %q; want none", name, got.Facts, got.EvidencePacket)
		}
		if len(r.asked) != 1 || r.asked[0].SnapshotID != "evs_synth0001" || r.asked[0].Facts != nil || r.asked[0].IntentNode != ev.intent.IntentNode || r.asked[0].TenantID != ev.intent.TenantID {
			t.Errorf("%s: Runnable was asked %+v, want once, with the snapshot, the intent node and no facts", name, r.asked)
		}
	}
}

// Nothing authorized is runnable: the Planner is never asked with an empty list, which would mean "plan the first steps as before". UNKNOWN PLAN_INVALID, nothing recorded.
func TestA511AnEmptyIntersectionIsPlanInvalidAndTheEmptyListNeverReachesThePlanner(t *testing.T) {
	for name, ids := range map[string][]string{"none runnable": nil, "none authorized": {"fin.cap.other"}} {
		f, _, ev := firstEval(t, &runFake{ids: ids})
		res := ev.run(tctx(t), ev.policySteps())
		planRefused(t, name, f, res)
		if len(f.planReq) != 0 {
			t.Errorf("%s: the Planner was asked %d times, want never", name, len(f.planReq))
		}
	}
}

// A Runnable error is a dependency failure, never none and never all: the Planner and the Recorder are not called.
func TestA511ARunnableErrorOrPanicIsNeverAPass(t *testing.T) {
	f, _, ev := firstEval(t, &runFake{ids: []string{capCase, capRoute}, err: errors.New("injected: the runnable port failed")})
	res := ev.run(tctx(t), ev.policySteps())
	if res.Decision.Outcome() != decide.Unknown || len(f.planReq) != 0 || f.called("Recorder.Record") != 0 {
		t.Fatalf("%s %v, Plan called %d times; want UNKNOWN and no Plan, no Record", res.Decision.Outcome(), codes13(res.Decision), len(f.planReq))
	}
	f, _, ev = firstEval(t, &runFake{panic: true})
	res = ev.run(tctx(t), ev.policySteps())
	if res.Decision.Outcome() != decide.Unknown || len(f.planReq) != 0 || f.called("Recorder.Record") != 0 {
		t.Fatalf("a panic in the port: %s %v, Plan called %d times; want UNKNOWN and no Plan, no Record", res.Decision.Outcome(), codes13(res.Decision), len(f.planReq))
	}
}

// A read has no write step, so the port is not asked; no Runnable port keeps the earlier request (no capabilities).
func TestA511AReadAndANilPortAreAsBefore(t *testing.T) {
	r := &runFake{ids: []string{capCase}}
	f, _, ev := firstEval(t, r)
	ev.late.needsWrite = false
	f.plan, f.rec = Plan{Hash: tPlanHash}, RecordResult{DecisionID: "dec_synth0001"} // a read: no step and no grant
	if res := ev.run(tctx(t), ev.policySteps()); res.Decision.Outcome() != decide.Allow {
		t.Fatalf("a read: %s %v", res.Decision.Outcome(), codes13(res.Decision))
	}
	if len(r.asked) != 0 || len(f.planReq) != 1 || len(f.planReq[0].Capabilities) != 0 {
		t.Errorf("a read: Runnable asked %d times, request %+v; want no ask and no capabilities", len(r.asked), f.planReq)
	}

	f, _, ev = firstEval(t, nil)
	if res := ev.run(tctx(t), ev.policySteps()); res.Decision.Outcome() != decide.Allow || len(f.planReq) != 1 || len(f.planReq[0].Capabilities) != 0 {
		t.Errorf("no port: %s, requests %+v; want ALLOW with no capabilities", res.Decision.Outcome(), f.planReq)
	}
}

// T13 asks the Planner for the plan too (to bind a new confirmation request) and T14 again: the port is asked once for the whole evaluation.
func TestA511TheRunnablePortIsAskedOncePerEvaluation(t *testing.T) {
	r := &runFake{ids: []string{capCase, capRoute}}
	f, _, ev := firstEval(t, r)
	if res := ev.run(tctx(t), ev.policySteps()); res.Decision.Outcome() != decide.Allow {
		t.Fatalf("%s %v", res.Decision.Outcome(), codes13(res.Decision))
	}
	if _, ok, err := ev.validPlan(tctx(t)); err != nil || !ok {
		t.Fatalf("a second plan request: %v %v", ok, err)
	}
	if len(r.asked) != 1 {
		t.Errorf("the port was asked %d times, want once (the plan requests were %d)", len(r.asked), len(f.planReq))
	}
}
