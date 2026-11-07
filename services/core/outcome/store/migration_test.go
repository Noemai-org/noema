package store_test

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

const (
	privilegeDenied = "42501" // insufficient_privilege
	integrityClass  = "23"    // integrity_constraint_violation family
)

func wantState(t *testing.T, err error, prefix string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with SQLSTATE %s*, got none", prefix)
	}
	if s := sqlState(err); len(s) < len(prefix) || s[:len(prefix)] != prefix {
		t.Fatalf("expected SQLSTATE %s*, got %q: %v", prefix, s, err)
	}
}

// REQ-VER-003, INV-04: the write path stores an ACHIEVED verdict without a RecoveryRequest, and a NOT_ACHIEVED verdict
// with exactly one, addressed by verdict_id and never by grant_id.
func TestWritePathStoresAchievedAlone(t *testing.T) {
	w := newWorld(t)
	if err := achieved("ver_a1").call(ctx(t), w.outcome(t)); err != nil {
		t.Fatal(err)
	}
	if n := w.count(t, `SELECT count(*) FROM outcome_verdict WHERE verdict_id = 'ver_a1'`); n != 1 {
		t.Fatalf("verdicts = %d", n)
	}
	if n := w.count(t, `SELECT count(*) FROM recovery_request WHERE verdict_id = 'ver_a1'`); n != 0 {
		t.Fatalf("an ACHIEVED verdict raised %d recovery requests", n)
	}
}

func TestWritePathPairsAnyOtherVerdictWithOneRecoveryRequest(t *testing.T) {
	w := newWorld(t)
	if err := notAchieved("ver_n1", "rrq_n1").call(ctx(t), w.outcome(t)); err != nil {
		t.Fatal(err)
	}
	var grant *string
	var attempts int64
	if err := w.admin.QueryRow(ctx(t), `SELECT grant_id, attempt_count FROM recovery_request WHERE verdict_id = 'ver_n1'`).Scan(&grant, &attempts); err != nil {
		t.Fatal(err)
	}
	if grant != nil {
		t.Fatalf("a verdict-sourced recovery request carries grant_id %q", *grant)
	}
	if attempts != 1 {
		t.Fatalf("attempt_count = %d, want the receipt's attempt (1)", attempts)
	}
}

func TestWritePathRefusesAVerdictThatBreaksItsShape(t *testing.T) {
	w := newWorld(t)
	p := w.outcome(t)
	for name, a := range map[string]verdictArgs{
		"NOT_ACHIEVED without a recovery request": func() verdictArgs { a := notAchieved("ver_x1", "rrq_x1"); a.recoveryID = nil; return a }(),
		"ACHIEVED with a recovery request":        func() verdictArgs { a := achieved("ver_x2"); a.recoveryID = ptr("rrq_x2"); return a }(),
		"ACHIEVED with a false predicate":         func() verdictArgs { a := achieved("ver_x3"); a.predicate = ptr(false); return a }(),
		"ACHIEVED with a null predicate":          func() verdictArgs { a := achieved("ver_x4"); a.predicate = nil; return a }(),
		"ACHIEVED with no evidence read":          func() verdictArgs { a := achieved("ver_x5"); a.reads = []string{}; return a }(),
		"NOT_ACHIEVED with a true predicate":      func() verdictArgs { a := notAchieved("ver_x6", "rrq_x6"); a.predicate = ptr(true); return a }(),
		"PENDING_EXTERNAL without a watch": {tenant: tenant, verdictID: "ver_x7", receiptID: receipt1, verdict: "PENDING_EXTERNAL", reason: "PENDING_CONDITION",
			predicate: ptr(false), reads: []string{"evr_01"}, recoveryID: ptr("rrq_x7")},
		"a watch on an UNKNOWN verdict": {tenant: tenant, verdictID: "ver_x8", receiptID: receipt1, verdict: "UNKNOWN", reason: "READS_EXHAUSTED",
			reads: []string{}, recoveryID: ptr("rrq_x8"), watchID: ptr("wat_x8"), next: ptr(verifiedAt.Add(time.Minute)), deadline: ptr(verifiedAt.Add(time.Hour))},
		"an unknown verdict value": {tenant: tenant, verdictID: "ver_x9", receiptID: receipt1, verdict: "DONE", reason: "X", reads: []string{}, recoveryID: ptr("rrq_x9")},
		"an empty reason":          func() verdictArgs { a := notAchieved("ver_xa", "rrq_xa"); a.reason = ""; return a }(),
		"a foreign tenant's receipt": func() verdictArgs {
			a := notAchieved("ver_xb", "rrq_xb")
			a.tenant = other
			return a
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := a.call(ctx(t), p); err == nil {
				t.Fatal("the write path accepted it")
			}
		})
	}
	if n := w.count(t, `SELECT count(*) FROM outcome_verdict`) + w.count(t, `SELECT count(*) FROM recovery_request WHERE verdict_id IS NOT NULL`); n != 0 {
		t.Fatalf("%d rows were left behind by refused calls", n)
	}
}

func TestWritePathStoresAPendingVerdictWithItsWatch(t *testing.T) {
	w := newWorld(t)
	a := verdictArgs{tenant: tenant, verdictID: "ver_p1", receiptID: receipt1, verdict: "PENDING_EXTERNAL", reason: "PENDING_CONDITION", predicate: ptr(false),
		reads: []string{"evr_01"}, recoveryID: ptr("rrq_p1"), watchID: ptr("wat_p1"), next: ptr(verifiedAt.Add(5 * time.Minute)), deadline: ptr(verifiedAt.Add(72 * time.Hour))}
	if err := a.call(ctx(t), w.outcome(t)); err != nil {
		t.Fatal(err)
	}
	if n := w.count(t, `SELECT count(*) FROM outcome_watch WHERE verdict_id = 'ver_p1' AND status = 'OPEN' AND next_check_at <= deadline_at`); n != 1 {
		t.Fatalf("open watches = %d", n)
	}
	a.verdictID, a.recoveryID, a.watchID, a.next, a.deadline = "ver_p2", ptr("rrq_p2"), ptr("wat_p2"), ptr(verifiedAt.Add(2*time.Hour)), ptr(verifiedAt.Add(time.Hour))
	if err := a.call(ctx(t), w.outcome(t)); err == nil {
		t.Fatal("a watch whose first check is after its deadline was accepted")
	}
}

// INV-04, BD-05, Q1: the function is the only write path and only outcome_rw may call it.
func TestOnlyOutcomeRwMayCallTheWritePath(t *testing.T) {
	w := newWorld(t)
	for _, role := range []string{"trust_rw", "execution_rw", "evidence_rw", "orchestrator_rw", "record_append"} {
		t.Run(role, func(t *testing.T) {
			wantState(t, achieved("ver_r_"+role).call(ctx(t), w.as(t, role)), privilegeDenied)
		})
	}
	if n := w.count(t, `SELECT count(*) FROM outcome_verdict`); n != 0 {
		t.Fatalf("%d verdicts written by a role that may not write them", n)
	}
}

// Codex blocking finding, Founder's decision on DEBT-03: the function authenticates the calling login, not only the role the
// session runs as. Outcome's login holds the outcome_rw power and no other; a superuser, or a login that also holds another
// power, cannot write a verdict by running as outcome_rw. (Who holds the outcome_rw credential is a separate limit, REC-08.)
func TestWritePathAuthenticatesTheCallingLogin(t *testing.T) {
	w := newWorld(t)
	for name, p := range map[string]*pgxpool.Pool{
		"a superuser that runs as outcome_rw":     w.as(t, "outcome_rw"),
		"a login that also holds execution_rw":    w.loginWith(t, "outcome_rw", "outcome_rw", "execution_rw"),
		"a login that also holds trust_rw":        w.loginWith(t, "outcome_rw", "outcome_rw", "trust_rw"),
		"a login that also holds evidence_rw":     w.loginWith(t, "outcome_rw", "outcome_rw", "evidence_rw"),
		"a login that also holds orchestrator_rw": w.loginWith(t, "outcome_rw", "outcome_rw", "orchestrator_rw"),
	} {
		t.Run(name, func(t *testing.T) {
			wantState(t, achieved("ver_l_"+strings.ReplaceAll(name, " ", "_")).call(ctx(t), p), privilegeDenied)
		})
	}
	if n := w.count(t, `SELECT count(*) FROM outcome_verdict`); n != 0 {
		t.Fatalf("%d verdicts written by a login that is not Outcome's", n)
	}
	if err := achieved("ver_l_ok").call(ctx(t), w.outcome(t)); err != nil {
		t.Fatalf("Outcome's own login was refused: %v", err)
	}
}

func TestOutcomeRwHasNoDirectWrites(t *testing.T) {
	w := newWorld(t)
	if err := notAchieved("ver_d1", "rrq_d1").call(ctx(t), w.outcome(t)); err != nil {
		t.Fatal(err)
	}
	p := w.outcome(t)
	for _, q := range []string{
		`INSERT INTO outcome_verdict (verdict_id, tenant_id, correlation_id, receipt_id, verification_id, outcome_definition, verdict, reason, predicate_result, evidence_reads, verified_at, json, sig_key_id, sig_value)
		 VALUES ('ver_z', 'ten_aa', 'corr_abc123', 'rcp_0001', 'vrf_z', 'd', 'ACHIEVED', 'r', true, ARRAY['evr_1'], now(), '{}', 'k', 's')`,
		`UPDATE outcome_verdict SET reason = 'x'`,
		`DELETE FROM outcome_verdict`,
		`INSERT INTO outcome_watch (watch_id, tenant_id, verdict_id, next_check_at, deadline_at, triggers, created_at) VALUES ('wat_z', 'ten_aa', 'ver_d1', now(), now(), ARRAY[]::text[], now())`,
		`UPDATE outcome_watch SET status = 'CLOSED'`,
		`INSERT INTO recovery_request (recovery_request_id, tenant_id, verdict_id, reason, created_at) VALUES ('rrq_z', 'ten_aa', 'ver_d1', 'r', now())`,
		`UPDATE recovery_request SET status = 'DECIDED'`,
	} {
		_, err := p.Exec(ctx(t), q)
		wantState(t, err, privilegeDenied)
	}
}

// The recovery_request table also takes inserts from Execution (a void grant). Execution cannot raise a request that names
// a verdict: against an ACHIEVED verdict the guard refuses it, against a non-ACHIEVED one the unique constraint does (the
// request already exists), and against no verdict the foreign key does. A forged request would send a finished action
// back to Trust. The grant-sourced insert Execution already made keeps working.
func TestExecutionCannotRaiseAVerdictSourcedRecoveryRequest(t *testing.T) {
	w := newWorld(t)
	p := w.outcome(t)
	if err := achieved("ver_e1").call(ctx(t), p); err != nil {
		t.Fatal(err)
	}
	if err := notAchieved("ver_e2", "rrq_e2").call(ctx(t), p); err != nil {
		t.Fatal(err)
	}
	exec := testpg.TenantLogin(t, w.admin, w.dsn, "execution_rw", tenant) // the tenant's own login: row-level security (migration 0017) needs a mapped login
	for name, verdict := range map[string]string{"an ACHIEVED verdict": "ver_e1", "a verdict that has its request": "ver_e2", "a verdict that does not exist": "ver_none"} {
		_, err := exec.Exec(ctx(t), `INSERT INTO recovery_request (recovery_request_id, tenant_id, verdict_id, reason, created_at) VALUES ('rrq_f_' || $1, 'ten_aa', $1, 'r', now())`, verdict)
		if err == nil {
			t.Fatalf("Execution raised a recovery request against %s", name)
		}
		wantState(t, err, integrityClass)
	}
	if _, err := exec.Exec(ctx(t), `INSERT INTO recovery_request (recovery_request_id, tenant_id, grant_id, reason, created_at) VALUES ('rrq_g', 'ten_aa', 'grt_0001', 'r', now())`); err != nil {
		t.Fatalf("Execution lost the insert of a grant-sourced recovery request: %v", err)
	}
}

// REQ-VER-003 "Non-ACHIEVED verdicts without a RecoveryRequest SHALL be 0 (DB constraint)": the rule holds for every
// inserter, the migrating role included.
func TestConstraintsHoldForEveryInserter(t *testing.T) {
	w := newWorld(t)
	insert := func(id, verdict string, predicate string) string {
		return `INSERT INTO outcome_verdict (verdict_id, tenant_id, correlation_id, receipt_id, verification_id, outcome_definition, verdict, reason, predicate_result, evidence_reads, verified_at, json, sig_key_id, sig_value)
		 VALUES ('` + id + `', 'ten_aa', 'corr_abc123', 'rcp_0001', 'vrf_` + id + `', 'd', '` + verdict + `', 'r', ` + predicate + `, ARRAY['evr_1'], now(), '{}', 'k', 's')`
	}
	for name, q := range map[string]string{
		"NOT_ACHIEVED alone":     insert("ver_c1", "NOT_ACHIEVED", "false"),
		"UNKNOWN alone":          insert("ver_c2", "UNKNOWN", "NULL"),
		"PENDING_EXTERNAL alone": insert("ver_c3", "PENDING_EXTERNAL", "false"),
	} {
		t.Run(name, func(t *testing.T) {
			tx, err := w.admin.Begin(ctx(t))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx(t)) }()
			if _, err := tx.Exec(ctx(t), q); err != nil {
				t.Fatalf("the insert itself should be accepted until commit: %v", err)
			}
			wantState(t, tx.Commit(ctx(t)), integrityClass)
		})
	}
	tx, err := w.admin.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	_, err = tx.Exec(ctx(t), `INSERT INTO recovery_request (recovery_request_id, tenant_id, grant_id, verdict_id, reason, created_at) VALUES ('rrq_b', 'ten_aa', 'grt_0001', NULL, 'r', now()), ('rrq_c', 'ten_aa', NULL, NULL, 'r', now())`)
	wantState(t, err, integrityClass)
}

func TestARecoveryRequestMustNotPointAtAnAchievedVerdict(t *testing.T) {
	w := newWorld(t)
	if err := achieved("ver_k1").call(ctx(t), w.outcome(t)); err != nil {
		t.Fatal(err)
	}
	_, err := w.admin.Exec(ctx(t), `INSERT INTO recovery_request (recovery_request_id, tenant_id, verdict_id, reason, created_at) VALUES ('rrq_k', 'ten_aa', 'ver_k1', 'r', now())`)
	wantState(t, err, integrityClass)
}

func TestVerdictsAreInsertOnly(t *testing.T) {
	w := newWorld(t)
	if err := achieved("ver_i1").call(ctx(t), w.outcome(t)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE outcome_verdict SET reason = 'x'`, `DELETE FROM outcome_verdict`, `TRUNCATE outcome_verdict CASCADE`} {
		_, err := w.admin.Exec(ctx(t), q)
		wantState(t, err, integrityClass)
	}
}

// Q2: migrations apply by name and a gap is allowed. 0007 on a fresh database, and 0002 (noema#94, pinned) applied
// afterwards on a database that already has 0007, leave the same working write path.
func TestUpgradeWhere0002ArrivesAfter0007(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/testdata/0002_grant_spent_void.noema94.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != "faafb174516bd197acc8935d8b7c8fcfc7f587f6b730096ccf9273bfd4a32626" {
		t.Fatalf("the pinned noema#94 migration changed: %s", got)
	}
	w := newWorld(t) // 0001 and 0007 only
	if err := achieved("ver_u1").call(ctx(t), w.outcome(t)); err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"0002_grant_spent_void.sql": {Data: raw}}
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := migrations.FS.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = &fstest.MapFile{Data: b}
	}
	applied, err := db.Migrate(ctx(t), w.admin, files)
	if err != nil {
		t.Fatalf("0002 after 0007: %v", err)
	}
	if len(applied) != 1 || applied[0] != "0002_grant_spent_void.sql" {
		t.Fatalf("applied = %v, want only 0002", applied)
	}
	if err := notAchieved("ver_u2", "rrq_u2").call(ctx(t), w.outcome(t)); err != nil {
		t.Fatalf("the write path after 0002: %v", err)
	}
}

// PERD 32.4, Q1: the function stores only a verdict that agrees with the receipt it names and with its own document. A
// stolen credential still cannot make the columns say one thing and the signed document another, or file a verdict under
// another grant's correlation.
func TestWritePathRefusesAVerdictThatDisagreesWithItsReceiptOrItsDocument(t *testing.T) {
	w := newWorld(t)
	p := w.outcome(t)
	wrongCorrelation := achieved("ver_c1")
	wrongCorrelation.correlation = "corr_other99"
	disagreeing := achieved("ver_c2")
	disagreeing.docVerdict = "NOT_ACHIEVED"
	field := func(id, override string) verdictArgs { a := achieved(id); a.docOverride = override; return a }
	for name, a := range map[string]verdictArgs{
		"another grant's correlation":            wrongCorrelation,
		"a document that says another verdict":   disagreeing,
		"a document with other evidence reads":   field("ver_c3", `{"evidence_reads":["evr_other"]}`),
		"a document with another predicate":      field("ver_c4", `{"predicate_result":false}`),
		"a document verified at another instant": field("ver_c5", `{"verified_at":"2026-11-03T10:00:00Z"}`),
		"a document signed with another key id":  field("ver_c6", `{"sig":{"key_id":"k_other","value":"sig"}}`),
		"a document with another signature":      field("ver_c7", `{"sig":{"key_id":"k_out","value":"other"}}`),
	} {
		t.Run(name, func(t *testing.T) { wantState(t, a.call(ctx(t), p), integrityClass) })
	}
	if n := w.count(t, `SELECT count(*) FROM outcome_verdict`); n != 0 {
		t.Fatalf("%d verdicts left behind", n)
	}
}

// The durable watch is part of the signed verdict: the row OUT-03 schedules from must say what the signed document says
// (REQ-VER-004, INV-28), and only a PENDING_EXTERNAL document carries a watch.
func TestWritePathRefusesAWatchThatDisagreesWithItsDocument(t *testing.T) {
	w := newWorld(t)
	p := w.outcome(t)
	pending := func(id, override string) verdictArgs {
		return verdictArgs{tenant: tenant, verdictID: id, receiptID: receipt1, verdict: "PENDING_EXTERNAL", reason: "PENDING_CONDITION", predicate: ptr(false),
			reads: []string{"evr_01"}, recoveryID: ptr("rrq_" + id), watchID: ptr("wat_" + id), next: ptr(verifiedAt.Add(5 * time.Minute)),
			deadline: ptr(verifiedAt.Add(72 * time.Hour)), docOverride: override}
	}
	withWatch := achieved("ver_w6")
	withWatch.docOverride = `{"watch":{"watch_id":"wat_x","next_check_at":"2026-11-03T10:05:00Z","deadline_at":"2026-11-06T10:00:00Z","triggers":[]}}`
	for name, a := range map[string]verdictArgs{
		"a document with another watch id":               pending("ver_w1", `{"watch":{"watch_id":"wat_other","next_check_at":"2026-11-03T10:05:00Z","deadline_at":"2026-11-06T10:00:00Z","triggers":["fin.evt.settlement.updated"]}}`),
		"a document with another next check":             pending("ver_w2", `{"watch":{"watch_id":"wat_ver_w2","next_check_at":"2026-11-03T10:06:00Z","deadline_at":"2026-11-06T10:00:00Z","triggers":["fin.evt.settlement.updated"]}}`),
		"a document with another deadline":               pending("ver_w3", `{"watch":{"watch_id":"wat_ver_w3","next_check_at":"2026-11-03T10:05:00Z","deadline_at":"2027-01-01T00:00:00Z","triggers":["fin.evt.settlement.updated"]}}`),
		"a document with other triggers":                 pending("ver_w4", `{"watch":{"watch_id":"wat_ver_w4","next_check_at":"2026-11-03T10:05:00Z","deadline_at":"2026-11-06T10:00:00Z","triggers":[]}}`),
		"a pending document with no watch":               pending("ver_w5", `{"watch":null}`),
		"a document that is not pending but has a watch": withWatch,
	} {
		t.Run(name, func(t *testing.T) { wantState(t, a.call(ctx(t), p), integrityClass) })
	}
	if n := w.count(t, `SELECT count(*) FROM outcome_verdict`) + w.count(t, `SELECT count(*) FROM outcome_watch`); n != 0 {
		t.Fatalf("%d rows left behind", n)
	}
}

// A verification is stored once. A second verdict under the same verification ID is refused by the database, whoever calls.
func TestAVerificationIsStoredOnce(t *testing.T) {
	w := newWorld(t)
	p := w.outcome(t)
	first := achieved("ver_v1")
	first.verification = "vrf_same"
	if err := first.call(ctx(t), p); err != nil {
		t.Fatal(err)
	}
	second := notAchieved("ver_v2", "rrq_v2")
	second.verification = "vrf_same"
	wantState(t, second.call(ctx(t), p), "23505")
	if n := w.count(t, `SELECT count(*) FROM outcome_verdict`) + w.count(t, `SELECT count(*) FROM recovery_request WHERE verdict_id IS NOT NULL`); n != 1 {
		t.Fatalf("rows after a refused second verdict = %d, want just the first verdict", n)
	}
}
