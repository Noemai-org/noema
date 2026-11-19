package db_test

// TRU-12, migration 0032: the confirmation REQUEST and the approver's ANSWER (REQ-TRU-009, REQ-AUD-001, REQ-SEC-004; SoT 43.2a; INV-11, INV-12, INV-17, INV-25). These tests connect as real
// Trust logins, one per tenant, under forced row security, against a real PostgreSQL; the signing extension (pgsodium) of the test server is the CI one, so they run in CI. All data is SYNTHETIC.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

var confirmationTables = []string{"confirmation_request", "confirmation_answer", "confirmation_expiry"}

// A368 (interim, relayed, not yet committed): every time of a confirmation row comes from the DATABASE clock. The tests therefore pass a time-to-live, never a time, and wait on the database's own
// clock (pg_sleep) for a request to expire; they never read the Go clock.
const (
	cfOpen  = time.Hour       // a request that stays open for the whole test
	cfShort = 2 * time.Second // a request the test lets expire
)

type cfWorld struct {
	admin  *pgxpool.Pool
	trustA *pgxpool.Pool
	trustB *pgxpool.Pool
	execA  *pgxpool.Pool
	n      int
}

func cfCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Minute)
}

func newCfWorld(t *testing.T) *cfWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A'), ('ten_b', 'B')`); err != nil {
		t.Fatal(err)
	}
	return &cfWorld{admin: admin, trustA: testpg.TenantLogin(t, admin, dsn, "trust_rw", "ten_a"), trustB: testpg.TenantLogin(t, admin, dsn, "trust_rw", "ten_b"),
		execA: testpg.TenantLogin(t, admin, dsn, "execution_rw", "ten_a")}
}

// cfBound is the eight values of SoT 43.2a. The test computes the binding hash itself (sorted keys, no white space), so that platform/db does not import trust.
type cfBound struct {
	PrincipalID, IntentHash, CapabilityID, TargetRef, ParamsHash, EvidenceSetHash, PlanHash, PolicyVersion string
}

func cfHash(b cfBound) string {
	s := `{"capability_id":"` + b.CapabilityID + `","evidence_set_hash":"` + b.EvidenceSetHash + `","intent_hash":"` + b.IntentHash + `","params_hash":"` + b.ParamsHash +
		`","plan_hash":"` + b.PlanHash + `","policy_version":"` + b.PolicyVersion + `","principal_id":"` + b.PrincipalID + `","target_ref":"` + b.TargetRef + `"}`
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cfBinding(target string) cfBound {
	return cfBound{PrincipalID: "prn_1", IntentHash: "sha256:i1", CapabilityID: "cap.change", TargetRef: target, ParamsHash: "sha256:p1", EvidenceSetHash: "sha256:e1",
		PlanHash: "sha256:l1", PolicyVersion: "pol_v1"}
}

// the insert names no requested_at: the database sets it (A368); expires_at is the database's now() plus the time-to-live in seconds
const cfInsertRequest = `INSERT INTO confirmation_request (tenant_id, confirmation_request_id, correlation_id, intent_id, intent_version, principal_id, intent_hash, capability_id, target_ref,
	params_hash, evidence_set_hash, plan_hash, policy_version, binding_hash, requirement, requester_id, expires_at, supersedes_request_id)
	VALUES ($1, $2, 'cor_1', 'int_1', 1, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'prn_1', now() + make_interval(secs => $13::float8), NULLIF($14, ''))`

// request inserts a request as pool for tenant, with a correct binding hash, and returns the error.
func (w *cfWorld) request(t *testing.T, pool *pgxpool.Pool, tenant, id, supersedes string, b cfBound, requirement string, ttl time.Duration) error {
	t.Helper()
	return w.requestWithHash(pool, tenant, id, supersedes, b, cfHash(b), requirement, ttl)
}

func (w *cfWorld) requestWithHash(pool *pgxpool.Pool, tenant, id, supersedes string, b cfBound, hash, requirement string, ttl time.Duration) error {
	c, cancel := cfCtx()
	defer cancel()
	_, err := pool.Exec(c, cfInsertRequest, tenant, id, b.PrincipalID, b.IntentHash, b.CapabilityID, b.TargetRef, b.ParamsHash, b.EvidenceSetHash, b.PlanHash, b.PolicyVersion, hash,
		requirement, ttl.Seconds(), supersedes)
	return err
}

// answer and expire name no time: the database sets answered_at and observed_at (A368).
func (w *cfWorld) answer(pool *pgxpool.Pool, tenant, event, request, approver, response string) error {
	c, cancel := cfCtx()
	defer cancel()
	_, err := pool.Exec(c, `INSERT INTO confirmation_answer (tenant_id, confirmation_event_id, confirmation_request_id, response, approver_id, approver_roles)
		VALUES ($1, $2, $3, $4, $5, ARRAY['reviewer'])`, tenant, event, request, response, approver)
	return err
}

func (w *cfWorld) expire(pool *pgxpool.Pool, tenant, request string) error {
	c, cancel := cfCtx()
	defer cancel()
	_, err := pool.Exec(c, `INSERT INTO confirmation_expiry (tenant_id, confirmation_request_id) VALUES ($1, $2)`, tenant, request)
	return err
}

// waitExpired blocks on the DATABASE clock until the request has expired (never the Go clock).
func (w *cfWorld) waitExpired(t *testing.T, request string) {
	t.Helper()
	c, cancel := cfCtx()
	defer cancel()
	if _, err := w.admin.Exec(c, `SELECT pg_sleep(greatest(0, extract(epoch FROM expires_at - clock_timestamp())) + 0.2) FROM confirmation_request WHERE confirmation_request_id = $1`, request); err != nil {
		t.Fatal(err)
	}
}

// wantCode: the database refused with exactly this SQLSTATE (the typed refusals of A368: 23E01 an answer after the expiry, 23S01 an answer to a superseded request).
func wantSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != code {
		t.Fatalf("got %v, want SQLSTATE %s", err, code)
	}
}

func count(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx(t), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func wantState(t *testing.T, err error, class string) {
	t.Helper()
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || !strings.HasPrefix(pe.Code, class) {
		t.Fatalf("got %v, want an error of class %s", err, class)
	}
}

func wantRefused(t *testing.T, err error) {
	t.Helper()
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || !(pe.Code == denied || strings.HasPrefix(pe.Code, violation)) {
		t.Fatalf("got %v, want a refusal", err)
	}
}

// REQ-SEC-004, INV-11: forced row security, a policy bound to the login's tenant, and no privilege for anyone but Trust.
func TestConfirmationTablesHaveForcedRowSecurityBoundToTheLoginTenantAndTrustOnly(t *testing.T) {
	pool := migrated(t)
	for _, tbl := range confirmationTables {
		var enabled, forced bool
		if err := pool.QueryRow(ctx(t), `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = ('public.' || $1)::regclass`, tbl).Scan(&enabled, &forced); err != nil {
			t.Fatalf("%s: %v", tbl, err)
		}
		if !enabled || !forced {
			t.Errorf("%s: row security enabled=%v forced=%v, want both", tbl, enabled, forced)
		}
		rows, err := pool.Query(ctx(t), `SELECT policyname, roles::text, coalesce(qual, ''), coalesce(with_check, '') FROM pg_policies WHERE schemaname = 'public' AND tablename = $1`, tbl)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rows.Next() {
			var name, roles, qual, check string
			if err := rows.Scan(&name, &roles, &qual, &check); err != nil {
				t.Fatal(err)
			}
			n++
			if roles != "{trust_rw}" || !strings.Contains(qual+check, "decision_login_tenant()") {
				t.Errorf("%s.%s: roles %s using %q check %q: want trust_rw and the login's tenant", tbl, name, roles, qual, check)
			}
		}
		rows.Close()
		if n != 2 {
			t.Errorf("%s: %d policies, want a read and a write policy", tbl, n)
		}
		var priv []string
		pr, err := pool.Query(ctx(t), `SELECT grantee || ':' || privilege_type FROM information_schema.role_table_grants WHERE table_schema = 'public' AND table_name = $1
			AND grantee IN ('PUBLIC', 'execution_rw', 'outcome_rw', 'evidence_rw', 'orchestrator_rw', 'record_append', 'record_read')`, tbl)
		if err != nil {
			t.Fatal(err)
		}
		for pr.Next() {
			var s string
			if err := pr.Scan(&s); err != nil {
				t.Fatal(err)
			}
			priv = append(priv, s)
		}
		pr.Close()
		if len(priv) != 0 {
			t.Errorf("%s: privileges for a role other than Trust: %v", tbl, priv)
		}
	}
	w := newCfWorld(t)
	for _, tbl := range confirmationTables {
		_, err := w.execA.Exec(ctx(t), `SELECT 1 FROM `+tbl)
		wantState(t, err, denied)
	}
}

// A request carries all eight bound values (SoT 43.2a), each NOT NULL and in the form the binding hash accepts, and a binding hash the database recomputes.
func TestARequestCarriesAllEightBoundValuesAndTheirHash(t *testing.T) {
	w := newCfWorld(t)
	for _, col := range []string{"principal_id", "intent_hash", "capability_id", "target_ref", "params_hash", "evidence_set_hash", "plan_hash", "policy_version", "binding_hash", "requirement",
		"requester_id", "expires_at", "tenant_id", "confirmation_request_id"} {
		var notNull bool
		if err := w.admin.QueryRow(ctx(t), `SELECT attnotnull FROM pg_attribute WHERE attrelid = 'public.confirmation_request'::regclass AND attname = $1`, col).Scan(&notNull); err != nil || !notNull {
			t.Errorf("confirmation_request.%s: not null=%v (%v), want NOT NULL", col, notNull, err)
		}
	}
	good := cfBinding("tgt_1")
	if err := w.request(t, w.trustA, "ten_a", "cfr_ok1", "", good, "MAKER_CHECKER", cfOpen); err != nil {
		t.Fatalf("a well-formed request: %v", err)
	}
	for name, mutate := range map[string]func(*cfBound){
		"empty principal": func(b *cfBound) { b.PrincipalID = "" }, "empty intent hash": func(b *cfBound) { b.IntentHash = "" },
		"empty capability": func(b *cfBound) { b.CapabilityID = "" }, "empty target": func(b *cfBound) { b.TargetRef = "" },
		"empty params": func(b *cfBound) { b.ParamsHash = "" }, "empty evidence set": func(b *cfBound) { b.EvidenceSetHash = "" },
		"empty plan": func(b *cfBound) { b.PlanHash = "" }, "empty policy": func(b *cfBound) { b.PolicyVersion = "" },
		"a quote in a value": func(b *cfBound) { b.TargetRef = `a"b` },
	} {
		b := good
		b.TargetRef = "tgt_2"
		mutate(&b)
		err := w.requestWithHash(w.trustA, "ten_a", "cfr_bad1", "", b, "sha256:"+strings.Repeat("a", 64), "SELF", cfOpen)
		if err == nil {
			t.Errorf("%s: the database accepted the request", name)
		}
	}
	other := good
	other.TargetRef = "tgt_3"
	err := w.requestWithHash(w.trustA, "ten_a", "cfr_bad2", "", other, "sha256:"+strings.Repeat("b", 64), "SELF", cfOpen)
	wantState(t, err, violation) // the hash is not the one of the eight values
	for name, req := range map[string]string{"unknown": "ANYONE", "empty role": "ROLE:", "lower": "self"} {
		if err := w.request(t, w.trustA, "ten_a", "cfr_bad3", "", cfBinding("tgt_4"), req, cfOpen); err == nil {
			t.Errorf("requirement %s accepted", name)
		}
	}
	if err := w.request(t, w.trustA, "ten_a", "cfr_ok2", "", cfBinding("tgt_5"), "ROLE:reviewer", cfOpen); err != nil {
		t.Errorf("ROLE:reviewer: %v", err)
	}
	if err := w.request(t, w.trustA, "ten_a", "cfr_bad4", "", cfBinding("tgt_6"), "SELF", 0); err == nil {
		t.Error("a request that expires when it is made was accepted")
	}
	if err := w.request(t, w.trustA, "ten_a", "badid", "", cfBinding("tgt_7"), "SELF", cfOpen); err == nil {
		t.Error("an ID outside the cfr_ form was accepted")
	}
}

// REQ-SEC-004, INV-11: a login reads and writes only its own tenant's rows.
func TestConfirmationRowsAreIsolatedByTenant(t *testing.T) {
	w := newCfWorld(t)
	if err := w.request(t, w.trustA, "ten_a", "cfr_a1", "", cfBinding("tgt_1"), "SELF", cfShort); err != nil {
		t.Fatal(err)
	}
	if err := w.answer(w.trustA, "ten_a", "cfe_a1", "cfr_a1", "prn_2", "ACCEPT"); err != nil {
		t.Fatal(err)
	}
	w.waitExpired(t, "cfr_a1")
	if err := w.expire(w.trustA, "ten_a", "cfr_a1"); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range confirmationTables {
		if n := count(t, w.trustB, tbl); n != 0 {
			t.Errorf("tenant B sees %d %s rows of tenant A", n, tbl)
		}
		if n := count(t, w.trustA, tbl); n != 1 {
			t.Errorf("tenant A sees %d %s rows, want 1", n, tbl)
		}
	}
	// B writes for A: refused (the policy, the tenant guard, or the request B cannot see)
	wantState(t, w.request(t, w.trustB, "ten_a", "cfr_x1", "", cfBinding("tgt_9"), "SELF", cfOpen), denied)
	wantRefused(t, w.answer(w.trustB, "ten_a", "cfe_x1", "cfr_a1", "prn_2", "ACCEPT"))
	wantRefused(t, w.expire(w.trustB, "ten_a", "cfr_a1"))
	// B answers or expires A's request under its own tenant: B cannot see it
	wantState(t, w.answer(w.trustB, "ten_b", "cfe_x2", "cfr_a1", "prn_2", "ACCEPT"), violation)
	wantState(t, w.expire(w.trustB, "ten_b", "cfr_a1"), violation)
	// B can use the same request ID for its own request: the key is per tenant
	if err := w.request(t, w.trustB, "ten_b", "cfr_a1", "", cfBinding("tgt_1"), "SELF", cfOpen); err != nil {
		t.Errorf("tenant B's own request with the same ID: %v", err)
	}
}

// A request is superseded by a NEW row, never updated; an answer and an expiry are insert-only; nobody may delete or truncate, not even the owner.
func TestConfirmationRowsAreInsertOnly(t *testing.T) {
	w := newCfWorld(t)
	if err := w.request(t, w.trustA, "ten_a", "cfr_a1", "", cfBinding("tgt_1"), "SELF", cfShort); err != nil {
		t.Fatal(err)
	}
	if err := w.answer(w.trustA, "ten_a", "cfe_a1", "cfr_a1", "prn_2", "DECLINE"); err != nil {
		t.Fatal(err)
	}
	w.waitExpired(t, "cfr_a1")
	if err := w.expire(w.trustA, "ten_a", "cfr_a1"); err != nil {
		t.Fatal(err)
	}
	stmts := map[string][]string{
		"confirmation_request": {`UPDATE confirmation_request SET expires_at = expires_at + interval '1 day'`, `UPDATE confirmation_request SET requirement = 'SELF'`, `DELETE FROM confirmation_request`, `TRUNCATE confirmation_request`},
		"confirmation_answer":  {`UPDATE confirmation_answer SET response = 'ACCEPT'`, `DELETE FROM confirmation_answer`, `TRUNCATE confirmation_answer`},
		"confirmation_expiry":  {`UPDATE confirmation_expiry SET recorded_at = recorded_at`, `DELETE FROM confirmation_expiry`, `TRUNCATE confirmation_expiry`},
	}
	for tbl, list := range stmts {
		for _, s := range list {
			for name, pool := range map[string]*pgxpool.Pool{"trust login": w.trustA, "administrator": w.admin} {
				_, err := pool.Exec(ctx(t), s)
				if err == nil {
					t.Errorf("%s: %q succeeded", name, s)
					continue
				}
				var pe *pgconn.PgError
				if !errors.As(err, &pe) || !(strings.HasPrefix(pe.Code, violation) || pe.Code == denied || pe.Code == "0A000") {
					t.Errorf("%s: %q: %v", name, s, err)
				}
			}
		}
		if n := count(t, w.admin, tbl); n != 1 {
			t.Errorf("%s has %d rows after the refused changes, want 1", tbl, n)
		}
	}
}

// One open request per (tenant, intent, capability, target); a fresh request supersedes, never updates; never a second request beside an open, unanswered, unexpired one. The expiry is the
// database's: the first request lives a few seconds and the test waits on the database clock.
func TestOneOpenRequestPerActionAndSupersessionByANewRow(t *testing.T) {
	w := newCfWorld(t)
	b := cfBinding("tgt_1")
	if err := w.request(t, w.trustA, "ten_a", "cfr_r1", "", b, "SELF", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	// a second root for the same action
	wantState(t, w.request(t, w.trustA, "ten_a", "cfr_r2", "", b, "SELF", cfOpen), violation)
	// a fresh request beside an open one: unanswered and not expired on the database clock
	b2 := b
	b2.PlanHash = "sha256:l2"
	wantState(t, w.request(t, w.trustA, "ten_a", "cfr_r2", "cfr_r1", b2, "SELF", cfOpen), violation)
	// a superseded request that does not exist, or one of another action
	wantState(t, w.request(t, w.trustA, "ten_a", "cfr_r2", "cfr_nope", b2, "SELF", cfOpen), violation)
	for name, mutate := range map[string]func(*cfBound){
		"capability": func(o *cfBound) { o.CapabilityID = "cap.other" }, "target": func(o *cfBound) { o.TargetRef = "tgt_2" }, "principal": func(o *cfBound) { o.PrincipalID = "prn_9" },
	} {
		other := b2
		mutate(&other)
		if err := w.request(t, w.trustA, "ten_a", "cfr_r2", "cfr_r1", other, "SELF", cfOpen); err == nil {
			t.Errorf("a request of another %s superseded cfr_r1", name)
		}
	}
	if n := count(t, w.admin, "confirmation_request"); n != 1 {
		t.Fatalf("%d requests after the refusals, want 1", n)
	}
	// after an answer (an ACCEPT whose binding changed): a fresh request supersedes even before the expiry
	if err := w.answer(w.trustA, "ten_a", "cfe_n1", "cfr_r1", "prn_2", "ACCEPT"); err != nil {
		t.Fatal(err)
	}
	if err := w.request(t, w.trustA, "ten_a", "cfr_r2", "cfr_r1", b2, "SELF", 3*time.Second); err != nil {
		t.Fatalf("a request after an answer: %v", err)
	}
	// a request is superseded once
	b3 := b
	b3.PlanHash = "sha256:l3"
	wantState(t, w.request(t, w.trustA, "ten_a", "cfr_r3", "cfr_r1", b3, "SELF", cfOpen), violation)
	// after the expiry on the database clock: a fresh request supersedes an unanswered one
	w.waitExpired(t, "cfr_r2")
	if err := w.request(t, w.trustA, "ten_a", "cfr_r3", "cfr_r2", b3, "SELF", cfOpen); err != nil {
		t.Fatalf("a request after the expiry: %v", err)
	}
	// another action of the same intent has its own root
	if err := w.request(t, w.trustA, "ten_a", "cfr_t2", "", cfBinding("tgt_2"), "SELF", cfOpen); err != nil {
		t.Fatalf("a request for another target: %v", err)
	}
	var heads int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM confirmation_request r WHERE NOT EXISTS (SELECT 1 FROM confirmation_request s WHERE s.tenant_id = r.tenant_id
		AND s.supersedes_request_id = r.confirmation_request_id)`).Scan(&heads); err != nil || heads != 2 {
		t.Errorf("%d heads (%v), want one per action (2)", heads, err)
	}
}

// Two requests for one action made at once: one root commits.
func TestTwoConcurrentRootRequestsCommitOne(t *testing.T) {
	w := newCfWorld(t)
	errs := make(chan error, 2)
	for _, id := range []string{"cfr_c1", "cfr_c2"} {
		go func() { errs <- w.request(t, w.trustA, "ten_a", id, "", cfBinding("tgt_1"), "SELF", cfOpen) }()
	}
	var failed int
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			failed++
			wantState(t, err, violation)
		}
	}
	if failed != 1 || count(t, w.admin, "confirmation_request") != 1 {
		t.Fatalf("%d refused, %d rows: want one request", failed, count(t, w.admin, "confirmation_request"))
	}
}

// One answer per request (A341: first answer wins); an answer is ACCEPT or DECLINE; the event ID is cfe_...; an answer names an existing request.
func TestOneAnswerPerRequestWhoeverTheApprover(t *testing.T) {
	w := newCfWorld(t)
	if err := w.request(t, w.trustA, "ten_a", "cfr_a1", "", cfBinding("tgt_1"), "MAKER_CHECKER", cfOpen); err != nil {
		t.Fatal(err)
	}
	if err := w.answer(w.trustA, "ten_a", "cfe_n1", "cfr_a1", "prn_2", "ACCEPT"); err != nil {
		t.Fatal(err)
	}
	wantState(t, w.answer(w.trustA, "ten_a", "cfe_n2", "cfr_a1", "prn_2", "DECLINE"), violation) // the same approver again
	wantState(t, w.answer(w.trustA, "ten_a", "cfe_n1", "cfr_a1", "prn_3", "ACCEPT"), violation)  // the same event ID
	// A341: the FIRST answer wins, so a second answer by ANOTHER approver is refused too (UNIQUE (tenant_id, confirmation_request_id))
	wantState(t, w.answer(w.trustA, "ten_a", "cfe_n3", "cfr_a1", "prn_3", "DECLINE"), violation)
	wantState(t, w.answer(w.trustA, "ten_a", "cfe_n4", "cfr_a1", "prn_4", "MAYBE"), violation)
	wantState(t, w.answer(w.trustA, "ten_a", "evt_5", "cfr_a1", "prn_4", "ACCEPT"), violation)
	wantState(t, w.answer(w.trustA, "ten_a", "cfe_n6", "cfr_zzz", "prn_4", "ACCEPT"), violation)
	if n := count(t, w.admin, "confirmation_answer"); n != 1 {
		t.Errorf("%d answers, want 1", n)
	}
}

// The expiry is recorded once, and only at or after the request's expiry on the DATABASE clock.
func TestAnExpiryIsRecordedOnceAndNotBeforeTheRequestExpires(t *testing.T) {
	w := newCfWorld(t)
	if err := w.request(t, w.trustA, "ten_a", "cfr_a1", "", cfBinding("tgt_1"), "SELF", cfShort); err != nil {
		t.Fatal(err)
	}
	wantState(t, w.expire(w.trustA, "ten_a", "cfr_a1"), violation) // not yet
	wantState(t, w.expire(w.trustA, "ten_a", "cfr_missing"), violation)
	// a caller cannot expire a request early by claiming a later time (A368; SR requirement 2, C2 and Voice P3)
	_, err := w.trustA.Exec(ctx(t), `INSERT INTO confirmation_expiry (tenant_id, confirmation_request_id, observed_at) VALUES ('ten_a', 'cfr_a1', now() + interval '1 day')`)
	wantState(t, err, violation)
	w.waitExpired(t, "cfr_a1")
	if err := w.expire(w.trustA, "ten_a", "cfr_a1"); err != nil {
		t.Fatalf("at the expiry: %v", err)
	}
	wantState(t, w.expire(w.trustA, "ten_a", "cfr_a1"), violation)
}

// A368 (ii): requested_at, answered_at and observed_at are the DATABASE's now(), whatever the caller supplies (SR requirement 2: a skewed or hostile clock changes nothing).
func TestConfirmationTimesComeFromTheDatabaseClock(t *testing.T) {
	w := newCfWorld(t)
	b := cfBinding("tgt_1")
	_, err := w.trustA.Exec(ctx(t), `INSERT INTO confirmation_request (tenant_id, confirmation_request_id, correlation_id, intent_id, intent_version, principal_id, intent_hash, capability_id,
		target_ref, params_hash, evidence_set_hash, plan_hash, policy_version, binding_hash, requirement, requester_id, requested_at, expires_at, recorded_at)
		VALUES ('ten_a', 'cfr_t1', 'cor_1', 'int_1', 1, $1, $2, $3, $4, $5, $6, $7, $8, $9, 'SELF', 'prn_1', '2000-01-01T00:00:00Z', now() + interval '2 seconds', '2100-01-01T00:00:00Z')`,
		b.PrincipalID, b.IntentHash, b.CapabilityID, b.TargetRef, b.ParamsHash, b.EvidenceSetHash, b.PlanHash, b.PolicyVersion, cfHash(b))
	if err != nil {
		t.Fatalf("a request with supplied times: %v", err)
	}
	_, err = w.trustA.Exec(ctx(t), `INSERT INTO confirmation_answer (tenant_id, confirmation_event_id, confirmation_request_id, response, approver_id, approver_roles, answered_at, recorded_at)
		VALUES ('ten_a', 'cfe_t1', 'cfr_t1', 'ACCEPT', 'prn_2', ARRAY['reviewer'], '2000-01-01T00:00:00Z', '2000-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("an answer with a supplied time: %v", err)
	}
	w.waitExpired(t, "cfr_t1")
	_, err = w.trustA.Exec(ctx(t), `INSERT INTO confirmation_expiry (tenant_id, confirmation_request_id, observed_at, recorded_at) VALUES ('ten_a', 'cfr_t1', '2100-01-01T00:00:00Z', '2000-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("an expiry with a supplied time: %v", err)
	}
	for _, q := range []string{
		`SELECT abs(extract(epoch FROM now() - requested_at)) < 60 AND abs(extract(epoch FROM now() - recorded_at)) < 60 FROM confirmation_request`,
		`SELECT abs(extract(epoch FROM now() - answered_at)) < 60 AND abs(extract(epoch FROM now() - recorded_at)) < 60 FROM confirmation_answer`,
		`SELECT abs(extract(epoch FROM now() - observed_at)) < 60 AND abs(extract(epoch FROM now() - recorded_at)) < 60 FROM confirmation_expiry`,
	} {
		var ok bool
		if err := w.admin.QueryRow(ctx(t), q).Scan(&ok); err != nil || !ok {
			t.Errorf("%s: a supplied time was stored (ok=%v err=%v), want the database's now()", q, ok, err)
		}
	}
}

// A368 (iii), SR requirement 3: the intent version is at least 1.
func TestAnIntentVersionBelowOneIsRefused(t *testing.T) {
	w := newCfWorld(t)
	b := cfBinding("tgt_1")
	for _, v := range []int{0, -1} {
		_, err := w.trustA.Exec(ctx(t), `INSERT INTO confirmation_request (tenant_id, confirmation_request_id, correlation_id, intent_id, intent_version, principal_id, intent_hash, capability_id,
			target_ref, params_hash, evidence_set_hash, plan_hash, policy_version, binding_hash, requirement, requester_id, expires_at)
			VALUES ('ten_a', 'cfr_v1', 'cor_1', 'int_1', $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'SELF', 'prn_1', now() + interval '1 hour')`,
			v, b.PrincipalID, b.IntentHash, b.CapabilityID, b.TargetRef, b.ParamsHash, b.EvidenceSetHash, b.PlanHash, b.PolicyVersion, cfHash(b))
		wantState(t, err, violation)
	}
	if n := count(t, w.admin, "confirmation_request"); n != 0 {
		t.Fatalf("%d requests stored with a version below 1", n)
	}
}

// A368 (iv), C2 P2: an answer AFTER the request expired is refused by the database on its own clock (SQLSTATE 23E01), and so is an answer to a request that has a successor (23S01); the
// supersession is decided first. A replay of the stored answer is not an answer and is left to the unique key.
func TestAnAnswerAfterTheExpiryOrTheSupersessionIsRefused(t *testing.T) {
	w := newCfWorld(t)
	b := cfBinding("tgt_1")
	if err := w.request(t, w.trustA, "ten_a", "cfr_e1", "", b, "SELF", cfShort); err != nil {
		t.Fatal(err)
	}
	if err := w.request(t, w.trustA, "ten_a", "cfr_e2", "", cfBinding("tgt_2"), "SELF", cfShort); err != nil {
		t.Fatal(err)
	}
	if err := w.answer(w.trustA, "ten_a", "cfe_ok", "cfr_e2", "prn_2", "ACCEPT"); err != nil {
		t.Fatalf("an answer before the expiry: %v", err)
	}
	w.waitExpired(t, "cfr_e1")
	wantSQLState(t, w.answer(w.trustA, "ten_a", "cfe_late", "cfr_e1", "prn_2", "ACCEPT"), "23E01")
	// the same answer after the expiry reaches the unique key, not the expiry rule, when the request already has an answer
	wantSQLState(t, w.answer(w.trustA, "ten_a", "cfe_dup", "cfr_e2", "prn_2", "ACCEPT"), "23505")
	b2 := b
	b2.PlanHash = "sha256:l2"
	if err := w.request(t, w.trustA, "ten_a", "cfr_e3", "cfr_e1", b2, "SELF", cfOpen); err != nil {
		t.Fatalf("a request after the expiry: %v", err)
	}
	wantSQLState(t, w.answer(w.trustA, "ten_a", "cfe_late2", "cfr_e1", "prn_2", "ACCEPT"), "23S01")
	wantSQLState(t, w.answer(w.trustA, "ten_a", "cfe_late3", "cfr_e1", "prn_2", "DECLINE"), "23S01")
	if n := count(t, w.admin, "confirmation_answer"); n != 1 {
		t.Errorf("%d answers, want only the one made before the expiry", n)
	}
	// the live head still takes its answer
	if err := w.answer(w.trustA, "ten_a", "cfe_head", "cfr_e3", "prn_2", "ACCEPT"); err != nil {
		t.Fatalf("an answer to the live head: %v", err)
	}
}

// A368 (iv), race: an answer and a superseding request at once on a request that has just expired never leave an answer on a superseded request.
func TestAnAnswerAndASupersedeAtOnceNeverLeaveAnAnswerOnASupersededRequest(t *testing.T) {
	w := newCfWorld(t)
	b := cfBinding("tgt_1")
	if err := w.request(t, w.trustA, "ten_a", "cfr_k1", "", b, "SELF", cfShort); err != nil {
		t.Fatal(err)
	}
	w.waitExpired(t, "cfr_k1")
	b2 := b
	b2.PlanHash = "sha256:l2"
	errs := make(chan error, 2)
	go func() { errs <- w.request(t, w.trustA, "ten_a", "cfr_k2", "cfr_k1", b2, "SELF", cfOpen) }()
	go func() { errs <- w.answer(w.trustA, "ten_a", "cfe_k1", "cfr_k1", "prn_2", "ACCEPT") }()
	var ok int
	for i := 0; i < 2; i++ {
		if err := <-errs; err == nil {
			ok++
		}
	}
	var stale int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM confirmation_answer a JOIN confirmation_request s ON s.tenant_id = a.tenant_id AND s.supersedes_request_id = a.confirmation_request_id`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 || ok != 1 || count(t, w.admin, "confirmation_answer") != 0 {
		t.Fatalf("%d answers on a superseded request, %d writes succeeded, %d answers: want none, exactly the supersede, none", stale, ok, count(t, w.admin, "confirmation_answer"))
	}
}
