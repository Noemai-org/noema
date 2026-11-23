package recorder_test

// TRU-07 (PR 2 of the confirmation lanes; REQ-TRU-009, REQ-TRU-008, INV-25, INV-03, INV-12, EC-E16, SoT 43.2a; PERD 32.4): a CONFIRM decision creates its confirmation request in the
// SAME transaction as the decision, as the CONFIRMATION_REQUESTED event right after DECISION_MADE (record first); the writer mints the request ID and computes the binding hash
// itself; a repeated Record returns the stored decision and creates no second request. Real PostgreSQL, real tenant logins, the real hash chain. All data is SYNTHETIC.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/payloadguard"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const (
	cfrTarget    = "fin.transaction:TX88213" // the contract's own example of a ResourceRef: a run of five digits, which the payload guard accepts at /target_ref (ruling A193)
	cfrPrincipal = "usr_synthA1"
	cfrCorr      = "corr_ABC123"
)

var cfrRe = regexp.MustCompile(`^cfr_[0-9A-Za-z]{2,32}$`)

// letters is a hash whose hex digits include letters: the payload guard refuses a content hash that is all digits (a genuine hash is never one, with negligible probability).
func letters(c byte) string { return string(h(10 + c%6)) }

func confirmDecision(t *testing.T, b *decide.BasisBuilder) decide.Decision {
	t.Helper()
	d, err := resolved(t).Confirm(pass)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = b.Attach(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func newCfr(w *world) *recorder.NewConfirmationRequest {
	return &recorder.NewConfirmationRequest{PrincipalID: cfrPrincipal, IntentHash: letters(0), CapabilityID: capability, TargetRef: cfrTarget, ParamsHash: letters(1), ConfirmationBindingEvidenceHash: letters(2),
		PlanHash: letters(3), PolicyVersion: policyVersion, Requirement: "SELF", RequesterID: cfrPrincipal, ExpiresAt: epoch.Unix() + 600}
}

// confirmRequest is a CONFIRM on snapshot evs_01ABC and policy fin.pol@3.1 with a request to create; withRequest false leaves the request out (a CONFIRM that names an open request).
func confirmRequest(t *testing.T, w *world, snapshot string, withRequest bool) recorder.Request {
	t.Helper()
	b := evaluatedWith(t, snapshot, "3.1")
	r := recorder.Request{TenantID: tenant, CorrelationID: cfrCorr, IntentID: "int_01J9X3", IntentHash: contracts.CommonV1Hash(letters(0)), PolicyID: "fin.pol", Decision: confirmDecision(t, b), Evaluated: b}
	if withRequest {
		r.ConfirmationRequest = newCfr(w)
	}
	return r
}

func (w *world) eventsOf(t *testing.T, types ...string) []chain.Row {
	t.Helper()
	rows, err := chain.ReadChain(ctx(t), w.reader)
	if err != nil {
		t.Fatal(err)
	}
	var out []chain.Row
	for _, r := range rows {
		for _, ty := range types {
			if r.EventType == ty {
				out = append(out, r)
			}
		}
	}
	return out
}

func payloadOf(t *testing.T, r chain.Row) map[string]any {
	t.Helper()
	if r.Payload == nil {
		t.Fatalf("%s has no payload", r.EventType)
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(*r.Payload), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (w *world) nothingWritten(t *testing.T, what string) {
	t.Helper()
	for _, q := range []string{`SELECT count(*) FROM decision_record`, `SELECT count(*) FROM record_event`, `SELECT count(*) FROM decision_grant`, `SELECT count(*) FROM confirmation_spend`} {
		if n := w.count(t, q); n != 0 {
			t.Errorf("%s: %q is %d, want nothing written", what, q, n)
		}
	}
}

// REQ-TRU-009, INV-25, EC-E16: one CONFIRM with a request writes exactly one DECISION_MADE and exactly one CONFIRMATION_REQUESTED, adjacent, in one transaction, in one correlation, by
// the Trust emitter; the request carries the eight bound values, the requirement, the requester and the expiry, and its binding hash is confirm.BindingHash of the eight values. The
// DECISION_MADE of the CONFIRM names the request. No grant, no spend.
func TestAConfirmWithARequestWritesOneDecisionAndOneConfirmationRequestInOneTransaction(t *testing.T) {
	w := newWorld(t)
	res, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true)
	if err != nil {
		t.Fatal(err)
	}
	if !cfrRe.MatchString(res.ConfirmationRequestID) {
		t.Fatalf("the request ID %q is not a cfr_ ID", res.ConfirmationRequestID)
	}
	if res.Record.Outcome != "CONFIRM" || len(res.Grants) != 0 {
		t.Errorf("the decision is %s with %d grants", res.Record.Outcome, len(res.Grants))
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record WHERE outcome = 'CONFIRM'`); n != 1 {
		t.Errorf("%d CONFIRM decisions, want 1", n)
	}
	events := w.eventsOf(t, "DECISION_MADE", "CONFIRMATION_REQUESTED")
	if len(events) != 2 || events[0].EventType != "DECISION_MADE" || events[1].EventType != "CONFIRMATION_REQUESTED" {
		t.Fatalf("events %v, want DECISION_MADE then CONFIRMATION_REQUESTED", events)
	}
	dm, cr := events[0], events[1]
	if cr.Seq != dm.Seq+1 {
		t.Errorf("the request event is seq %d, the decision event seq %d: they are not adjacent", cr.Seq, dm.Seq)
	}
	if cr.CorrelationID != cfrCorr || dm.CorrelationID != cfrCorr || cr.WrittenBy != "core-trust" || cr.Actor != dm.Actor {
		t.Errorf("the request event: correlation %q, written_by %q, actor %q (decision actor %q)", cr.CorrelationID, cr.WrittenBy, cr.Actor, dm.Actor)
	}
	if cr.ObjectRef != res.ConfirmationRequestID || dm.ObjectRef != string(res.Record.DecisionId) {
		t.Errorf("object refs: request event %q, decision event %q", cr.ObjectRef, dm.ObjectRef)
	}
	binding := bindingHashOf(t, confirm.BindingInput{PrincipalID: cfrPrincipal, IntentHash: letters(0), CapabilityID: capability, TargetRef: cfrTarget, ParamsHash: letters(1), EvidenceSetHash: letters(2),
		PlanHash: letters(3), PolicyVersion: policyVersion})
	want := map[string]any{"confirmation_request_id": res.ConfirmationRequestID, "decision_id": string(res.Record.DecisionId), "binding_hash": binding, "principal_id": cfrPrincipal,
		"intent_hash": letters(0), "capability_id": capability, "target_ref": cfrTarget, "params_hash": letters(1), "evidence_set_hash": letters(2), "plan_hash": letters(3),
		"policy_version": policyVersion, "approver_requirement": "SELF", "requester_id": cfrPrincipal}
	// A552: the expiry is the database's clock plus the TTL, so it is not a value this test can name; confirmrequest_row_test.go asserts that the event carries the row's own expiry.
	got := payloadOf(t, cr)
	if _, has := got["expires_at"]; !has {
		t.Error("CONFIRMATION_REQUESTED carries no expires_at")
	}
	delete(got, "expires_at")
	if !jsonEqual(t, got, want) {
		t.Errorf("CONFIRMATION_REQUESTED payload %v, want %v", got, want)
	}
	if got := payloadOf(t, dm)["confirmation_request_id"]; got != res.ConfirmationRequestID {
		t.Errorf("the DECISION_MADE names request %v, want %s", got, res.ConfirmationRequestID)
	}
	if _, has := payloadOf(t, dm)["confirmation_event_id"]; has {
		t.Error("the DECISION_MADE of a CONFIRM names a confirmation event")
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`) + w.spends(t); n != 0 {
		t.Errorf("%d grants and spends were written for a CONFIRM", n)
	}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	return string(x) == string(y)
}

// EC-E16: the decision and its request commit together or not at all: a rolled-back write leaves neither, and no event.
func TestARolledBackConfirmLeavesNeitherTheDecisionNorTheRequest(t *testing.T) {
	w := newWorld(t)
	if _, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), false); err != nil {
		t.Fatal(err)
	}
	w.nothingWritten(t, "a rolled-back CONFIRM with a request")
}

// A186, G: a repeated Record of the same CONFIRM tuple returns the stored decision and creates no second request: one decision, one DECISION_MADE, one CONFIRMATION_REQUESTED.
func TestARepeatedConfirmReturnsTheStoredDecisionAndCreatesNoSecondRequest(t *testing.T) {
	w := newWorld(t)
	first, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true)
	if err != nil {
		t.Fatalf("the repeated Record: %v", err)
	}
	if second.Record.DecisionId != first.Record.DecisionId {
		t.Errorf("the repeat returned decision %s, not the stored %s", second.Record.DecisionId, first.Record.DecisionId)
	}
	if second.ConfirmationRequestID != "" {
		t.Errorf("the repeat names a new request %q", second.ConfirmationRequestID)
	}
	if n := len(w.eventsOf(t, "CONFIRMATION_REQUESTED")); n != 1 {
		t.Errorf("%d CONFIRMATION_REQUESTED events after a repeat, want 1", n)
	}
	if n := len(w.eventsOf(t, "DECISION_MADE")); n != 1 {
		t.Errorf("%d DECISION_MADE events after a repeat, want 1", n)
	}
}

// A request is checked whatever the path: a malformed one on a retry of a stored decision is still ErrRequest, not the stored decision.
func TestAMalformedRequestOnARetryOfAStoredConfirmIsStillRefused(t *testing.T) {
	w := newWorld(t)
	if _, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true); err != nil {
		t.Fatal(err)
	}
	r := confirmRequest(t, w, "evs_01ABC", true)
	r.ConfirmationRequest.TargetRef = "fin.transaction:TX 8821"
	if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrRequest) {
		t.Errorf("err %v, want ErrRequest", err)
	}
	if n := len(w.eventsOf(t, "CONFIRMATION_REQUESTED")); n != 1 {
		t.Errorf("%d CONFIRMATION_REQUESTED events, want the one of the first write", n)
	}
}

// A CONFIRM that names an already open request is recorded with no request event and no request member in its DECISION_MADE.
func TestAConfirmWithNoRequestWritesNoRequestEvent(t *testing.T) {
	w := newWorld(t)
	res, err := w.write(t, confirmRequest(t, w, "evs_01ABC", false), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.ConfirmationRequestID != "" || len(w.eventsOf(t, "CONFIRMATION_REQUESTED")) != 0 {
		t.Errorf("a request %q was created for a CONFIRM that carried none", res.ConfirmationRequestID)
	}
	dm := w.eventsOf(t, "DECISION_MADE")
	if len(dm) != 1 {
		t.Fatalf("%d DECISION_MADE events", len(dm))
	}
	if _, has := payloadOf(t, dm[0])["confirmation_request_id"]; has {
		t.Error("the DECISION_MADE names a request that was not created")
	}
}

// Only a CONFIRM may carry a request: on any other outcome it is ErrRequest, refused before anything is written.
func TestARequestOnAnyOutcomeButConfirmIsRefusedAndNothingIsWritten(t *testing.T) {
	for _, o := range []decide.Outcome{decide.Allow, decide.Reject, decide.Wait, decide.Escalate} {
		w := newWorld(t)
		b := evaluatedWith(t, "evs_01ABC", "3.1")
		r := request(t, decision(t, o, b), b)
		r.IntentHash = contracts.CommonV1Hash(letters(0))
		if o == decide.Allow {
			r.Steps = []grants.Step{step(t)}
		}
		r.ConfirmationRequest = newCfr(w)
		if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrRequest) {
			t.Errorf("%s with a request: err %v, want ErrRequest", o, err)
		}
		w.nothingWritten(t, string(o)+" with a request")
	}
}

// Malformed values are ErrRequest before anything is written: the writer re-checks every one (the binding's own refusal of an unsafe or empty value, the contract's forms, the
// decision's own intent hash and bound policy), whatever the intake did.
func TestAMalformedConfirmationRequestIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	for name, mutate := range map[string]func(*recorder.NewConfirmationRequest){
		"no principal":                        func(c *recorder.NewConfirmationRequest) { c.PrincipalID = "" },
		"a principal with a space":            func(c *recorder.NewConfirmationRequest) { c.PrincipalID = "usr synth" },
		"an intent hash of the wrong form":    func(c *recorder.NewConfirmationRequest) { c.IntentHash = "sha256:ABC" },
		"another intent hash than the record": func(c *recorder.NewConfirmationRequest) { c.IntentHash = letters(5) },
		"no capability":                       func(c *recorder.NewConfirmationRequest) { c.CapabilityID = "" },
		"a target with a space":               func(c *recorder.NewConfirmationRequest) { c.TargetRef = "fin.transaction:TX 8821" },
		"no target":                           func(c *recorder.NewConfirmationRequest) { c.TargetRef = "" },
		"a params hash of the wrong form":     func(c *recorder.NewConfirmationRequest) { c.ParamsHash = "x" },
		"no evidence set hash":                func(c *recorder.NewConfirmationRequest) { c.ConfirmationBindingEvidenceHash = "" },
		"a plan hash of the wrong form":       func(c *recorder.NewConfirmationRequest) { c.PlanHash = letters(3)[7:] },
		"no policy version":                   func(c *recorder.NewConfirmationRequest) { c.PolicyVersion = "" },
		"another policy version than bound":   func(c *recorder.NewConfirmationRequest) { c.PolicyVersion = "sha256:" + strings.Repeat("0", 64) },
		"no requirement":                      func(c *recorder.NewConfirmationRequest) { c.Requirement = "" },
		"a requirement not in the enum":       func(c *recorder.NewConfirmationRequest) { c.Requirement = "OWNER" },
		"a role in upper case":                func(c *recorder.NewConfirmationRequest) { c.Requirement = "ROLE:SUPERVISOR" },
		"no requester":                        func(c *recorder.NewConfirmationRequest) { c.RequesterID = "" },
		"no expiry":                           func(c *recorder.NewConfirmationRequest) { c.ExpiresAt = 0 },
		"an expiry that is now":               func(c *recorder.NewConfirmationRequest) { c.ExpiresAt = epoch.Unix() },
		"an expiry in the past":               func(c *recorder.NewConfirmationRequest) { c.ExpiresAt = epoch.Unix() - 1 },
	} {
		w := newWorld(t)
		r := confirmRequest(t, w, "evs_01ABC", true)
		mutate(r.ConfirmationRequest)
		if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrRequest) {
			t.Errorf("%s: err %v, want ErrRequest", name, err)
		}
		w.nothingWritten(t, name)
	}
}

// The contract's requirements are accepted as they are.
func TestEachApproverRequirementOfTheContractIsRecorded(t *testing.T) {
	for _, req := range []string{"SELF", "MAKER_CHECKER", "ROLE:supervisor", "ROLE:l2_support"} {
		w := newWorld(t)
		r := confirmRequest(t, w, "evs_01ABC", true)
		r.ConfirmationRequest.Requirement = req
		if _, err := w.write(t, r, true); err != nil {
			t.Errorf("%s: %v", req, err)
			continue
		}
		cr := w.eventsOf(t, "CONFIRMATION_REQUESTED")
		if len(cr) != 1 || payloadOf(t, cr[0])["approver_requirement"] != req {
			t.Errorf("%s: events %v", req, cr)
		}
	}
}

// cfrIDs mints the given cfr_ value and delegates every other prefix, so a test can tell that the request ID comes from the writer's own generator and from nobody else.
type cfrIDs struct {
	inner *ids.Generator
	cfr   string
}

func (f cfrIDs) ID(prefix string) (string, error) {
	if prefix == "cfr" {
		return f.cfr, nil
	}
	return f.inner.ID(prefix)
}

// "Minted by Trust" (SoT 43.2a): the request ID is the writer's generator's, and a generator that answers with an ID outside the contract's form is ErrRequest, with nothing written.
func TestTheRequestIDIsMintedByTheWriterAndMustBeInTheContractsForm(t *testing.T) {
	w := newWorld(t)
	w.w.IDs = cfrIDs{inner: ids.New(w.w.Clock, nil), cfr: "cfr_MINTED01"}
	res, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.ConfirmationRequestID != "cfr_MINTED01" {
		t.Errorf("the request ID is %q, want the writer's generator's cfr_MINTED01", res.ConfirmationRequestID)
	}
	if cr := w.eventsOf(t, "CONFIRMATION_REQUESTED"); len(cr) != 1 || cr[0].ObjectRef != "cfr_MINTED01" {
		t.Errorf("events %v", cr)
	}
	for _, bad := range []string{"", "cfr_", "dec_MINTED01", "cfr_has space"} {
		w := newWorld(t)
		w.w.IDs = cfrIDs{inner: ids.New(w.w.Clock, nil), cfr: bad}
		if _, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true); !errors.Is(err, recorder.ErrRequest) {
			t.Errorf("a generator that answers %q: err %v, want ErrRequest", bad, err)
		}
		w.nothingWritten(t, "a request ID "+bad)
	}
}

// REQ-TRU-009 (the order): a REJECT DECLINED_BY_APPROVER decision is a decision like any other for the writer: stored with its reason code, no grant, no step, no request, no spend.
// (T13 does not return it yet: status/TRU-07-confirm-t13.md.) A597: it names the declined request.
func TestARejectDeclinedByTheApproverIsStoredWithItsReasonCodeAndNoGrant(t *testing.T) {
	w := newWorld(t)
	b := evaluatedWith(t, "evs_01ABC", "3.1")
	reason, err := decide.Core("DECLINED_BY_APPROVER")
	if err != nil {
		t.Fatal(err)
	}
	d, err := resolved(t).Reject(pass, reason)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = b.Attach(d); err != nil {
		t.Fatal(err)
	}
	declined := w.openRequest(t) // A597: a decline rests on the request it declined
	r := request(t, d, b)
	r.DeclinedRequestID = declined
	res, err := w.write(t, r, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Record.Outcome != "REJECT" || len(res.Record.ReasonCodes) != 1 || string(res.Record.ReasonCodes[0]) != "DECLINED_BY_APPROVER" || len(res.Grants) != 0 || res.ConfirmationRequestID != "" {
		t.Errorf("the record is %s %v with %d grants and request %q", res.Record.Outcome, res.Record.ReasonCodes, len(res.Grants), res.ConfirmationRequestID)
	}
	dm := w.eventsOf(t, "DECISION_MADE")
	if len(dm) != 1 || !jsonEqual(t, payloadOf(t, dm[0])["reason_codes"], []string{"DECLINED_BY_APPROVER"}) {
		t.Errorf("DECISION_MADE events %v", dm)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`) + w.spends(t) + len(w.eventsOf(t, "CONFIRMATION_REQUESTED", "GRANT_ISSUED")); n != 0 {
		t.Errorf("%d grants, spends or request events for a declined decision", n)
	}
	repeat := request(t, d, b)
	repeat.DeclinedRequestID = declined
	again, err := w.write(t, repeat, true)
	if err != nil || again.Record.DecisionId != res.Record.DecisionId {
		t.Errorf("the repeat: %v, decision %s (stored %s)", err, again.Record.DecisionId, res.Record.DecisionId)
	}
}

// REQ-EXE-008, REQ-OPS-002: the kill switch refuses a decision with steps. A CONFIRM has none, so it and its request are written as before while the switch is on.
func TestTheKillSwitchDoesNotRefuseAConfirmAndItsRequest(t *testing.T) {
	w := newWorld(t)
	w.setSwitch(t, tenant, true)
	res, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true)
	if err != nil {
		t.Fatalf("a CONFIRM with the kill switch on: %v", err)
	}
	if !cfrRe.MatchString(res.ConfirmationRequestID) || len(w.eventsOf(t, "CONFIRMATION_REQUESTED")) != 1 {
		t.Errorf("request %q, events %v", res.ConfirmationRequestID, w.eventsOf(t, "CONFIRMATION_REQUESTED"))
	}
}

// A186, INV-21, INV-03, A366: ONE decision exists for an input tuple (tenant, intent, intent hash, evidence snapshot, policy version) AND confirmation state, and the database says so
// (decision_record_input_state_key, migration 0034). After a stored CONFIRM, an ALLOW or a REJECT that rests on NO confirmation for the SAME tuple is ErrDecisionConflict and writes
// nothing; one that rests on a resolved or declined confirmation is a second decision (confirmation_state_test.go); the same decisions on a NEW snapshot are new decisions.
func TestAStoredConfirmAnswersItsTupleAndStateOnlyAndAnotherOutcomeOfNoConfirmationIsAConflict(t *testing.T) {
	w := newWorld(t)
	w.confirmed(t, given)
	first, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true)
	if err != nil {
		t.Fatal(err)
	}
	decisions := func() int { return w.count(t, `SELECT count(*) FROM decision_record`) }
	events := func() int { return w.count(t, `SELECT count(*) FROM record_event`) }
	d0, e0 := decisions(), events()

	allow := allowWith(t, "evs_01ABC", "")                // rests on no confirmation (A366: one that rests on a resolved confirmation is a second decision, confirmation_state_test.go)
	allow.IntentHash = contracts.CommonV1Hash(letters(0)) // the tuple of the stored CONFIRM
	if _, err := w.write(t, allow, true); !errors.Is(err, recorder.ErrDecisionConflict) {
		t.Errorf("an ALLOW that rests on no confirmation, on the tuple of a stored CONFIRM: err %v, want ErrDecisionConflict", err)
	}
	b := evaluatedWith(t, "evs_01ABC", "3.1")
	reason, rerr := decide.Core("NOT_AUTHORIZED") // a REJECT that rests on NO confirmation: not a decline (A597: a decline names its request, confirmation_state_test.go)
	if rerr != nil {
		t.Fatal(rerr)
	}
	rej, rerr := resolved(t).Reject(pass, reason)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if rej, rerr = b.Attach(rej); rerr != nil {
		t.Fatal(rerr)
	}
	same := request(t, rej, b)
	same.IntentHash = contracts.CommonV1Hash(letters(0))
	if _, err := w.write(t, same, true); !errors.Is(err, recorder.ErrDecisionConflict) {
		t.Errorf("a REJECT on the tuple of a stored CONFIRM: err %v, want ErrDecisionConflict", err)
	}
	if decisions() != d0 || events() != e0 {
		t.Errorf("a conflict wrote something: decisions %d (was %d), events %d (was %d)", decisions(), d0, events(), e0)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record WHERE outcome = 'CONFIRM'`); n != 1 {
		t.Errorf("%d CONFIRM decisions, want the one stored (%s)", n, first.Record.DecisionId)
	}
	// a new snapshot is another input: its REJECT is recorded as a new decision
	b2 := evaluatedWith(t, "evs_02XYZ", "3.1")
	rej2, rerr := resolved(t).Reject(pass, reason)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if rej2, rerr = b2.Attach(rej2); rerr != nil {
		t.Fatal(rerr)
	}
	fresh := request(t, rej2, b2)
	fresh.IntentHash = contracts.CommonV1Hash(letters(0))
	res, err := w.write(t, fresh, true)
	if err != nil || res.Record.Outcome != "REJECT" || res.Record.DecisionId == first.Record.DecisionId {
		t.Errorf("a REJECT on a new snapshot: %v, %v %s", err, res.Record.Outcome, res.Record.DecisionId)
	}
}

// The payload guard (A184) refuses a run of more than four digits in a payload string, but accepts a target reference at /target_ref (ruling A193, DEBT-30), so a request for the
// contract's own example target, fin.transaction:TX88213, is recorded. (The same string at another path is refused: the guard is not weakened.)
func TestAConfirmationRequestForATargetWithALongDigitRunIsRecordedAtTargetRef(t *testing.T) {
	w := newWorld(t)
	if _, err := w.write(t, confirmRequest(t, w, "evs_01ABC", true), true); err != nil {
		t.Fatal(err)
	}
	cr := w.eventsOf(t, "CONFIRMATION_REQUESTED")
	if len(cr) != 1 || payloadOf(t, cr[0])["target_ref"] != "fin.transaction:TX88213" {
		t.Fatalf("events %v", cr)
	}
	w2 := newWorld(t)
	r := confirmRequest(t, w2, "evs_01ABC", true)
	r.ConfirmationRequest.RequesterID = "usr_88213" // a body of digits only is no ID the guard exempts; it is not a target reference either
	if _, err := w2.write(t, r, true); !errors.Is(err, payloadguard.ErrDigitRun) {
		t.Errorf("a requester that is a run of digits: err %v, want the guard's ErrDigitRun", err)
	}
	w2.nothingWritten(t, "a refused payload")
}

// twoPolicies is a basis whose rules come from two policies, fin.pol@3.1 (the one the request names for the decision record) and fin.other@2.0.
func twoPolicies(t *testing.T, snapshot string) *decide.BasisBuilder {
	t.Helper()
	b, err := decide.NewBasisBuilder("fin.pack@1.0.0", epoch.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RecordEvidence(snapshot, []decide.Freshness{freshness()}); err != nil {
		t.Fatal(err)
	}
	if err := b.RecordPolicy([]decide.PolicyRuleVersion{{RuleID: "fin.pol.default.permit_case_writes", PolicyID: "fin.pol", Version: "3.1"},
		{RuleID: "fin.other.default.permit", PolicyID: "fin.other", Version: "2.0"}}); err != nil {
		t.Fatal(err)
	}
	return b
}

func confirmTwoPolicies(t *testing.T, w *world, policy string) recorder.Request {
	t.Helper()
	b := twoPolicies(t, "evs_01ABC")
	r := recorder.Request{TenantID: tenant, CorrelationID: cfrCorr, IntentID: "int_01J9X3", IntentHash: contracts.CommonV1Hash(letters(0)), PolicyID: "fin.pol", Decision: confirmDecision(t, b), Evaluated: b,
		ConfirmationRequest: newCfr(w)}
	r.ConfirmationRequest.PolicyVersion = policy
	return r
}

// A341 (interim), REQ-TRU-009, INV-25: the policy version a CONFIRMATION_REQUESTED binds is the composite of every policy version of the decision's basis, one policy or several. The
// event carries it and the binding hash is computed from it; the decision record still binds the one policy it names (fin.pol@3.1).
func TestAConfirmationRequestBindsTheCompositeOfEveryPolicyVersionOfTheBasis(t *testing.T) {
	for name, tc := range map[string]struct {
		build func(t *testing.T, w *world) recorder.Request
		want  string
	}{
		"one policy":   {func(t *testing.T, w *world) recorder.Request { return confirmRequest(t, w, "evs_01ABC", true) }, policyVersion},
		"two policies": {func(t *testing.T, w *world) recorder.Request { return confirmTwoPolicies(t, w, policyVersionTwo) }, policyVersionTwo},
	} {
		w := newWorld(t)
		res, err := w.write(t, tc.build(t, w), true)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		cr := w.eventsOf(t, "CONFIRMATION_REQUESTED")
		if len(cr) != 1 {
			t.Fatalf("%s: %d CONFIRMATION_REQUESTED events", name, len(cr))
		}
		p := payloadOf(t, cr[0])
		binding := bindingHashOf(t, confirm.BindingInput{PrincipalID: cfrPrincipal, IntentHash: letters(0), CapabilityID: capability, TargetRef: cfrTarget, ParamsHash: letters(1),
			EvidenceSetHash: letters(2), PlanHash: letters(3), PolicyVersion: tc.want})
		if p["policy_version"] != tc.want || p["binding_hash"] != binding {
			t.Errorf("%s: the event carries policy_version %v and binding %v, want %s and %s", name, p["policy_version"], p["binding_hash"], tc.want, binding)
		}
		if _, has := p["policy_set_hash"]; has {
			t.Errorf("%s: the event carries the interim policy_set_hash member: %v", name, p["policy_set_hash"])
		}
		if res.Record.PolicyVersion != "fin.pol@3.1" {
			t.Errorf("%s: the decision record binds %q, want the one policy it names, fin.pol@3.1", name, res.Record.PolicyVersion)
		}
	}
}

// A341 (interim): a request that binds anything but the composite of the basis is refused before anything is written: the old single-policy form "id@version" (also when it is the only
// policy), the composite of only one of two policies, the composite of another set, and a composite in upper case.
func TestAConfirmationRequestThatDoesNotBindTheCompositeIsRefused(t *testing.T) {
	for name, build := range map[string]func(t *testing.T, w *world) recorder.Request{
		"the single-policy form, with one policy": func(t *testing.T, w *world) recorder.Request {
			r := confirmRequest(t, w, "evs_01ABC", true)
			r.ConfirmationRequest.PolicyVersion = "fin.pol@3.1"
			return r
		},
		"the composite of one of two policies": func(t *testing.T, w *world) recorder.Request { return confirmTwoPolicies(t, w, policyVersion) },
		"the single-policy form, with two policies": func(t *testing.T, w *world) recorder.Request {
			return confirmTwoPolicies(t, w, "fin.pol@3.1")
		},
		"the composite of another set": func(t *testing.T, w *world) recorder.Request {
			r := confirmRequest(t, w, "evs_01ABC", true)
			r.ConfirmationRequest.PolicyVersion = policyVersionTwo
			return r
		},
		"a composite in upper case": func(t *testing.T, w *world) recorder.Request {
			r := confirmRequest(t, w, "evs_01ABC", true)
			r.ConfirmationRequest.PolicyVersion = strings.ToUpper(policyVersion)
			return r
		},
	} {
		w := newWorld(t)
		if _, err := w.write(t, build(t, w), true); !errors.Is(err, recorder.ErrRequest) {
			t.Errorf("%s: err %v, want ErrRequest", name, err)
		}
		w.nothingWritten(t, name)
	}
}

// A341 (interim), INV-25, INV-21: the composite is the RFC 8785 form of the sorted, distinct "policy_id@version" strings, hashed. The expected value here is computed with the
// repository's own canonicaliser (platform/crypto.CanonicalJSON), so the function in trust/decide and the canonicaliser agree byte for byte.
func TestTheCompositeIsTheHashOfTheCanonicalFormOfTheSortedDistinctPolicyVersions(t *testing.T) {
	list := []string{"fin.pol@3.1", "fin.other@2.0", "fin.pol@3.1"}
	distinct := []string{"fin.other@2.0", "fin.pol@3.1"}
	canonical, err := crypto.CanonicalJSON(distinct)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	want := "sha256:" + hex.EncodeToString(sum[:])
	var refs []decide.PolicyVersionRef
	for _, s := range list {
		id, version, _ := strings.Cut(s, "@")
		refs = append(refs, decide.PolicyVersionRef{PolicyID: id, Version: version})
	}
	got, err := decide.CompositePolicyVersion(refs)
	if err != nil || got != want || got != policyVersionTwo {
		t.Errorf("composite %q (%v), canonical form %s hashes to %q, pinned %q", got, err, canonical, want, policyVersionTwo)
	}
}

// A367, A184 (payload guard, not changed): the composite is "sha256:" and 64 hex, a typed value the guard exempts whole, so it rides in the policy_version member although a SHA-256 hex
// holds a run of more than four digits with a probability near one. The pinned composites of these tests hold such a run (six digits), which is what makes the tests above prove it.
func TestThePinnedCompositesHoldALongDigitRunSoTheTestsProveTheGuardExemptsTheCompositeForm(t *testing.T) {
	long := regexp.MustCompile(`[0-9]{5,}`)
	for name, composite := range map[string]string{"one policy": policyVersion, "two policies": policyVersionTwo} {
		if !long.MatchString(composite) {
			t.Errorf("%s: %s holds no run of five digits: the test no longer shows that the guard exempts the form", name, composite)
		}
	}
}
