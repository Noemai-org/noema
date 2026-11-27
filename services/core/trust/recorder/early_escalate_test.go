package recorder_test

// TRU-10 (A663, A581, A590; interim, relayed; A574, A577, A578; REQ-TRU-001, REQ-TRU-007, REQ-TRU-010; INV-03, INV-17, INV-21, INV-25): an ESCALATE reached before the evidence and the policy
// were evaluated has an incomplete basis. The Writer records it in the null-basis form decision-record.v2 allows (the three basis members are required keys holding null), with no step and no
// grant, keyed by the turn (the client's request ID) and the verified principal as well as the intent hash. A replay of the same turn returns the stored record; a new turn records a new one;
// a stored decision the new request contradicts is a typed conflict. ALLOW and CONFIRM still need a complete basis, and so does every other outcome. Real PostgreSQL. A refused request writes
// nothing, so these tests COMMIT after a refusal: a leftover row or event would survive and be counted. All data is SYNTHETIC.

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const (
	turnA      = "0b8f3c1e-5d2a-4e7b-9c41-6a1d2f3e4b50"
	turnB      = "7c2d9a40-1e3b-4f6a-8d57-2b9c0e1f3a64"
	principalA = "usr_88213"
	principalB = "usr_88214"
)

// earlyEscalate is an ESCALATE of an intent no step has read evidence or policy for: its basis holds the pack version and the evaluation time only.
func earlyEscalate(t *testing.T, reasonCode, packVersion string) (decide.Decision, *decide.BasisBuilder) {
	t.Helper()
	b, err := decide.NewBasisBuilder(packVersion, epoch.Unix())
	if err != nil {
		t.Fatal(err)
	}
	reason, err := decide.Core(reasonCode)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := decide.Admit(decide.IntentView{Version: 2, MaterialFlags: []string{"X"}}).Unresolved()
	if !ok {
		t.Fatal("the intent is resolved")
	}
	d, err := u.Escalate(reason)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = b.Attach(d); err != nil {
		t.Fatal(err)
	}
	return d, b
}

func earlyRequest(t *testing.T, turn, principal string) recorder.Request {
	t.Helper()
	d, b := earlyEscalate(t, "NOT_AUTHORIZED", "fin.pack@1.0.0")
	r := request(t, d, b)
	r.TurnID, r.TurnPrincipalID = turn, principal
	return r
}

func (w *world) earlyRows(t *testing.T) int {
	return w.count(t, `SELECT count(*) FROM decision_record WHERE outcome = 'ESCALATE' AND jsonb_typeof(json -> 'evidence_snapshot_id') = 'null'
		AND jsonb_typeof(json -> 'policy_version') = 'null' AND jsonb_typeof(json -> 'evidence_freshness') = 'null'`)
}

// The early ESCALATE is recorded: the three basis keys are present and null, there is no grant, and the decision and its event are written.
func TestAnEarlyEscalateIsRecordedWithTheBasisNullAndNoGrant(t *testing.T) { // A663, A574, A577, INV-03, INV-17
	w := newWorld(t)
	res, err := w.write(t, earlyRequest(t, turnA, principalA), true)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	rec := res.Record
	if rec.Outcome != contracts.EnumsV1DecisionOutcomeESCALATE || rec.EvidenceSnapshotId != nil || rec.PolicyVersion != nil || rec.EvidenceFreshness != nil || len(res.Grants) != 0 {
		t.Fatalf("record %+v grants %d: want an ESCALATE with the three basis members null and no grant", rec, len(res.Grants))
	}
	if n := w.earlyRows(t); n != 1 {
		t.Errorf("%d stored rows with a null basis, want 1", n)
	}
	if n := w.count(t, fmt.Sprintf(`SELECT count(*) FROM decision_record WHERE turn_id = '%s' AND principal_id = '%s'`, turnA, principalA)); n != 1 {
		t.Errorf("%d rows name the turn and the principal, want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`); n != 0 {
		t.Errorf("%d grants, want none", n)
	}
	if got := w.events(t); len(got) != 1 || !strings.HasPrefix(got[0], "DECISION_MADE:") {
		t.Errorf("events %v, want one DECISION_MADE", got)
	}
	// INV-12: the turn's principal is a key member, never the Record actor.
	rows, err := chain.ReadChain(ctx(t), w.reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range rows {
		if e.Actor != "core-trust" || e.WrittenBy != "core-trust" {
			t.Errorf("%s: actor %q written_by %q, want both core-trust", e.EventType, e.Actor, e.WrittenBy)
		}
	}
}

// A replay of the same turn, principal and tenant returns the stored record and writes nothing (A581, A590, INV-21).
func TestAReplayOfTheSameTurnReturnsTheStoredEarlyDecision(t *testing.T) { // A581, A590, INV-21
	w := newWorld(t)
	first, err := w.write(t, earlyRequest(t, turnA, principalA), true)
	if err != nil {
		t.Fatal(err)
	}
	again, err := w.write(t, earlyRequest(t, turnA, principalA), true)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if again.Record.DecisionId != first.Record.DecisionId {
		t.Errorf("the replay returned %s, want the stored %s", again.Record.DecisionId, first.Record.DecisionId)
	}
	if n := w.earlyRows(t); n != 1 {
		t.Errorf("%d rows after a replay, want 1", n)
	}
	if got := w.events(t); len(got) != 1 {
		t.Errorf("events %v, want the one of the first write", got)
	}
}

// A new turn evaluates fresh and records a new decision; so does another principal on the same turn (A581, A590). Nobody reads another's.
func TestANewTurnOrAnotherPrincipalRecordsANewEarlyDecision(t *testing.T) { // A581, A590
	w := newWorld(t)
	a, err := w.write(t, earlyRequest(t, turnA, principalA), true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := w.write(t, earlyRequest(t, turnB, principalA), true)
	if err != nil {
		t.Fatalf("a new turn: %v", err)
	}
	c, err := w.write(t, earlyRequest(t, turnA, principalB), true)
	if err != nil {
		t.Fatalf("another principal: %v", err)
	}
	ids := map[contracts.CommonV1DecisionId]bool{a.Record.DecisionId: true, b.Record.DecisionId: true, c.Record.DecisionId: true}
	if len(ids) != 3 || w.earlyRows(t) != 3 {
		t.Errorf("%d distinct decisions and %d rows, want 3 and 3", len(ids), w.earlyRows(t))
	}
}

// A stored decision of this turn that the new request contradicts is not returned and not duplicated: a typed conflict (A581: the key holds the pack version, the outcome and the reason).
func TestARequestOfTheSameTurnThatContradictsTheStoredDecisionIsAConflict(t *testing.T) { // A581, INV-21
	for name, mutate := range map[string]func(t *testing.T) recorder.Request{
		"another reason": func(t *testing.T) recorder.Request {
			d, b := earlyEscalate(t, "NO_RUNNABLE_CAPABILITY", "fin.pack@1.0.0")
			r := request(t, d, b)
			r.TurnID, r.TurnPrincipalID = turnA, principalA
			return r
		},
		"another pack version": func(t *testing.T) recorder.Request {
			d, b := earlyEscalate(t, "NOT_AUTHORIZED", "fin.pack@1.0.1")
			r := request(t, d, b)
			r.TurnID, r.TurnPrincipalID = turnA, principalA
			return r
		},
	} {
		w := newWorld(t)
		if _, err := w.write(t, earlyRequest(t, turnA, principalA), true); err != nil {
			t.Fatal(err)
		}
		_, err := w.write(t, mutate(t), true)
		if !errors.Is(err, recorder.ErrDecisionConflict) {
			t.Errorf("%s: err %v, want ErrDecisionConflict", name, err)
		}
		if n := w.earlyRows(t); n != 1 {
			t.Errorf("%s: %d rows, want 1", name, n)
		}
	}
}

// What the early form refuses, before anything is written (fail closed).
func TestAnEarlyEscalateTheKeyDoesNotCoverIsRefusedAndWritesNothing(t *testing.T) { // A663, A581, A590, INV-17
	cases := map[string]func(r *recorder.Request){
		"no turn":                  func(r *recorder.Request) { r.TurnID = "" },
		"an upper-case turn":       func(r *recorder.Request) { r.TurnID = strings.ToUpper(turnA) },
		"a turn with a newline":    func(r *recorder.Request) { r.TurnID += "\n" },
		"a short turn":             func(r *recorder.Request) { r.TurnID = turnA[:30] },
		"no principal":             func(r *recorder.Request) { r.TurnPrincipalID = "" },
		"a malformed principal":    func(r *recorder.Request) { r.TurnPrincipalID = "88213" },
		"a principal with newline": func(r *recorder.Request) { r.TurnPrincipalID += "\n" },
		"a step":                   func(r *recorder.Request) { r.Steps = append(r.Steps, step(t)) },
		"a confirmation request": func(r *recorder.Request) {
			r.ConfirmationRequest = &recorder.NewConfirmationRequest{PrincipalID: principalA}
		},
		"a declined request": func(r *recorder.Request) { r.DeclinedRequestID = "cfr_synth0001" },
	}
	for name, mutate := range cases {
		w := newWorld(t)
		r := earlyRequest(t, turnA, principalA)
		mutate(&r)
		if _, err := w.write(t, r, true); err == nil {
			t.Errorf("%s: no error", name)
		}
		w.nothingRemains(t, name)
	}
}

// Only an ESCALATE takes the early form, and a decision with a complete basis names no turn and no principal: both are ErrRequest. ALLOW, CONFIRM and every other outcome with an incomplete
// basis keep the refusal they had (INV-17, INV-03).
func TestOnlyAnEscalateTakesTheEarlyFormAndACompleteBasisNamesNoTurn(t *testing.T) { // A663, INV-17, INV-03
	for _, o := range []decide.Outcome{decide.Allow, decide.Wait} { // a REJECT with an incomplete basis is the early form of A680, tested in early_reject_test.go
		w := newWorld(t)
		b, err := decide.NewBasisBuilder("fin.pack@1.0.0", epoch.Unix())
		if err != nil {
			t.Fatal(err)
		}
		var d decide.Decision
		reason, _ := decide.Core("NOT_AUTHORIZED")
		switch o {
		case decide.Allow:
			d, err = resolved(t).Allow(pass)
		case decide.Wait:
			d, err = resolved(t).Wait(pass, epoch.Unix()+60, reason)
		}
		if err != nil {
			t.Fatal(err)
		}
		if o != decide.Allow {
			if d, err = b.Attach(d); err != nil {
				t.Fatal(err)
			}
		}
		r := request(t, d, b)
		r.TurnID, r.TurnPrincipalID = turnA, principalA
		if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrNoEvaluatedBasis) {
			t.Errorf("%s with an incomplete basis and a turn: err %v, want ErrNoEvaluatedBasis", o, err)
		}
		w.nothingRemains(t, string(o))
	}
	w := newWorld(t)
	b := evaluated(t)
	r := request(t, decision(t, decide.Escalate, b), b)
	r.TurnID, r.TurnPrincipalID = turnA, principalA
	if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrRequest) {
		t.Errorf("an ESCALATE with a complete basis that names a turn: err %v, want ErrRequest", err)
	}
	w.nothingRemains(t, "a complete basis naming a turn")
	// A680: a REJECT with a complete basis (a policy refusal, T11) names no turn either.
	w = newWorld(t)
	rej, err := resolved(t).Reject(decide.Authority{Technical: decide.Pass, Policy: decide.Fail, Business: decide.NotEvaluated}, mustCore(t, "NOT_AUTHORIZED"))
	if err != nil {
		t.Fatal(err)
	}
	rb := evaluated(t)
	if rej, err = rb.Attach(rej); err != nil {
		t.Fatal(err)
	}
	r = request(t, rej, rb)
	r.TurnID, r.TurnPrincipalID = turnA, principalA
	if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrRequest) {
		t.Errorf("a REJECT with a complete basis that names a turn: err %v, want ErrRequest", err)
	}
	w.nothingRemains(t, "a complete-basis REJECT naming a turn")
	r.TurnID, r.TurnPrincipalID = "", ""
	if res, err := w.write(t, r, true); err != nil || res.Record.EvidenceSnapshotId == nil {
		t.Errorf("an ESCALATE with a complete basis and no turn: %v, snapshot %v; want it recorded as before", err, res.Record.EvidenceSnapshotId)
	}
}

// Property: the early decisions stored for an intent are exactly the distinct (turn, principal) pairs written for it; a pair written twice has one decision (A581, A590, INV-21).
func TestTheEarlyKeyIsTheTurnAndThePrincipalProperty(t *testing.T) { // A581, A590, INV-21
	turns := []string{turnA, turnB, "5e1a7b22-9c3d-4a08-b6f1-0d4e8a2c7f19"}
	principals := []string{principalA, principalB}
	w := newWorld(t)
	var n atomic.Int64
	rapid.Check(t, func(rt *rapid.T) {
		intent := fmt.Sprintf("int_P%d", n.Add(1))
		seen := map[string]contracts.CommonV1DecisionId{}
		for i := 0; i < rapid.IntRange(1, 7).Draw(rt, "writes"); i++ {
			turn, principal := rapid.SampledFrom(turns).Draw(rt, "turn"), rapid.SampledFrom(principals).Draw(rt, "principal")
			r := earlyRequest(t, turn, principal)
			r.IntentID = intent
			res, err := w.write(t, r, true)
			if err != nil {
				rt.Fatalf("write %s %s: %v", turn, principal, err)
			}
			k := turn + "|" + principal
			if prev, ok := seen[k]; ok && prev != res.Record.DecisionId {
				rt.Fatalf("%s: a replay returned %s, want %s", k, res.Record.DecisionId, prev)
			}
			seen[k] = res.Record.DecisionId
		}
		if got := w.count(t, fmt.Sprintf(`SELECT count(*) FROM decision_record WHERE json ->> 'intent_id' = '%s'`, intent)); got != len(seen) {
			rt.Fatalf("%d stored rows for %d distinct pairs", got, len(seen))
		}
	})
}

func mustCore(t *testing.T, code string) decide.ReasonCode {
	t.Helper()
	r, err := decide.Core(code)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
