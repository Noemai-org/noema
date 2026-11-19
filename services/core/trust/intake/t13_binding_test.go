package intake

// TRU-07: the Confirmations port carries the fresh binding (FreshBinding), the declined status and the request fields (REQ-TRU-009, INV-25, SoT 43.2a). All data is SYNTHETIC.

import (
	"context"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

const tSetHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// The composite policy versions (A341, interim): "sha256:" and the SHA-256 hex of the RFC 8785 form of the sorted, distinct list of "policy_id@version" strings, computed by an
// independent Python implementation (see trust/decide policyset_test.go). SYNTHETIC policies.
const (
	tPolicySet     = "sha256:50ca8c31e714463c1257e6e3f8b3f6c57fdb0c3fe471e8a2f015fa2b4f539ebb" // ["pol_synth01@3"]
	tPolicySetTwo  = "sha256:f81f4a1916c40274bfeb88b814e9b98bc36d43393b9cff1249f6b39c1fe364d1" // ["pol_synth01@3","pol_synth02@1"]
	tPolicySetVers = "sha256:52a779a7fd72828f57e07726d30090c0beacc19b4b76fc1f06573b45d1d0c5d7" // ["pol_synth01@2","pol_synth01@3"]
)

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
	if got, want := oneBinding(t, f), (FreshBinding{EvidenceSetHash: tSetHash, PolicyVersion: tPolicySet}); got != want {
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
	if got := oneBinding(t, f).PolicyVersion; got != tPolicySet {
		t.Errorf("PolicyVersion %q, want %s", got, tPolicySet)
	}
}

// A341 (interim), INV-25: the policy version of the binding is the composite of EVERY policy version that took part in T11 ("sha256:" and the hash of the sorted, distinct list), whether
// there is one, several policies or several versions of one policy; no policy, or one that cannot be bound, gives an empty value (decide.CompositePolicyVersion refuses it). The
// answer does not depend on the order T11 listed them in. The status is NotRequired so that the test looks at what was passed only.
func TestT13ThePolicyVersionIsTheCompositeOfEveryPolicyVersionOfT11(t *testing.T) {
	for name, tc := range map[string]struct {
		versions []decide.PolicyVersionRef
		want     string
	}{
		"two policies":                      {[]decide.PolicyVersionRef{{PolicyID: "pol_synth01", Version: "3"}, {PolicyID: "pol_synth02", Version: "1"}}, tPolicySetTwo},
		"the same two in the other order":   {[]decide.PolicyVersionRef{{PolicyID: "pol_synth02", Version: "1"}, {PolicyID: "pol_synth01", Version: "3"}}, tPolicySetTwo},
		"two policies and a repeat":         {[]decide.PolicyVersionRef{{PolicyID: "pol_synth01", Version: "3"}, {PolicyID: "pol_synth02", Version: "1"}, {PolicyID: "pol_synth01", Version: "3"}}, tPolicySetTwo},
		"two versions of one policy":        {[]decide.PolicyVersionRef{{PolicyID: "pol_synth01", Version: "2"}, {PolicyID: "pol_synth01", Version: "3"}}, tPolicySetVers},
		"no policy took part":               {nil, ""},
		"a policy version that cannot bind": {[]decide.PolicyVersionRef{{PolicyID: "pol synth01", Version: "3"}}, ""},
		"an empty version among good ones":  {[]decide.PolicyVersionRef{{PolicyID: "pol_synth01", Version: "3"}, {PolicyID: "pol_synth02", Version: ""}}, ""},
		"an @ inside a policy ID":           {[]decide.PolicyVersionRef{{PolicyID: "pol@synth01", Version: "3"}}, ""},
	} {
		f := newFake13()
		ev := newEval13(t, f)
		ev.evidenceSetHash = tSetHash
		ev.policyVersions = tc.versions
		run13(t, ev)
		if got, want := oneBinding(t, f), (FreshBinding{EvidenceSetHash: tSetHash, PolicyVersion: tc.want}); got != want {
			t.Errorf("%s: FreshBinding %+v, want %+v", name, got, want)
		}
	}
}

// INV-25, INV-16 (fail closed, TRU-07 A341 follow-up): a Given answer is refused by T13 itself when the FreshBinding it passed has an empty EvidenceSetHash or an empty PolicyVersion: the
// adapter must refuse it, and Trust does not rely on it. UNKNOWN INTERNAL_ERROR at T13, the answer is discarded (no confirmation event is kept, so nothing can follow it), the Planner and
// the Recorder are never called, and no grant exists.
func TestT13GivenWithAnEmptyFreshValueIsUnknownAndTheAnswerIsDiscarded(t *testing.T) {
	for name, mutate := range map[string]func(*evaluation){
		"an empty evidence set hash": func(e *evaluation) { e.evidenceSetHash = "" },
		"no policy version":          func(e *evaluation) { e.policyVersions = nil },
		"a policy version that cannot be bound": func(e *evaluation) {
			e.policyVersions = []decide.PolicyVersionRef{{PolicyID: "pol synth01", Version: "3"}}
		},
		"both empty": func(e *evaluation) { e.evidenceSetHash, e.policyVersions = "", nil },
	} {
		f := newFake13()
		f.conf = given13()
		ev := newEval13(t, f)
		mutate(ev)
		res := run13(t, ev)
		wantUnknown(t, name, res, "INTERNAL_ERROR", decide.T13Confirmation, decide.StepFailed)
		if f.called("Confirmations.Status") != 1 || f.called("Planner.Plan") != 0 || f.called("Recorder.Record") != 0 {
			t.Errorf("%s: calls %v, want Status once and nothing else", name, f.calls)
		}
		if ev.late.confirmationID != "" || ev.late.confirmationPlanHash != "" {
			t.Errorf("%s: the Given answer was kept: event %q, plan hash %q", name, ev.late.confirmationID, ev.late.confirmationPlanHash)
		}
		if res.DecisionID != "" || len(res.GrantIDs) != 0 {
			t.Errorf("%s: the result names decision %q and grants %v", name, res.DecisionID, res.GrantIDs)
		}
	}
}

// INV-25: the refusal is for Given only. Empty values are still passed through to Status (the adapter decides what it needs), NotRequired goes on, and Pending with an open request is
// recorded as before (the request values were bound when it was created; Pending with a request to make already refuses empty values, see t13_confirm_test.go).
func TestT13EmptyFreshValuesAreStillPassedAndFailOnlyAGivenAnswer(t *testing.T) {
	f := newFake13()
	ev := newEval13(t, f)
	ev.evidenceSetHash, ev.policyVersions = "", nil
	res := run13(t, ev)
	if got := oneBinding(t, f); got != (FreshBinding{}) {
		t.Errorf("FreshBinding %+v, want the zero value", got)
	}
	if res.Decision.Outcome() != decide.Allow {
		t.Errorf("NotRequired with empty values: outcome %s, want ALLOW", res.Decision.Outcome())
	}
	g := pendingOpen(newFake13())
	ev = confirmEval(t, g)
	ev.evidenceSetHash, ev.policyVersions = "", nil
	if res := run13(t, ev); res.Decision.Outcome() != decide.Confirm || g.called("Recorder.Record") != 1 {
		t.Errorf("Pending with an open request and empty values: outcome %s, calls %v, want CONFIRM recorded once", res.Decision.Outcome(), g.calls)
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
