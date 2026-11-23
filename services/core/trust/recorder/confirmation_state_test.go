package recorder_test

// TRU-07 (A366, A186; migration 0034; REQ-TRU-007, REQ-TRU-008, REQ-TRU-009, INV-21, INV-25, REQ-SEC-004): the single-decision key of an input tuple includes the CONFIRMATION STATE.
// A decision rests on no confirmation (state NULL: a CONFIRM, or an ALLOW or REJECT that needed none), on an ACCEPTED confirmation (an ALLOW that spends it: the confirmation request
// the spent event resolves) or on a DECLINED one (a REJECT DECLINED_BY_APPROVER: the declined request). The key is the tuple and the state, so a stored CONFIRM (NULL) and the ALLOW
// that follows its ACCEPT (the request) on the SAME snapshot are two decisions, while a second decision of the same state is still one (a repeated Record returns the stored one).
// Real PostgreSQL, real tenant logins, the real chain. All data is SYNTHETIC (AGENT-25).

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// The tuple of these tests has the intent hash letters(0): the payload guard refuses a content hash of digits only (the hash h(1) of the other tests), and a CONFIRM's request event
// carries the intent hash.

// confirmOnTheTupleOfAllowWith is a CONFIRM, with its request, on the very tuple allowOnTheTuple(t, snapshot, _) decides: the same intent, intent hash, snapshot and policy version.
func confirmOnTheTupleOfAllowWith(t *testing.T, w *world, snapshot string) recorder.Request {
	t.Helper()
	return confirmRequest(t, w, snapshot, true)
}

// allowOnTheTuple is allowWith with the intent hash of the CONFIRM's tuple.
func allowOnTheTuple(t *testing.T, snapshot, event string) recorder.Request {
	t.Helper()
	r := allowWith(t, snapshot, event)
	r.IntentHash, r.DispositionDecisionID = contracts.CommonV1Hash(letters(0)), seededDispositionL // the intent of the tuple, and its own disposition (A542)
	return r
}

// confirmedOnTheTuple records an accepted confirmation (event cfe_x, request cfr_x) given for the binding of the ALLOW of allowOnTheTuple.
func (w *world) confirmedOnTheTuple(t *testing.T, event string) {
	t.Helper()
	b := validBinding()
	b.IntentHash = letters(0)
	if err := recordConfirmation(t, w.trust, tenant, event, b, realNow().Add(time.Hour)); err != nil {
		t.Fatalf("recording the confirmation %s: %v", event, err)
	}
}

// declinedReject is a REJECT DECLINED_BY_APPROVER on the tuple of allowOnTheTuple(t, snapshot, _) that rests on the declined request.
func declinedReject(t *testing.T, snapshot, requestID string) recorder.Request {
	t.Helper()
	b := evaluatedWith(t, snapshot, "3.1")
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
	r := request(t, d, b)
	r.IntentHash = contracts.CommonV1Hash(letters(0))
	r.DeclinedRequestID = requestID
	return r
}

func (w *world) stateOf(t *testing.T, decisionID string) (state string, null bool) {
	t.Helper()
	var s *string
	if err := w.admin.QueryRow(ctx(t), `SELECT confirmation_request_id FROM decision_record WHERE decision_id = $1`, decisionID).Scan(&s); err != nil {
		t.Fatalf("read the confirmation state of %s: %v", decisionID, err)
	}
	if s == nil {
		return "", true
	}
	return *s, false
}

// A366, A186, INV-21: a stored CONFIRM and the ALLOW that follows its ACCEPT on the SAME snapshot are two decisions. The CONFIRM rests on no confirmation (NULL); the ALLOW rests on the
// request its spent confirmation resolves; the ALLOW has its grant and its DECISION_MADE names the request. A second ALLOW of the same state returns the stored one: no second decision,
// no second grant, no second spend, no second event.
func TestAConfirmAndTheAllowThatFollowsItsAcceptOnOneTupleAreTwoDecisionsAndARepeatIsOne(t *testing.T) {
	w := newWorld(t)
	w.confirmedOnTheTuple(t, given)
	confirm, err := w.write(t, confirmOnTheTupleOfAllowWith(t, w, "evs_01ABC"), true)
	if err != nil || confirm.Record.Outcome != "CONFIRM" {
		t.Fatalf("the CONFIRM: %v, %+v", err, confirm.Record.Outcome)
	}
	allow, err := w.write(t, allowOnTheTuple(t, "evs_01ABC", given), true)
	if err != nil {
		t.Fatalf("the ALLOW that follows the ACCEPT on the tuple of the stored CONFIRM: %v", err)
	}
	if allow.Record.Outcome != "ALLOW" || allow.Record.DecisionId == confirm.Record.DecisionId || len(allow.Grants) != 1 {
		t.Fatalf("the ALLOW: %s %s with %d grants; the CONFIRM is %s", allow.Record.Outcome, allow.Record.DecisionId, len(allow.Grants), confirm.Record.DecisionId)
	}
	if n := w.count(t, recordsQ); n != 2 {
		t.Fatalf("%d decision records, want the CONFIRM and the ALLOW", n)
	}
	if s, null := w.stateOf(t, string(confirm.Record.DecisionId)); !null {
		t.Errorf("the CONFIRM rests on confirmation %q, want none (NULL)", s)
	}
	if s, null := w.stateOf(t, string(allow.Record.DecisionId)); null || s != "cfr_"+strings.TrimPrefix(given, "cfe_") {
		t.Errorf("the ALLOW rests on %q (null %v), want the request its confirmation resolved, cfr_%s", s, null, strings.TrimPrefix(given, "cfe_"))
	}
	payload := payloadOf(t, w.eventsOf(t, "DECISION_MADE")[1])
	if payload["confirmation_request_id"] != "cfr_"+strings.TrimPrefix(given, "cfe_") {
		t.Errorf("the ALLOW's DECISION_MADE names request %v, want the one it rests on", payload["confirmation_request_id"])
	}
	events, decisions, grants, spends := len(w.events(t)), w.count(t, recordsQ), w.count(t, `SELECT count(*) FROM decision_grant`), w.spends(t)
	again, err := w.write(t, allowOnTheTuple(t, "evs_01ABC", given), true)
	if err != nil || again.Record.DecisionId != allow.Record.DecisionId || len(again.Grants) != 1 || asJSON(t, again.Grants[0]) != asJSON(t, allow.Grants[0]) {
		t.Fatalf("the same ALLOW again: %v, %s, want the stored %s", err, again.Record.DecisionId, allow.Record.DecisionId)
	}
	if len(w.events(t)) != events || w.count(t, recordsQ) != decisions || w.count(t, `SELECT count(*) FROM decision_grant`) != grants || w.spends(t) != spends {
		t.Error("a repeated ALLOW wrote something: a decision, a grant, a spend or an event")
	}
	// and the CONFIRM asked for again is still the stored one (its state is NULL), not a third decision
	if again, err := w.write(t, confirmOnTheTupleOfAllowWith(t, w, "evs_01ABC"), true); err != nil || again.Record.DecisionId != confirm.Record.DecisionId {
		t.Errorf("the CONFIRM again: %v, %s, want the stored %s", err, again.Record.DecisionId, confirm.Record.DecisionId)
	}
}

// A366, A233, INV-21: a SECOND confirmation on the tuple that already has a decision resting on one is a typed conflict and writes nothing: a tuple has one decision of an outcome, so two
// accepted confirmations of one binding cannot give two grants on one input. The first ALLOW is untouched.
func TestADifferentConfirmationOnATupleThatHasAnAllowOnAnotherIsAConflict(t *testing.T) {
	w := newWorld(t)
	w.confirmedOnTheTuple(t, given)
	w.confirmedOnTheTuple(t, "cfe_01OTHER2")
	first, err := w.write(t, allowOnTheTuple(t, "evs_01ABC", given), true)
	if err != nil {
		t.Fatal(err)
	}
	events, decisions, spends := len(w.events(t)), w.count(t, recordsQ), w.spends(t)
	if _, err := w.write(t, allowOnTheTuple(t, "evs_01ABC", "cfe_01OTHER2"), true); !errors.Is(err, recorder.ErrDecisionConflict) {
		t.Errorf("an ALLOW on another confirmation, same tuple: err %v, want ErrDecisionConflict", err)
	}
	if len(w.events(t)) != events || w.count(t, recordsQ) != decisions || w.spends(t) != spends {
		t.Error("the conflict wrote something")
	}
	if s, _ := w.stateOf(t, string(first.Record.DecisionId)); s != "cfr_01SPENT" {
		t.Errorf("the first ALLOW rests on %q", s)
	}
}

// A366: the state is part of the key, so a decision that rests on NO confirmation is still the one decision of the tuple a stored CONFIRM holds: an ALLOW or a REJECT with no resolved
// confirmation on the tuple of a stored CONFIRM is ErrDecisionConflict (the rule of A186 for the same state), and a new snapshot is another input.
func TestADecisionThatRestsOnNoConfirmationOnTheTupleOfAStoredConfirmIsStillAConflict(t *testing.T) {
	w := newWorld(t)
	if _, err := w.write(t, confirmOnTheTupleOfAllowWith(t, w, "evs_01ABC"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := w.write(t, allowOnTheTuple(t, "evs_01ABC", ""), true); !errors.Is(err, recorder.ErrDecisionConflict) {
		t.Errorf("an ALLOW with no confirmation on the tuple of a stored CONFIRM: err %v, want ErrDecisionConflict", err)
	}
	b := evaluatedWith(t, "evs_01ABC", "3.1")
	rej := request(t, decision(t, decide.Reject, b), b)
	rej.IntentHash = contracts.CommonV1Hash(letters(0))
	if _, err := w.write(t, rej, true); !errors.Is(err, recorder.ErrDecisionConflict) {
		t.Errorf("a REJECT with no confirmation on the tuple of a stored CONFIRM: err %v, want ErrDecisionConflict", err)
	}
	if n := w.count(t, recordsQ); n != 1 {
		t.Errorf("%d decisions, want the CONFIRM only", n)
	}
}

// A366, A341 (the DECLINED_BY_APPROVER mapping): a REJECT that rests on the declined request is a second decision on the tuple of the stored CONFIRM, with its own state; asked again it
// is the stored one; one that names another declined request is a typed conflict; and the request ID is checked: malformed, or on any outcome but REJECT, is ErrRequest.
func TestADeclinedRejectOnTheTupleOfAStoredConfirmIsASecondDecisionAndARepeatIsOne(t *testing.T) {
	w := newWorld(t)
	if _, err := w.write(t, confirmOnTheTupleOfAllowWith(t, w, "evs_01ABC"), true); err != nil {
		t.Fatal(err)
	}
	w.seedRequest(t, w.trust, tenant, "cfr_01DECLINED", "int_01J9X3") // the request the decline rests on is a request of the tenant and the intent (A507)
	w.seedRequest(t, w.trust, tenant, "cfr_01ANOTHER", "int_01J9X3")
	first, err := w.write(t, declinedReject(t, "evs_01ABC", "cfr_01DECLINED"), true)
	if err != nil || first.Record.Outcome != "REJECT" {
		t.Fatalf("the declined REJECT: %v, %s", err, first.Record.Outcome)
	}
	if s, null := w.stateOf(t, string(first.Record.DecisionId)); null || s != "cfr_01DECLINED" {
		t.Errorf("the REJECT rests on %q (null %v)", s, null)
	}
	decisions, events := w.count(t, recordsQ), len(w.events(t))
	if again, err := w.write(t, declinedReject(t, "evs_01ABC", "cfr_01DECLINED"), true); err != nil || again.Record.DecisionId != first.Record.DecisionId {
		t.Errorf("the same declined REJECT again: %v, %s, want %s", err, again.Record.DecisionId, first.Record.DecisionId)
	}
	if _, err := w.write(t, declinedReject(t, "evs_01ABC", "cfr_01ANOTHER"), true); !errors.Is(err, recorder.ErrDecisionConflict) {
		t.Errorf("a REJECT resting on another declined request: err %v, want ErrDecisionConflict", err)
	}
	if w.count(t, recordsQ) != decisions || len(w.events(t)) != events || decisions != 2 {
		t.Errorf("a repeat or a conflict wrote something (%d decisions)", decisions)
	}
	for name, r := range map[string]recorder.Request{
		"a malformed request ID": declinedReject(t, "evs_02XYZ", "req_01"),
		"an upper-case prefix":   declinedReject(t, "evs_02XYZ", "CFR_01"),
		"an ALLOW that names one": func() recorder.Request {
			a := allowOnTheTuple(t, "evs_02XYZ", "")
			a.DeclinedRequestID = "cfr_01DECLINED"
			return a
		}(),
	} {
		if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrRequest) {
			t.Errorf("%s: err %v, want ErrRequest", name, err)
		}
	}
}

// A366, REQ-SEC-004, INV-11: the tenant is in the key. Another tenant records the same tuple, the same state and the same confirmation request ID as decisions of its own; nothing of
// tenant A answers for tenant B, and B's second decision of one state is one decision of B's.
func TestTheConfirmationStateKeyIsPerTenant(t *testing.T) {
	w := newWorld(t)
	w.confirmedOnTheTuple(t, given)
	if _, err := w.write(t, confirmOnTheTupleOfAllowWith(t, w, "evs_01ABC"), true); err != nil {
		t.Fatal(err)
	}
	allowA, err := w.write(t, allowOnTheTuple(t, "evs_01ABC", given), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	bb := newTenantTrust(t, w, "ten_bb")
	bound := validBinding()
	bound.IntentHash = letters(0)
	if err := recordConfirmation(t, bb, "ten_bb", given, bound, realNow().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	asB := func(r recorder.Request) recorder.Request { r.TenantID = "ten_bb"; return r }
	confirmB, err := w.writeVia(t, bb, asB(confirmOnTheTupleOfAllowWith(t, w, "evs_01ABC")))
	if err != nil || confirmB.Record.Outcome != "CONFIRM" {
		t.Fatalf("tenant B's CONFIRM on the tuple tenant A has decided: %v", err)
	}
	bEvaluated := evaluatedWith(t, "evs_01DSP", "3.1")                                      // its own sealed snapshot: another input than the ALLOW of the same intent (A186, A542)
	bDispositionRequest := request(t, decision(t, decide.Escalate, bEvaluated), bEvaluated) // tenant B's own ESCALATE decision, written through B's login: the one its ALLOW names
	bDispositionRequest.TenantID, bDispositionRequest.IntentHash = "ten_bb", contracts.CommonV1Hash(letters(0))
	bDispositionResult, err := w.writeVia(t, bb, bDispositionRequest)
	if err != nil {
		t.Fatalf("tenant B's disposition: %v", err)
	}
	bDisposition := string(bDispositionResult.Record.DecisionId)
	allowAsB := func(r recorder.Request) recorder.Request {
		r = asB(r)
		r.DispositionDecisionID = bDisposition
		return r
	}
	allowB, err := w.writeVia(t, bb, allowAsB(allowOnTheTuple(t, "evs_01ABC", given)))
	if err != nil || allowB.Record.DecisionId == allowA.Record.DecisionId || allowB.Record.Outcome != "ALLOW" {
		t.Fatalf("tenant B's ALLOW resting on the same request ID: %v, %s (tenant A's is %s)", err, allowB.Record.DecisionId, allowA.Record.DecisionId)
	}
	if again, err := w.writeVia(t, bb, allowAsB(allowOnTheTuple(t, "evs_01ABC", given))); err != nil || again.Record.DecisionId != allowB.Record.DecisionId {
		t.Errorf("tenant B's ALLOW again: %v, %s, want B's own %s", err, again.Record.DecisionId, allowB.Record.DecisionId)
	}
	if n := w.countFor(t, `SELECT count(*) FROM decision_record WHERE tenant_id = $1 AND outcome <> 'ESCALATE'`, "ten_bb"); n != 2 {
		t.Errorf("%d decisions for tenant B, want 2", n)
	}
	if n := w.countFor(t, `SELECT count(*) FROM decision_record WHERE tenant_id = $1 AND outcome <> 'ESCALATE'`, tenant); n != 2 {
		t.Errorf("%d decisions for tenant A, want 2", n)
	}
}

// A366, A145 (migration 0034): the schema says it. decision_record has a nullable confirmation_request_id with a check on its form, the unique key is over the old columns and
// COALESCE of it (named, tenant first), and the old key is gone; the immutability triggers of the table are still there.
func TestMigration0034TheDecisionKeyIncludesTheConfirmationState(t *testing.T) {
	w := newWorld(t)
	var nullable, check string
	if err := w.admin.QueryRow(ctx(t), `SELECT is_nullable FROM information_schema.columns WHERE table_name = 'decision_record' AND column_name = 'confirmation_request_id'`).Scan(&nullable); err != nil || nullable != "YES" {
		t.Fatalf("the column confirmation_request_id: nullable %q, %v; want a nullable column", nullable, err)
	}
	if err := w.admin.QueryRow(ctx(t), `SELECT string_agg(pg_get_constraintdef(oid), ' ') FROM pg_constraint WHERE conrelid = 'decision_record'::regclass AND contype = 'c' AND pg_get_constraintdef(oid) LIKE '%confirmation_request_id%'`).Scan(&check); err != nil || !strings.Contains(check, "cfr_") {
		t.Errorf("no check on the form of confirmation_request_id: %q, %v", check, err)
	}
	var def string
	if err := w.admin.QueryRow(ctx(t), `SELECT indexdef FROM pg_indexes WHERE tablename = 'decision_record' AND indexname = 'decision_record_input_state_key'`).Scan(&def); err != nil {
		t.Fatalf("the index decision_record_input_state_key: %v", err)
	}
	if !strings.Contains(def, "UNIQUE") || !strings.Contains(def, "COALESCE(confirmation_request_id, ''::text)") || !strings.HasPrefix(strings.SplitN(def, "(", 2)[1], "tenant_id,") {
		t.Errorf("the key is not unique over the tuple and COALESCE of the state, tenant first: %s", def)
	}
	if n := w.count(t, `SELECT count(*) FROM pg_indexes WHERE tablename = 'decision_record' AND indexname = 'decision_record_input_key'`); n != 0 {
		t.Error("the old key decision_record_input_key (state-blind) is still there")
	}
	if n := w.count(t, `SELECT count(*) FROM pg_trigger WHERE tgrelid = 'decision_record'::regclass AND tgname IN ('decision_record_immutable', 'decision_record_no_truncate', 'decision_record_tenant_guard')`); n != 3 {
		t.Errorf("%d of the 3 decision_record triggers remain", n)
	}
}
