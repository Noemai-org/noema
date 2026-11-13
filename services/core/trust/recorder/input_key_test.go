package recorder_test

// TRU-05 follow-up (A172, A186; migration 0027; REQ-TRU-007, REQ-TRU-008, INV-21, INV-25): a Decision Record is written once for its full input tuple (tenant,
// intent ID, intent hash, sealed context snapshot, policy version). Asking again for the same tuple returns the stored record and its grants and writes
// nothing more: no second record, no second grant, no second event. A new snapshot or another policy version is a new decision. Real PostgreSQL, real tenant
// logins, the real hash chain, as in recorder_test.go. All data is SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// evaluatedWith is evaluated(t) with another snapshot and policy version: the same intent evaluated against other evidence or under another policy.
func evaluatedWith(t *testing.T, snapshot, version string, extraRules ...string) *decide.BasisBuilder {
	t.Helper()
	b, err := decide.NewBasisBuilder("fin.pack@1.0.0", epoch.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RecordEvidence(snapshot, []decide.Freshness{freshness()}); err != nil {
		t.Fatal(err)
	}
	rules := []decide.PolicyRuleVersion{{RuleID: "fin.pol.default.permit_case_writes", PolicyID: "fin.pol", Version: version}}
	for _, id := range extraRules {
		rules = append(rules, decide.PolicyRuleVersion{RuleID: id, PolicyID: "fin.pol", Version: version})
	}
	if err := b.RecordPolicy(rules); err != nil {
		t.Fatal(err)
	}
	return b
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A186: the same tuple returns the stored decision. The second request is built from fresh objects (a new builder, a new decision value, a new step), as a
// retried or resumed run would build it; the writer has an ID source that would give a new decision ID, so a second write could not hide.
func TestTheSameInputReturnsTheStoredDecisionAndWritesNothingMore(t *testing.T) {
	w := newWorld(t)
	b1 := evaluated(t)
	first, err := w.write(t, request(t, decision(t, decide.Allow, b1), b1, step(t)), true)
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore := w.events(t)
	b2 := evaluated(t)
	second, err := w.write(t, request(t, decision(t, decide.Allow, b2), b2, step(t)), true)
	if err != nil {
		t.Fatalf("the same input a second time: %v", err)
	}
	if second.Record.DecisionId != first.Record.DecisionId || asJSON(t, second.Record) != asJSON(t, first.Record) {
		t.Fatalf("the second write returned another record:\n first %s\nsecond %s", asJSON(t, first.Record), asJSON(t, second.Record))
	}
	if len(second.Grants) != 1 || asJSON(t, second.Grants[0]) != asJSON(t, first.Grants[0]) {
		t.Fatalf("the stored grant was not returned as it was issued:\n first %s\nsecond %v", asJSON(t, first.Grants), asJSON(t, second.Grants))
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 1 {
		t.Errorf("%d decision records, want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`); n != 1 {
		t.Errorf("%d grants, want 1: a stored decision must not get a second grant (INV-03, INV-25)", n)
	}
	if after := w.events(t); len(after) != len(eventsBefore) {
		t.Errorf("the Record gained events on a read of a stored decision: before %v, after %v", eventsBefore, after)
	}
}

// A stored decision that is not an ALLOW is returned the same way, and with no grants.
func TestTheSameInputReturnsAStoredRefusalToo(t *testing.T) {
	w := newWorld(t)
	b1 := evaluated(t)
	first, err := w.write(t, request(t, decision(t, decide.Reject, b1), b1), true)
	if err != nil {
		t.Fatal(err)
	}
	b2 := evaluated(t)
	second, err := w.write(t, request(t, decision(t, decide.Reject, b2), b2), true)
	if err != nil || second.Record.DecisionId != first.Record.DecisionId || len(second.Grants) != 0 {
		t.Fatalf("got %+v, %v; want the stored REJECT and no grants", second, err)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 1 {
		t.Errorf("%d decision records, want 1", n)
	}
}

// A186: a new snapshot makes a new decision; so does another policy version.
func TestANewSnapshotOrAnotherPolicyVersionMakesANewDecision(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)
	first, err := w.write(t, request(t, decision(t, decide.Reject, b), b), true)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{string(first.Record.DecisionId): true}
	for name, nb := range map[string]*decide.BasisBuilder{
		"a new context snapshot": evaluatedWith(t, "evs_02XYZ", "3.1"),
		"another policy version": evaluatedWith(t, "evs_01ABC", "3.2"),
	} {
		res, err := w.write(t, request(t, decision(t, decide.Reject, nb), nb), true)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if seen[string(res.Record.DecisionId)] {
			t.Errorf("%s returned the decision of another input (%s)", name, res.Record.DecisionId)
		}
		seen[string(res.Record.DecisionId)] = true
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 3 {
		t.Errorf("%d decision records, want 3", n)
	}
	// and the same intent under another tenant's intent hash or intent ID is another input as well
	other := request(t, decision(t, decide.Reject, b), b)
	other.IntentHash = h(9)
	if res, err := w.write(t, other, true); err != nil || seen[string(res.Record.DecisionId)] {
		t.Errorf("another intent hash: %+v, %v", res, err)
	}
}

// INV-21: the same input must give the same decision. If the stored decision and the new evaluation disagree, the writer returns neither: it refuses
// (ErrDecisionConflict) and writes nothing, and the error never carries the decision's content.
func TestAStoredDecisionThatTheNewEvaluationContradictsIsRefused(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)
	if _, err := w.write(t, request(t, decision(t, decide.Allow, b), b, step(t)), true); err != nil {
		t.Fatal(err)
	}
	before := w.events(t)
	cases := map[string]func() recorder.Request{
		"another outcome": func() recorder.Request {
			nb := evaluated(t)
			return request(t, decision(t, decide.Reject, nb), nb)
		},
		"more grants than were issued": func() recorder.Request {
			nb := evaluated(t)
			s2 := step(t)
			s2.PlanStep = 2
			return request(t, decision(t, decide.Allow, nb), nb, step(t), s2)
		},
		"another plan hash": func() recorder.Request {
			nb := evaluated(t)
			s := step(t)
			s.PlanHash = h(8)
			return request(t, decision(t, decide.Allow, nb), nb, s)
		},
		"another set of rule versions under the same bound policy": func() recorder.Request {
			nb := evaluatedWith(t, "evs_01ABC", "3.1", "fin.pol.default.other_rule")
			return request(t, decision(t, decide.Allow, nb), nb, step(t))
		},
	}
	for name, mk := range cases {
		_, err := w.write(t, mk(), true)
		if !errors.Is(err, recorder.ErrDecisionConflict) {
			t.Errorf("%s: got %v, want ErrDecisionConflict", name, err)
		}
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 1 {
		t.Errorf("%d decision records after the refusals, want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`); n != 1 {
		t.Errorf("%d grants after the refusals, want 1", n)
	}
	if after := w.events(t); len(after) != len(before) {
		t.Errorf("a refused write changed the Record: before %v, after %v", before, after)
	}
}

// Two writers of the same input at the same time get one decision: the second waits for the first's transaction and then reads what it committed. Without the
// lock the second would find nothing, insert, and fail on the unique index (or, without the index, write a second decision).
func TestTwoWritersOfTheSameInputGetOneDecision(t *testing.T) {
	w := newWorld(t)
	b1 := evaluated(t)
	tx1, err := w.trust.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	first, err := w.w.Write(ctx(t), tx1, request(t, decision(t, decide.Allow, b1), b1, step(t)))
	if err != nil {
		_ = tx1.Rollback(ctx(t))
		t.Fatal(err)
	}
	type outcome struct {
		res recorder.Result
		err error
	}
	done := make(chan outcome, 1)
	c2 := ctx(t)
	tx2, err := w.trust.Begin(c2)
	if err != nil {
		_ = tx1.Rollback(ctx(t))
		t.Fatal(err)
	}
	b2 := evaluated(t)
	r2 := request(t, decision(t, decide.Allow, b2), b2, step(t))
	go func() {
		res, werr := w.w.Write(c2, tx2, r2)
		if werr != nil {
			_ = tx2.Rollback(c2)
		} else {
			werr = tx2.Commit(c2)
		}
		done <- outcome{res, werr}
	}()
	// The second writer must be waiting on a lock of the first, not finished and not past the lookup.
	waited := false
	for tries := 0; tries < 2000; tries++ { // a bound on the wait, about 20 seconds
		time.Sleep(10 * time.Millisecond)
		if w.count(t, `SELECT count(*) FROM pg_locks WHERE NOT granted`) > 0 {
			waited = true
			break
		}
		select {
		case o := <-done:
			_ = tx1.Rollback(ctx(t))
			t.Fatalf("the second writer finished while the first was uncommitted: %+v, %v", o.res.Record.DecisionId, o.err)
		default:
		}
	}
	if !waited {
		_ = tx1.Rollback(ctx(t))
		t.Fatal("the second writer never waited for the first")
	}
	if err := tx1.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	o := <-done
	if o.err != nil {
		t.Fatalf("the second writer: %v", o.err)
	}
	if o.res.Record.DecisionId != first.Record.DecisionId || len(o.res.Grants) != 1 || o.res.Grants[0].GrantId != first.Grants[0].GrantId {
		t.Fatalf("the second writer returned another decision: %s against %s", o.res.Record.DecisionId, first.Record.DecisionId)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 1 {
		t.Errorf("%d decision records, want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`); n != 1 {
		t.Errorf("%d grants, want 1", n)
	}
}

// seedEvidence stores one SYNTHETIC evidence item of the tenant (the Evidence Layer's write is not under test here): a payload row and the item that points at it.
func (w *world) seedEvidence(t *testing.T, ev *pgxpool.Pool, n int) string {
	t.Helper()
	ref, id := fmt.Sprintf("pay_%d", n), fmt.Sprintf("evr_aa%d", n)
	if _, err := ev.Exec(ctx(t), `INSERT INTO evidence_payload (payload_ref, tenant_id, json, raw_hash, sensitivity, created_at)
		VALUES ($1, $2, '{"synthetic": true}', $3, 'S2_internal', '2026-11-05T09:00:00Z')`, ref, tenant, fmt.Sprintf("sha256:%064x", 1000+n)); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if _, err := ev.Exec(ctx(t), `INSERT INTO evidence_item (evidence_id, tenant_id, correlation_id, source, source_record_id, read_by, read_at, effective_at,
		freshness_sla_seconds, authority_class, resource_scope, principal_scope, sensitivity, fact_status, payload_hash, payload_ref, created_at)
		VALUES ($1, $2, 'corr_synthetic', 'fin.src.core_ledger', 'rec-1', 'TRUST', '2026-11-05T09:00:00Z', '2026-11-05T08:59:00Z',
		60, 'AUTHORITATIVE_FOR:debit_posted_or_reversed', 'res_synthetic', 'prn_synthetic', 'S2_internal', 'KNOWN', $3, $4, '2026-11-05T09:00:00Z')`,
		id, tenant, fmt.Sprintf("sha256:%064x", n), ref); err != nil {
		t.Fatalf("item: %v", err)
	}
	return id
}

// seal seals the given evidence in its own transaction as the tenant's Trust login, with the real Sealer (REQ-CTX-004).
func (w *world) seal(t *testing.T, evidence ...string) snapshot.Sealed {
	t.Helper()
	tx, err := w.trust.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	s := snapshot.Sealer{Rec: w.w.Appender, Clock: w.w.Clock, IDs: w.w.IDs}
	sealed, err := s.Seal(ctx(t), tx, snapshot.Input{TenantID: tenant, CorrelationID: "corr_ABC123", EvidenceIDs: evidence, ReconciliationID: "rec_synth1", Classification: "CONSISTENT"})
	if err != nil {
		_ = tx.Rollback(ctx(t))
		t.Fatal(err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return sealed
}

// A186: new evidence gives a new snapshot and a new decision. Each seal mints its own snapshot ID, so a fresh read of the same evidence is also a new snapshot; only a
// request that names the very same snapshot gets the stored decision. The writer is given the real Sealer's snapshots, not made-up IDs.
func TestNewEvidenceGivesANewSnapshotAndANewDecision(t *testing.T) {
	w := newWorld(t)
	ev := testpg.TenantLogin(t, w.admin, w.dsn, "evidence_rw", tenant) // the Evidence Layer's own login writes evidence, as in production
	e1, e2 := w.seedEvidence(t, ev, 1), w.seedEvidence(t, ev, 2)
	decide1 := func(s snapshot.Sealed) recorder.Result {
		b := evaluatedWith(t, s.SnapshotID, "3.1")
		res, err := w.write(t, request(t, decision(t, decide.Reject, b), b), true)
		if err != nil {
			t.Fatalf("decision for %s: %v", s.SnapshotID, err)
		}
		return res
	}
	first := w.seal(t, e1)
	again := w.seal(t, e1)    // the same evidence, read again
	more := w.seal(t, e1, e2) // new evidence
	if first.SnapshotID == again.SnapshotID || first.SnapshotID == more.SnapshotID || again.SnapshotID == more.SnapshotID {
		t.Fatalf("each seal must mint its own snapshot: %s %s %s", first.SnapshotID, again.SnapshotID, more.SnapshotID)
	}
	if first.EvidenceSetHash != again.EvidenceSetHash || first.EvidenceSetHash == more.EvidenceSetHash {
		t.Fatalf("the set hash follows the evidence, not the seal: %s %s %s", first.EvidenceSetHash, again.EvidenceSetHash, more.EvidenceSetHash)
	}
	d1, d1again, d2 := decide1(first), decide1(again), decide1(more)
	ids := map[string]bool{string(d1.Record.DecisionId): true, string(d1again.Record.DecisionId): true, string(d2.Record.DecisionId): true}
	if len(ids) != 3 {
		t.Fatalf("a new snapshot must be a new decision: %v", ids)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 3 {
		t.Errorf("%d decision records, want 3", n)
	}
	// the same snapshot, named again, is the stored decision (not a fourth)
	if back := decide1(first); back.Record.DecisionId != d1.Record.DecisionId {
		t.Errorf("the same snapshot returned %s, want the stored %s", back.Record.DecisionId, d1.Record.DecisionId)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 3 {
		t.Errorf("%d decision records after the repeat, want 3", n)
	}
}
