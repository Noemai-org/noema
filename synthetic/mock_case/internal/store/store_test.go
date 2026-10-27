package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/store"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/testpg"
)

var t0 = time.Date(2026, 10, 1, 8, 40, 0, 0, time.UTC)

func open(t *testing.T) (*store.Store, testpg.DB) {
	t.Helper()
	db := testpg.New(t)
	s, err := store.Open(context.Background(), db.AppDSN, db.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, db
}

var callNo int64

func cmd(op string) store.Cmd {
	callNo++
	return store.Cmd{At: t0, Seed: 42, ScenarioID: "SCN-T", FaultPlanID: "fp_none", Operation: op, CallNo: callNo}
}

func newCase(key string) store.CreateCaseInput {
	return store.CreateCaseInput{TenantID: "ten_a", PrincipalID: "usr_1", TransactionRef: "TX88213", ReasonCode: "CREDIT_MISSING",
		OwnerQueue: "PAYMENTS_OPERATIONS", Priority: "NORMAL", CreatedByIntent: "int_01J9X3", Epoch: 0, IdempotencyKey: key}
}

// newCaseFor is a create for another subject: a case is created only if none exists for its transaction and epoch.
func newCaseFor(key, tx string) store.CreateCaseInput {
	in := newCase(key)
	in.TransactionRef = tx
	return in
}

func TestMigrateTwiceIsANoOp(t *testing.T) {
	s, _ := open(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// §34.4: same key and same payload returns the original case; same key and a different payload is a conflict.
func TestCreateCaseIdempotency(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	first, oc, err := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	if err != nil || oc != store.Created {
		t.Fatalf("%v %v", oc, err)
	}
	if first.Status != "OPEN" || first.VersionToken != "v1" || first.Reference != "CMP-2026-00001" {
		t.Fatalf("unexpected case %+v", first)
	}
	again, oc, err := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	if err != nil || oc != store.Replayed || again.CaseID != first.CaseID || again.Reference != first.Reference {
		t.Fatalf("replay: %v %v %+v", oc, err, again)
	}
	different := newCase("idk_1")
	different.TransactionRef = "TX99999"
	got, oc, err := s.CreateCase(ctx, cmd("create_case"), different)
	if err != nil || oc != store.IdempotencyConflict || got.CaseID != "" {
		t.Fatalf("conflict must be deterministic and leak nothing: %v %v %+v", oc, err, got)
	}
	// Another tenant may reuse the key: keys are scoped to a tenant.
	other := newCase("idk_1")
	other.TenantID = "ten_b"
	if _, oc, err := s.CreateCase(ctx, cmd("create_case"), other); err != nil || oc != store.Created {
		t.Fatalf("%v %v", oc, err)
	}
}

// 100 concurrent creates with one key give exactly one case, and references have no gap.
func TestConcurrentCreatesSameKeyYieldOneCase(t *testing.T) {
	s, db := open(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	counts := map[store.Outcome]int{}
	var mu sync.Mutex
	ids := map[string]bool{}
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, oc, err := s.CreateCase(ctx, store.Cmd{At: t0, Seed: 42, ScenarioID: "S", FaultPlanID: "fp_none", Operation: "create_case", CallNo: 1}, newCase("idk_race"))
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			counts[oc]++
			ids[c.CaseID] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	if counts[store.Created] != 1 || counts[store.Replayed] != 99 || len(ids) != 1 {
		t.Fatalf("outcomes %v, ids %v", counts, ids)
	}
	next, oc, err := s.CreateCase(ctx, cmd("create_case"), newCaseFor("idk_next", "TX00002"))
	if err != nil || oc != store.Created || next.Reference != "CMP-2026-00002" {
		t.Fatalf("references must stay gapless after lost races: %+v %v %v", next, oc, err)
	}
	conn := adminConn(t, db)
	var n int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM cases`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("%d cases, %v", n, err)
	}
}

func adminConn(t *testing.T, db testpg.DB) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), db.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

func appConn(t *testing.T, db testpg.DB) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), db.AppDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

// §34.4: route with expected version; a mismatch is a distinct outcome and applies nothing.
func TestRouteWithExpectedVersion(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	c, _, _ := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	stale, oc, err := s.RouteCase(ctx, cmd("route_case"), "ten_a", c.CaseID, "DISPUTES", 7, false)
	if err != nil || oc != store.VersionMismatch || stale.OwnerQueue != "PAYMENTS_OPERATIONS" || stale.VersionToken != "v1" {
		t.Fatalf("mismatch must report the current case and apply nothing: %v %v %+v", oc, err, stale)
	}
	routed, oc, err := s.RouteCase(ctx, cmd("route_case"), "ten_a", c.CaseID, "DISPUTES", 1, false)
	if err != nil || oc != store.Routed || routed.OwnerQueue != "DISPUTES" || routed.VersionToken != "v2" {
		t.Fatalf("%v %v %+v", oc, err, routed)
	}
	if _, oc, _ := s.RouteCase(ctx, cmd("route_case"), "ten_a", c.CaseID, "FRAUD_REVIEW", 1, false); oc != store.VersionMismatch {
		t.Fatalf("a retry with the old token must be refused, got %v", oc)
	}
	read, _, _ := s.GetCase(ctx, cmd("get_case"), "ten_a", c.CaseID)
	if read.OwnerQueue != "DISPUTES" || read.VersionToken != "v2" {
		t.Fatalf("read back: %+v", read)
	}
	if _, oc, _ := s.RouteCase(ctx, cmd("route_case"), "ten_b", c.CaseID, "DISPUTES", 2, false); oc != store.NotFound {
		t.Fatalf("another tenant must not see the case, got %v", oc)
	}
}

func TestConcurrentModificationBumpsVersionAndRefuses(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	c, _, _ := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	got, oc, err := s.RouteCase(ctx, cmd("route_case"), "ten_a", c.CaseID, "DISPUTES", 1, true)
	if err != nil || oc != store.VersionMismatch || got.VersionToken != "v2" || got.OwnerQueue != "PAYMENTS_OPERATIONS" {
		t.Fatalf("%v %v %+v", oc, err, got)
	}
	read, _, _ := s.GetCase(ctx, cmd("get_case"), "ten_a", c.CaseID)
	if read.VersionToken != "v2" {
		t.Fatalf("the competing modification must be real: %+v", read)
	}
}

// 50 concurrent routes with the same expected version: exactly one applies.
func TestConcurrentRoutesOnlyOneWins(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	c, _, _ := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, oc, err := s.RouteCase(ctx, store.Cmd{At: t0, ScenarioID: "S", FaultPlanID: "fp_none", Operation: "route_case", CallNo: 1}, "ten_a", c.CaseID, fmt.Sprintf("Q%d", i), 1, false)
			if err != nil {
				t.Error(err)
			}
			if oc == store.Routed {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	read, _, _ := s.GetCase(ctx, cmd("get_case"), "ten_a", c.CaseID)
	if won != 1 || read.VersionToken != "v2" {
		t.Fatalf("won=%d, final %+v", won, read)
	}
}

func TestComplaintsFollowTheSameRules(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	c, _, _ := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	first, oc, err := s.CreateComplaint(ctx, cmd("create_complaint"), "ten_a", c.CaseID, "idk_c1")
	if err != nil || oc != store.Created || first.ComplaintRef != "CPR-2026-00001" || !first.ClockStartedAt.Equal(first.LoggedAt) {
		t.Fatalf("%v %v %+v", oc, err, first)
	}
	again, oc, _ := s.CreateComplaint(ctx, cmd("create_complaint"), "ten_a", c.CaseID, "idk_c1")
	if oc != store.Replayed || again.ComplaintRef != first.ComplaintRef {
		t.Fatalf("%v %+v", oc, again)
	}
	c2, _, _ := s.CreateCase(ctx, cmd("create_case"), newCaseFor("idk_2", "TX00002"))
	if _, oc, _ := s.CreateComplaint(ctx, cmd("create_complaint"), "ten_a", c2.CaseID, "idk_c1"); oc != store.IdempotencyConflict {
		t.Fatalf("same key, other case: %v", oc)
	}
	if _, oc, _ := s.CreateComplaint(ctx, cmd("create_complaint"), "ten_a", "case_missing", "idk_c9"); oc != store.NotFound {
		t.Fatalf("unknown case: %v", oc)
	}
	read, oc, _ := s.GetComplaint(ctx, cmd("get_complaint"), "ten_a", first.ComplaintRef)
	if oc != store.Found || read.CaseID != c.CaseID {
		t.Fatalf("%v %+v", oc, read)
	}
	byKey, more, _ := s.ListComplaints(ctx, cmd("find_complaints"), "ten_a", "idk_c1", "", 0, 10)
	if len(byKey) != 1 || more {
		t.Fatalf("%v %v", byKey, more)
	}
}

func TestEvidenceIsAppendOnlyWithPayloadHash(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	c, _, _ := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	a, oc, err := s.AppendEvidence(ctx, cmd("append_evidence"), "ten_a", c.CaseID, "idk_e1", `{"note":"synthetic"}`)
	want := store.PayloadHash(`{"note":"synthetic"}`)
	if err != nil || oc != store.Created || a.PayloadHash != want {
		t.Fatalf("%v %v %+v", oc, err, a)
	}
	// Known SHA-256 of "abc": the hash is over the exact bytes received.
	if got := store.PayloadHash("abc"); got != "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("hash %s", got)
	}
	if _, oc, _ := s.AppendEvidence(ctx, cmd("append_evidence"), "ten_a", c.CaseID, "idk_e1", `{"note":"synthetic"}`); oc != store.Replayed {
		t.Fatalf("replay: %v", oc)
	}
	if _, oc, _ := s.AppendEvidence(ctx, cmd("append_evidence"), "ten_a", c.CaseID, "idk_e1", `{"note":"edited"}`); oc != store.IdempotencyConflict {
		t.Fatalf("same key other payload: %v", oc)
	}
	list, _, _ := s.ListEvidence(ctx, cmd("find_evidence"), "ten_a", "", c.CaseID, 0, 10)
	if len(list) != 1 || list[0].Payload != `{"note":"synthetic"}` {
		t.Fatalf("a conflicting append must not have changed or added evidence: %+v", list)
	}
}

// The database itself refuses what the API never offers (REQ-ENV-008: those operations are absent).
func TestDatabaseRefusesWhatTheServiceDoesNotOffer(t *testing.T) {
	s, db := open(t)
	ctx := context.Background()
	c, _, _ := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	s.CreateComplaint(ctx, cmd("create_complaint"), "ten_a", c.CaseID, "idk_c1")
	s.AppendEvidence(ctx, cmd("append_evidence"), "ten_a", c.CaseID, "idk_e1", "x")
	app := appConn(t, db)
	admin := adminConn(t, db)
	refused := []struct{ role, name, sql string }{
		{"app", "close a case", `UPDATE cases SET status = 'CLOSED', version = version + 1`},
		{"app", "resolve a case", `UPDATE cases SET status = 'RESOLVED', version = version + 1`},
		{"app", "downgrade priority", `UPDATE cases SET priority = 'LOW', version = version + 1`},
		{"app", "delete a case", `DELETE FROM cases`},
		{"app", "edit evidence", `UPDATE evidence SET payload = 'y'`},
		{"app", "delete evidence", `DELETE FROM evidence`},
		{"app", "edit a complaint clock", `UPDATE complaints SET clock_started_at = now()`},
		{"app", "delete a complaint", `DELETE FROM complaints`},
		{"app", "rewrite the command log", `UPDATE command_log SET status_code = 200`},
		{"app", "truncate evidence", `TRUNCATE evidence`},
		{"app", "skip a version", `UPDATE cases SET version = version + 2`},
		{"admin", "close a case as the owner", `UPDATE cases SET status = 'CLOSED', version = version + 1`},
		{"admin", "downgrade priority as the owner", `UPDATE cases SET priority = 'LOW', version = version + 1`},
		{"admin", "rewrite the reference as the owner", `UPDATE cases SET reference = 'CMP-2026-99999', version = version + 1`},
		{"admin", "move a case without advancing its version", `UPDATE cases SET owner_queue = 'DISPUTES'`},
		{"admin", "edit evidence as the owner", `UPDATE evidence SET payload = 'y'`},
		{"admin", "delete evidence as the owner", `DELETE FROM evidence`},
		{"admin", "truncate cases as the owner", `TRUNCATE cases CASCADE`},
	}
	for _, r := range refused {
		conn := app
		if r.role == "admin" {
			conn = admin
		}
		if _, err := conn.Exec(ctx, r.sql); err == nil {
			t.Errorf("%s was allowed to %s", r.role, r.name)
		}
	}
	if _, err := app.Exec(ctx, `UPDATE cases SET owner_queue = 'DISPUTES', version = version + 1, updated_at = now()`); err != nil {
		t.Errorf("the one permitted change, a queue move, was refused: %v", err)
	}
	var n int
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM cases) + (SELECT count(*) FROM evidence) + (SELECT count(*) FROM complaints)`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("rows survived? %d %v", n, err)
	}
}

// created_by_intent, epoch and the OPEN to ROUTED step (reconciliation with the pack's case entity).
func TestDatabaseAllowsOnlyOpenToRoutedAndFixesIntentAndEpoch(t *testing.T) {
	s, db := open(t)
	ctx := context.Background()
	s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	app, admin := appConn(t, db), adminConn(t, db)
	refused := []struct{ role, name, sql string }{
		{"app", "rewrite the intent", `UPDATE cases SET created_by_intent = 'int_x', version = version + 1`},
		{"app", "rewrite the epoch", `UPDATE cases SET epoch = 9, version = version + 1`},
		{"admin", "rewrite the intent as the owner", `UPDATE cases SET created_by_intent = 'int_x', version = version + 1`},
		{"admin", "rewrite the epoch as the owner", `UPDATE cases SET epoch = 9, version = version + 1`},
		{"app", "close an open case", `UPDATE cases SET status = 'CLOSED', version = version + 1`},
		{"admin", "close an open case as the owner", `UPDATE cases SET status = 'CLOSED', version = version + 1`},
		{"app", "set a negative epoch", `UPDATE cases SET epoch = -1, version = version + 1`},
	}
	for _, r := range refused {
		conn := app
		if r.role == "admin" {
			conn = admin
		}
		if _, err := conn.Exec(ctx, r.sql); err == nil {
			t.Errorf("%s was allowed to %s", r.role, r.name)
		}
	}
	if _, err := app.Exec(ctx, `UPDATE cases SET status = 'ROUTED', owner_queue = 'DISPUTES', version = version + 1, updated_at = now()`); err != nil {
		t.Fatalf("OPEN to ROUTED was refused: %v", err)
	}
	for _, r := range []struct{ role, name, sql string }{
		{"app", "reopen a routed case", `UPDATE cases SET status = 'OPEN', version = version + 1`},
		{"admin", "reopen a routed case as the owner", `UPDATE cases SET status = 'OPEN', version = version + 1`},
		{"app", "close a routed case", `UPDATE cases SET status = 'CLOSED', version = version + 1`},
		{"admin", "close a routed case as the owner", `UPDATE cases SET status = 'CLOSED', version = version + 1`},
	} {
		conn := app
		if r.role == "admin" {
			conn = admin
		}
		if _, err := conn.Exec(ctx, r.sql); err == nil {
			t.Errorf("%s was allowed to %s", r.role, r.name)
		}
	}
}

func TestResetEmptiesAndRestartsNumbering(t *testing.T) {
	s, db := open(t)
	ctx := context.Background()
	first, _, _ := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	s.AppendEvidence(ctx, cmd("append_evidence"), "ten_a", first.CaseID, "idk_e", "x")
	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	c, oc, err := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	if err != nil || oc != store.Created || c.Reference != "CMP-2026-00001" || c.CaseID != first.CaseID {
		t.Fatalf("a reset run must reproduce the same identifiers: %v %v %+v vs %+v", oc, err, c, first)
	}
	s.AppendEvidence(ctx, cmd("append_evidence"), "ten_a", c.CaseID, "idk_e", "x")
	// The triggers are back on after a reset: it does not leave the tables editable.
	if _, err := adminConn(t, db).Exec(ctx, `DELETE FROM evidence`); err == nil {
		t.Fatal("evidence became deletable after a reset")
	}
}

func TestPagingHasAnExplicitCompletenessMarker(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		s.CreateCase(ctx, cmd("create_case"), newCaseFor(fmt.Sprintf("idk_%d", i), fmt.Sprintf("TX%05d", i)))
	}
	f := store.CaseFilter{TenantID: "ten_a", PrincipalID: "usr_1"}
	var seen []string
	after := int64(0)
	for pages := 0; pages < 10; pages++ {
		items, more, err := s.ListCases(ctx, cmd("find_cases"), f, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range items {
			seen = append(seen, c.Reference)
			after = c.Seq
		}
		if !more {
			break
		}
		if len(items) != 2 {
			t.Fatalf("an incomplete page must be full: %d", len(items))
		}
	}
	if len(seen) != 5 || seen[0] != "CMP-2026-00001" || seen[4] != "CMP-2026-00005" {
		t.Fatalf("saw %v", seen)
	}
}

func TestCommandLogCarriesScenarioSeedAndPlan(t *testing.T) {
	s, db := open(t)
	ctx := context.Background()
	idx := 0
	c := cmd("create_case")
	c.FaultIndex, c.FaultEffect, c.Suppressed = &idx, "DROP_RESPONSE", true
	if _, _, err := s.CreateCase(ctx, c, newCase("idk_1")); err != nil {
		t.Fatal(err)
	}
	var scenario, plan, effect string
	var seed int64
	var committed, suppressed bool
	var status int
	err := adminConn(t, db).QueryRow(ctx, `SELECT scenario_id, seed, fault_plan_id, fault_effect, committed, response_suppressed, status_code FROM command_log`).
		Scan(&scenario, &seed, &plan, &effect, &committed, &suppressed, &status)
	if err != nil || scenario != "SCN-T" || seed != 42 || plan != "fp_none" || effect != "DROP_RESPONSE" || !committed || !suppressed || status != 0 {
		t.Fatalf("%v %s %d %s %s %v %v %d", err, scenario, seed, plan, effect, committed, suppressed, status)
	}
}

// Create-if-absent (the precondition of a create is "no case exists for this subject and epoch"): a new key for a
// subject that has a case is refused, names the case that exists, and writes nothing, not even a reference number.
func TestCreateCaseIfAbsentRefusesASecondCaseForTheSubject(t *testing.T) {
	s, db := open(t)
	ctx := context.Background()
	first, oc, err := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1"))
	if err != nil || oc != store.Created {
		t.Fatalf("%v %v", oc, err)
	}
	got, oc, err := s.CreateCase(ctx, cmd("create_case"), newCase("idk_2"))
	if err != nil || oc != store.PreconditionFailed || got.CaseID != first.CaseID || got.VersionToken != first.VersionToken {
		t.Fatalf("a second key for the same subject and epoch: %v %v %+v", oc, err, got)
	}
	if oc.Status() != 412 || oc.ErrorCode() != "PRECONDITION_FAILED" {
		t.Fatalf("the refusal is a 412 PRECONDITION_FAILED: %d %s", oc.Status(), oc.ErrorCode())
	}
	// The refused key was not recorded: it is still free, and the refusal is repeatable.
	again, oc, _ := s.CreateCase(ctx, cmd("create_case"), newCase("idk_2"))
	if oc != store.PreconditionFailed || again.CaseID != first.CaseID {
		t.Fatalf("a refusal repeats: %v %+v", oc, again)
	}
	// The same key and the same payload is still a replay of the original create, not a refusal.
	if rep, oc, _ := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1")); oc != store.Replayed || rep.CaseID != first.CaseID {
		t.Fatalf("replay of the original: %v %+v", oc, rep)
	}
	// Another epoch, another transaction and another tenant are other subjects.
	epoch1 := newCase("idk_e1")
	epoch1.Epoch = 1
	tenantB := newCase("idk_b")
	tenantB.TenantID = "ten_b"
	for name, in := range map[string]store.CreateCaseInput{"epoch": epoch1, "transaction": newCaseFor("idk_t", "TX00002"), "tenant": tenantB} {
		if _, oc, err := s.CreateCase(ctx, cmd("create_case"), in); err != nil || oc != store.Created {
			t.Fatalf("%s is another subject: %v %v", name, oc, err)
		}
	}
	// No reference number was spent by the refusals: the next reference follows the last created one.
	next, oc, _ := s.CreateCase(ctx, cmd("create_case"), newCaseFor("idk_n", "TX00003"))
	if oc != store.Created || next.Reference != "CMP-2026-00005" {
		t.Fatalf("references stay gapless across refusals: %v %+v", oc, next)
	}
	var n int
	if err := adminConn(t, db).QueryRow(ctx, `SELECT count(*) FROM cases`).Scan(&n); err != nil || n != 5 {
		t.Fatalf("%d cases, %v", n, err)
	}
}

// The guarantee does not rest on the code path: the database refuses a second open case for a subject and epoch.
func TestTheDatabaseRefusesASecondOpenCaseForTheSubject(t *testing.T) {
	s, db := open(t)
	ctx := context.Background()
	if _, _, err := s.CreateCase(ctx, cmd("create_case"), newCase("idk_1")); err != nil {
		t.Fatal(err)
	}
	_, err := adminConn(t, db).Exec(ctx, `INSERT INTO cases (case_id, seq, tenant_id, reference, principal_id, transaction_ref, reason_code, status,
 owner_queue, priority, idempotency_key, request_hash, version, created_at, updated_at, created_by_intent, epoch)
 VALUES ('case_rogue', 99, 'ten_a', 'CMP-2026-00099', 'usr_1', 'TX88213', 'CREDIT_MISSING', 'OPEN', 'PAYMENTS_OPERATIONS', 'NORMAL', 'idk_rogue', 'h', 1, now(), now(), 'int_01J9X3', 0)`)
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != "23505" || pe.ConstraintName != "cases_subject_epoch_uq" {
		t.Fatalf("the unique index must refuse it: %v", err)
	}
}

// 100 creators with 100 different keys race for one subject: exactly one wins, the other 99 are refused and name the
// winner, one case exists, and the next reference has no gap.
func TestConcurrentCreatesForOneSubjectYieldOneCase(t *testing.T) {
	s, db := open(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[store.Outcome]int{}
	ids := map[string]bool{}
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, oc, err := s.CreateCase(ctx, store.Cmd{At: t0, Seed: 42, ScenarioID: "S", FaultPlanID: "fp_none", Operation: "create_case", CallNo: 1}, newCase(fmt.Sprintf("idk_rival_%d", i)))
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			counts[oc]++
			ids[c.CaseID] = true
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if counts[store.Created] != 1 || counts[store.PreconditionFailed] != 99 || len(ids) != 1 {
		t.Fatalf("outcomes %v, case ids %v", counts, ids)
	}
	next, oc, err := s.CreateCase(ctx, cmd("create_case"), newCaseFor("idk_next", "TX00002"))
	if err != nil || oc != store.Created || next.Reference != "CMP-2026-00002" {
		t.Fatalf("references stay gapless after lost races: %+v %v %v", next, oc, err)
	}
	var n int
	if err := adminConn(t, db).QueryRow(ctx, `SELECT count(*) FROM cases`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("%d cases, %v", n, err)
	}
}
