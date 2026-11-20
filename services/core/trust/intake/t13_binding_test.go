package intake

// TRU-07: the Confirmations port carries the fresh binding (FreshBinding), the declined status and the request fields (REQ-TRU-009, INV-25, SoT 43.2a). All data is SYNTHETIC.

import (
	"context"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

const tSetHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func given13() Confirmation {
	return Confirmation{Status: ConfirmationGiven, EventID: "cfe_synth0001", PlanHash: tPlanHash, RequestID: "cfr_synth0001"}
}

func oneBinding(t *testing.T, f *f13) FreshBinding {
	t.Helper()
	if len(f.bindings) != 1 {
		t.Fatalf("Confirmations.Status was called %d times, want once; calls %v", len(f.bindings), f.calls)
	}
	return f.bindings[0]
}

// INV-25, SoT 43.2a: T13 passes the two values only the fresh evaluation knows: the evidence set hash T9 got from the Evidence port and the policy_id@version T11 used.
func TestT13PassesTheEvidenceSetHashOfT9AndThePolicyVersionOfT11(t *testing.T) {
	f := newFake13()
	f.conf = given13()
	ev := newEval13(t, f)
	ev.evidenceSetHash = tSetHash
	ev.policyVersions = []decide.PolicyVersionRef{{PolicyID: "pol_synth01", Version: "3"}}
	run13(t, ev)
	if got, want := oneBinding(t, f), (FreshBinding{EvidenceSetHash: tSetHash, PolicyVersion: "pol_synth01@3"}); got != want {
		t.Errorf("FreshBinding %+v, want %+v", got, want)
	}
}

// INV-25: the same policy listed twice (two capabilities answered by one policy version) is one policy version; the binding names it once.
func TestT13ThePolicyVersionIsTheSameWhenOnePolicyVersionIsListedTwice(t *testing.T) {
	f := newFake13()
	f.conf = given13()
	ev := newEval13(t, f)
	ev.policyVersions = []decide.PolicyVersionRef{{PolicyID: "pol_synth01", Version: "3"}, {PolicyID: "pol_synth01", Version: "3"}}
	run13(t, ev)
	if got := oneBinding(t, f).PolicyVersion; got != "pol_synth01@3" {
		t.Errorf("PolicyVersion %q, want pol_synth01@3", got)
	}
}

// INV-25: with more than one policy in the evaluation the policy of the binding is not one policy, so it is passed empty (the adapter then refuses a Given answer); Trust
// guesses none.
func TestT13WithSeveralPoliciesThePolicyVersionIsEmpty(t *testing.T) {
	for name, versions := range map[string][]decide.PolicyVersionRef{
		"two policies":               {{PolicyID: "pol_synth01", Version: "3"}, {PolicyID: "pol_synth02", Version: "1"}},
		"two versions of one policy": {{PolicyID: "pol_synth01", Version: "2"}, {PolicyID: "pol_synth01", Version: "3"}},
		"no policy took part":        nil,
	} {
		f := newFake13()
		f.conf = given13()
		ev := newEval13(t, f)
		ev.evidenceSetHash = tSetHash
		ev.policyVersions = versions
		run13(t, ev)
		if got, want := oneBinding(t, f), (FreshBinding{EvidenceSetHash: tSetHash}); got != want {
			t.Errorf("%s: FreshBinding %+v, want %+v (policy version empty)", name, got, want)
		}
	}
}

// INV-25: an empty value is passed through as empty and Trust does not fail T13 for it in this version (the adapter refuses a Given answer for it).
func TestT13EmptyBindingValuesArePassedThroughAndFailNothing(t *testing.T) {
	f := newFake13()
	f.conf = given13()
	res := run13(t, newEval13(t, f))
	if got := oneBinding(t, f); got != (FreshBinding{}) {
		t.Errorf("FreshBinding %+v, want the zero value", got)
	}
	if res.Decision.Outcome() != decide.Allow {
		t.Errorf("outcome %s, want ALLOW (Trust does not fail T13 for an empty value)", res.Decision.Outcome())
	}
}

// INV-25: T9 keeps the evidence set hash the Evidence port gave, in the evaluation, for T13.
func TestT9KeepsTheEvidenceSetHashForT13(t *testing.T) {
	f := newFake()
	f.evid.EvidenceSetHash = tSetHash
	e := newEval(t, resolvedView(), f, nil)
	d, trace := runStep(t, e, decide.T9Evidence)
	continued(t, "a well-formed hash", d, trace, decide.T9Evidence)
	if e.evidenceSetHash != tSetHash {
		t.Errorf("kept %q, want %q", e.evidenceSetHash, tSetHash)
	}
	f = newFake()
	e = newEval(t, resolvedView(), f, nil)
	d, trace = runStep(t, e, decide.T9Evidence)
	continued(t, "no hash given", d, trace, decide.T9Evidence)
	if e.evidenceSetHash != "" {
		t.Errorf("kept %q for an adapter that gave none, want empty", e.evidenceSetHash)
	}
}

// INV-16: a non-empty evidence set hash that is not in the contract's form is an unusable answer: UNKNOWN at T9, and the Confirmations port is never reached.
func TestT9AMalformedEvidenceSetHashIsUnknownAndConfirmationsIsNeverCalled(t *testing.T) {
	for name, h := range map[string]string{"upper case": "sha256:" + "A" + tSetHash[8:], "too short": "sha256:abc", "no prefix": tSetHash[7:], "another algorithm": "sha512:" + tSetHash[7:], "a space": " " + tSetHash} {
		f := newFake()
		f.evid.EvidenceSetHash = h
		e := newEval(t, resolvedView(), f, nil)
		c := newFake13()
		c.conf = given13()
		e.svc.Ports.Confirmations = c
		var steps decide.Steps
		for i := range steps {
			steps[i] = func(context.Context) decide.StepResult { return decide.Continue() }
		}
		steps[decide.T9Evidence-1], steps[decide.T13Confirmation-1] = e.t9, e.t13
		cx := tctx(t)
		d, trace := decide.Evaluate(cx, tVersion, steps)
		failed(t, name, d, trace, decide.T9Evidence, decide.StepFailed)
		if len(c.bindings) != 0 || c.called("Confirmations.Status") != 0 {
			t.Errorf("%s: Confirmations.Status was called after a malformed evidence set hash", name)
		}
		if e.evidenceSetHash != "" {
			t.Errorf("%s: the malformed hash %q was kept", name, e.evidenceSetHash)
		}
	}
}

// REQ-TRU-009: Declined is a status of its own: not the zero value, and none of the other three.
func TestConfirmationDeclinedIsItsOwnStatus(t *testing.T) {
	d := ConfirmationDeclined
	if d == 0 {
		t.Error("ConfirmationDeclined is the zero value, which is not a status")
	}
	for name, other := range map[string]ConfirmationStatus{"NotRequired": ConfirmationNotRequired, "Pending": ConfirmationPending, "Given": ConfirmationGiven} {
		if d == other {
			t.Errorf("ConfirmationDeclined equals %s", name)
		}
	}
	if ConfirmationNotRequired != 1 || ConfirmationPending != 2 || ConfirmationGiven != 3 || d != 4 {
		t.Errorf("the values are %d %d %d %d, want 1 2 3 4", ConfirmationNotRequired, ConfirmationPending, ConfirmationGiven, d)
	}
}

// REQ-TRU-009, SoT 43.2a (the Founder's order of 19 Nov 2026, CR docs#209, interim until committed): a Declined answer ends T13 in REJECT DECLINED_BY_APPROVER, not UNKNOWN. This
// replaces the interim test of noema#757 that pinned "Declined is UNKNOWN", the reading this decision supersedes. A435 (interim, relayed): the REJECT is recorded once; nothing is
// planned or requested.
func TestT13DeclinedIsRejectNotUnknownRecordedOnceAndNothingIsPlannedOrRequested(t *testing.T) {
	for name, c := range map[string]Confirmation{
		"a bare decline":                {Status: ConfirmationDeclined, RequestID: "cfr_synth0001"},
		"a decline that names an event": {Status: ConfirmationDeclined, RequestID: "cfr_synth0001", EventID: "cfe_synth0001", PlanHash: tPlanHash},
		"a decline with a new request":  {Status: ConfirmationDeclined, RequestID: "cfr_synth0001", NewRequest: &RequestSpec{Requirement: "SELF", TTL: time.Minute}},
	} {
		f := newFake13()
		f.conf = c
		f.rec = RecordResult{DecisionID: "dec_synth0002"}
		res := run13(t, newEval13(t, f))
		if res.Decision.Outcome() != decide.Reject {
			t.Errorf("%s: outcome %s %v, want REJECT", name, res.Decision.Outcome(), codes13(res.Decision))
		}
		if f.called("Planner.Plan") != 0 || f.called("Recorder.Record") != 1 || f.called("Recorder.RequestConfirmation") != 0 {
			t.Errorf("%s: want one Record and no plan or request creation after a decline: %v", name, f.calls)
		}
	}
}

// REQ-TRU-009: Pending still stops with CONFIRM, whether an open request exists (RequestID) or Trust must create one (NewRequest); the new fields change nothing in this version.
func TestT13PendingWithARequestIDOrANewRequestStillYieldsConfirm(t *testing.T) {
	for name, c := range map[string]Confirmation{
		"an open request":   {Status: ConfirmationPending, RequestID: "cfr_synth0001"},
		"a request to make": {Status: ConfirmationPending, NewRequest: &RequestSpec{Requirement: "SELF", TTL: 15 * time.Minute}},
	} {
		f := newFake13()
		f.conf = c
		res := run13(t, newEval13(t, f))
		if res.Decision.Outcome() != decide.Confirm {
			t.Errorf("%s: outcome %s, want CONFIRM", name, res.Decision.Outcome())
		}
		if lastTrace(res).Step != decide.T13Confirmation || f.called("Planner.Plan") != 0 || f.called("Recorder.Record") != 0 {
			t.Errorf("%s: trace %v, calls %v, want a stop at T13 with nothing planned or recorded", name, res.Trace, f.calls)
		}
	}
}
