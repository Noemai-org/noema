package store_test

import (
	"context"
	"errors"
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
		{`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		  VALUES ($1, $2, 'dec_1', 'n_' || $1, '{"correlation_id":"corr_abc123","principal_id":"usr_100"}', 'p', '1', now() + interval '1 hour', 'k', 's')`, []any{grant, tenantID}},
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
		'sig', jsonb_build_object('key_id', 'k_out'::text, 'value', 'sig'::text)) || $17::jsonb),
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
