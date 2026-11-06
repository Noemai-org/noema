package store_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

const (
	tenant   = "ten_aa"
	other    = "ten_bb"
	grantID  = "grt_0001"
	receipt1 = "rcp_0001"
	corr     = "corr_abc123"
	defn     = "fin.outcome.exception.case_opened"
)

var verifiedAt = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

// world is a migrated database with one tenant, one spent grant and one receipt (SYNTHETIC rows, planted as the
// migrating role would see them). admin is the superuser; Outcome gets its own pool as outcome_rw.
type world struct {
	admin *pgxpool.Pool
	out   *pgxpool.Pool // Outcome's tenant login, made on first use
	dsn   string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	w := &world{admin: admin, dsn: dsn}
	w.seed(t, tenant, grantID, receipt1)
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Other')`, other); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) seed(t *testing.T, tenantID, grant, receipt string) {
	w.seedAt(t, tenantID, grant, receipt, verifiedAt)
}

// seedAt plants a tenant, a spent grant and its receipt dispatched at the given instant.
func (w *world) seedAt(t *testing.T, tenantID, grant, receipt string, dispatched time.Time) {
	t.Helper()
	type stmt struct {
		q    string
		args []any
	}
	for _, s := range []stmt{
		{`INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T') ON CONFLICT DO NOTHING`, []any{tenantID}},
		{`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value) VALUES ('dec_1', $1, jsonb_build_object('decision_id', 'dec_1', 'tenant_id', $1::text, 'outcome', 'ALLOW', 'sig', jsonb_build_object('key_id', 'k', 'value', 's')), 'ALLOW', 'sha256:' || repeat('0', 64), '[]', 'k', 's') ON CONFLICT (decision_id) DO NOTHING`, []any{tenantID}},
		{`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		  VALUES ($1, $2, 'dec_1', 'n_' || $1, jsonb_build_object('correlation_id', 'corr_abc123', 'principal_id', 'usr_100', 'decision_id', 'dec_1', 'tenant_id', $2::text), 'p', '1', now() + interval '1 hour', 'k', 's')`, []any{grant, tenantID}},
		{`INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ($1, $2, 'n_' || $1, 'idk_v1_' || $1, 1, now())`, []any{grant, tenantID}},
		{`INSERT INTO execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state, raw_status, dispatched_at, json, sig_key_id, sig_value, created_at)
		  VALUES ($1, $2, $3, 1, 'idk_v1_' || $3, 'ESCALATE_CASE', 'OUTCOME_UNKNOWN', 'TIMEOUT', $4, '{}', 'k', 's', now())`, []any{receipt, tenantID, grant, dispatched}},
	} {
		if _, err := w.admin.Exec(ctx(t), s.q, s.args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, s.q)
		}
	}
}

func (w *world) as(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	p, err := db.OpenAsRole(ctx(t), w.dsn, role)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

var logins atomic.Int64

// loginWith creates a LOGIN role that is a member of the given per-power roles and returns a pool that connects as that
// login and then runs as `role` (SET ROLE), as a deployment maps a login to a power. Roles are cluster-wide, so the name is
// unique to the process and the call.
func (w *world) loginWith(t *testing.T, role string, member ...string) *pgxpool.Pool {
	t.Helper()
	name := fmt.Sprintf("outcome_test_login_%d_%d", os.Getpid(), logins.Add(1))
	const password = "synthetic-test-password" // SYNTHETIC
	if _, err := w.admin.Exec(ctx(t), `CREATE ROLE `+name+` LOGIN NOSUPERUSER PASSWORD '`+password+`'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = w.admin.Exec(context.Background(), `DROP ROLE IF EXISTS `+name) })
	for _, m := range member {
		if _, err := w.admin.Exec(ctx(t), `GRANT `+m+` TO `+name); err != nil {
			t.Fatal(err)
		}
	}
	u, err := url.Parse(w.dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(name, password)
	p, err := db.OpenAsRole(ctx(t), u.String(), role)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// outcome is Outcome's own pool: the tenant's outcome login, which holds the outcome_rw power and no other and is mapped to the
// tenant, so the Record functions find the tenant from the login (DEBT-04 option A).
func (w *world) outcome(t *testing.T) *pgxpool.Pool {
	if w.out == nil { // one login per world: a role name is unique per tenant and database
		w.out = testpg.TenantLogin(t, w.admin, w.dsn, "outcome_rw", tenant)
	}
	return w.out
}

// verdictArgs are the arguments of outcome_record_verdict. verification, correlation and docVerdict default to values that
// agree with the receipt and the other columns; a test overrides one to show that the database refuses the disagreement.
type verdictArgs struct {
	tenant, verdictID, receiptID, verdict, reason string
	verification, correlation, docVerdict         string
	docOverride                                   string // JSON merged over the signed document, to make one field disagree with its column
	predicate                                     *bool
	reads                                         []string
	recoveryID, watchID                           *string
	next, deadline                                *time.Time
}

const callVerdict = `SELECT outcome_record_verdict($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'k_out', 'sig',
	(jsonb_build_object('verdict_id', $2::text, 'tenant_id', $1::text, 'receipt_id', $3::text, 'verdict', $16::text, 'correlation_id', $5::text, 'outcome_definition', $6::text,
		'evidence_reads', to_jsonb($10::text[]), 'predicate_result', COALESCE(to_jsonb($9::boolean), 'null'::jsonb), 'verified_at', to_jsonb($11::timestamptz),
		'sig', jsonb_build_object('key_id', 'k_out'::text, 'value', 'sig'::text))
		|| CASE WHEN $13::text IS NULL THEN '{}'::jsonb ELSE jsonb_build_object('watch', jsonb_build_object('watch_id', $13::text,
			'next_check_at', to_jsonb($14::timestamptz), 'deadline_at', to_jsonb($15::timestamptz), 'triggers', to_jsonb(ARRAY['fin.evt.settlement.updated']))) END
		|| $17::jsonb),
	$12, $13, $14, $15, ARRAY['fin.evt.settlement.updated'])`

func (a verdictArgs) call(c context.Context, p *pgxpool.Pool) error {
	verification, correlation, docVerdict := a.verification, a.correlation, a.docVerdict
	if verification == "" {
		verification = "vrf_" + a.verdictID
	}
	if correlation == "" {
		correlation = corr
	}
	if docVerdict == "" {
		docVerdict = a.verdict
	}
	override := a.docOverride
	if override == "" {
		override = "{}"
	}
	_, err := p.Exec(c, callVerdict, a.tenant, a.verdictID, a.receiptID, verification, correlation, defn, a.verdict, a.reason, a.predicate, a.reads, verifiedAt,
		a.recoveryID, a.watchID, a.next, a.deadline, docVerdict, override)
	return err
}

func ptr[T any](v T) *T { return &v }

func achieved(id string) verdictArgs {
	return verdictArgs{tenant: tenant, verdictID: id, receiptID: receipt1, verdict: "ACHIEVED", reason: "PREDICATE_TRUE", predicate: ptr(true), reads: []string{"evr_01"}}
}

func notAchieved(id, rrq string) verdictArgs {
	return verdictArgs{tenant: tenant, verdictID: id, receiptID: receipt1, verdict: "NOT_ACHIEVED", reason: "VERIFIED_ABSENT", predicate: ptr(false), reads: []string{"evr_01"}, recoveryID: &rrq}
}

func sqlState(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func (w *world) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(t), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
