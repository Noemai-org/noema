package main

// STM-05, the recovery write path (A696, A675, A692, interim, relayed; REQ-VER-004, REQ-VER-001, REQ-SEC-004; INV-03, INV-04, INV-07, INV-12, AGENT-08): what recovery needs to verify a grant
// the Record shows as spent, with no way to dispatch. The ports are the same Verifier (Outcome's, over the request reader of slice 4), Journal and Bindings as the live write path, and an
// Executor that always refuses. They are built from parts that are all present; a part that is missing is a named gap and builds no ports. These tests run on a real PostgreSQL with each
// tenant's own logins; the signature check, the pack's outcomes and the Outcome verdict are the seams other lanes own and are stood in by fixed answers. Every row is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/internal/sigfixture"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
	"github.com/Noemai-org/noema/services/core/outcome/request"
	outcomeverifier "github.com/Noemai-org/noema/services/core/outcome/verifier"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

const (
	rwTenantA = "ten_rwaa"
	rwTenantB = "ten_rwbb"
	rwGrant   = "grt_rw01"
	rwReceipt = "rcp_rw01"
	rwCap     = "fin.cap.create_escalation_case"
)

type rwVerifier struct {
	seen   []outcomeverifier.Request
	result outcomeverifier.Result
}

func (v *rwVerifier) Verify(_ context.Context, r outcomeverifier.Request) (outcomeverifier.Result, error) {
	v.seen = append(v.seen, r)
	return v.result, nil
}

type rwSeams struct{}

func (rwSeams) Check(context.Context, outcomeverifier.Receipt) (outcomeverifier.Authenticity, error) {
	return outcomeverifier.AuthValid, nil
}

func (rwSeams) For(context.Context, string, string) (request.Outcome, error) {
	return request.Outcome{
		Definition:      request.Definition{OutcomeID: "synthetic.outcome", SuccessPredicate: "exists(case where case.subject == subject_resource)", SettleWindowSeconds: 30},
		SubjectVariable: "subject_resource",
		Bindings:        map[string]request.Source{"subject_resource": {Kind: request.FromPrincipalID}, "intent_id": {Kind: request.FromIntentID}},
		Capability:      request.Capability{ID: rwCap, RetrySafety: "QUERYABLE_BY_KEY"},
	}, nil
}

func (rwSeams) ID(prefix string) (string, error) { return prefix + "_rw0001", nil }

type rwBindings struct{}

func (rwBindings) Bound(context.Context, orchestrator.Session, orchestrator.Binding) error {
	return nil
}

type rwRecords map[string]*pgxpool.Pool

func (r rwRecords) For(tenant string) (reconstruct.TxBeginner, error) {
	p, ok := r[tenant]
	if !ok {
		return nil, errors.New("synthetic: no Record login for the tenant")
	}
	return p, nil
}

type rwWorld struct {
	admin    *pgxpool.Pool
	parts    *executionParts
	records  rwRecords
	verifier *rwVerifier
}

func rwDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// newRWWorld makes two tenants, a spent grant with its signed receipt for tenant A, and each tenant's own outcome and Record-read login.
func newRWWorld(t *testing.T) *rwWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(rwDeadline(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	key := sigfixture.NewKey("recovery-write", 1)
	sigfixture.Register(t, admin, key)
	w := &rwWorld{admin: admin, parts: &executionParts{OutcomePools: map[string]*pgxpool.Pool{}}, records: rwRecords{}, verifier: &rwVerifier{result: outcomeverifier.Result{Verdict: outcomeverifier.Achieved}}}
	for _, tenant := range []string{rwTenantA, rwTenantB} {
		if _, err := admin.Exec(rwDeadline(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T')`, tenant); err != nil {
			t.Fatal(err)
		}
		w.parts.OutcomePools[tenant] = testpg.TenantLogin(t, admin, dsn, "outcome_rw", tenant)
		w.records[tenant] = testpg.TenantLogin(t, admin, dsn, "record_read", tenant)
	}
	for _, q := range []string{
		`INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value) VALUES ('dec_rw01', $1, jsonb_build_object('decision_id', 'dec_rw01', 'tenant_id', $1::text, 'outcome', 'ALLOW', 'sig', jsonb_build_object('key_id', 'k', 'value', 's')) || ` + testpg.BoundFieldsSQL + `, 'ALLOW', '` + testpg.FixtureIntentHash + `', '[]', 'k', 's')`,
		`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		  VALUES ('` + rwGrant + `', $1, 'dec_rw01', 'n_rw01', jsonb_build_object('correlation_id', 'corr_abc123', 'principal_id', 'usr_100', 'decision_id', 'dec_rw01', 'tenant_id', $1::text,
		  'capability_id', '` + rwCap + `', 'plan_hash', 'sha256:` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `') || ` + testpg.GrantBoundSQL + `, 'p', '1', now() + interval '1 hour', 'k', 's')`,
		`INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ('` + rwGrant + `', $1, 'n_rw01', 'idk_v1_rw01', 1, now())`,
	} {
		if _, err := admin.Exec(rwDeadline(t), q, rwTenantA); err != nil {
			t.Fatalf("seed: %v\n%s", err, q)
		}
	}
	sigfixture.InsertReceipt(t, admin, key, sigfixture.Receipt{ID: rwReceipt, Tenant: rwTenantA, Grant: rwGrant, IdempotencyKey: "idk_v1_rw01", LogicalAction: rwCap,
		State: "OUTCOME_UNKNOWN", Raw: "TIMEOUT", Attempt: 1, Dispatched: time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)})
	return w
}

func (w *rwWorld) seams() recoveryWriteSeams {
	st := rwSeams{}
	return recoveryWriteSeams{Verifier: w.verifier, Requests: outcomeRequestSeams{Signatures: st, Outcomes: st, IDs: st}, Bindings: rwBindings{}}
}

func (w *rwWorld) ports(t *testing.T) orchestrator.WritePorts {
	t.Helper()
	p, gaps := buildRecoveryWritePorts(w.records, w.parts, w.seams())
	if len(gaps) != 0 || p.Executor == nil || p.Verifier == nil || p.Journal == nil || p.Bindings == nil {
		t.Fatalf("ports %+v, gaps %v; want complete ports and no gap", p, gaps)
	}
	return p
}

func (w *rwWorld) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(rwDeadline(t), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func rwSession(tenant string) orchestrator.Session {
	return orchestrator.Session{TenantID: tenant, PrincipalID: "usr_100", Role: "customer"}
}

// REQ-VER-001, REQ-VER-004; INV-04: a grant the Record shows as spent is verified by key, through the request reader on the tenant's own outcome login: the verifier is asked for the
// receipt Execution wrote for that grant, and its verdict is the answer.
func TestRecoveryVerifiesASpentGrantByKeyOverTheRealReceipt(t *testing.T) {
	w := newRWWorld(t)
	got, err := w.ports(t).Verifier.Verify(rwDeadline(t), rwSession(rwTenantA), "dec_rw01", rwGrant)
	if err != nil || got != contracts.EnumsV1VerdictACHIEVED {
		t.Fatalf("verdict %q, err %v; want ACHIEVED", got, err)
	}
	if len(w.verifier.seen) != 1 || w.verifier.seen[0].ReceiptID != rwReceipt || w.verifier.seen[0].TenantID != rwTenantA || w.verifier.seen[0].VerificationID != "vrf_rw0001" {
		t.Errorf("the verifier saw %+v; want one request for %s of %s", w.verifier.seen, rwReceipt, rwTenantA)
	}
}

// INV-03, INV-07, AGENT-08: recovery cannot dispatch, and a verify writes nothing: the Executor refuses with recovery.ErrNoDispatch and no row of the Record, the grants' spends or the
// receipts changes.
func TestRecoveryCannotDispatchAndItsVerifyWritesNothing(t *testing.T) {
	w := newRWWorld(t)
	before := map[string]int{}
	tables := []string{"record_event", "grant_spend", "execution_receipt", "decision_grant"}
	for _, tb := range tables {
		before[tb] = w.count(t, tb)
	}
	p := w.ports(t)
	if _, err := p.Executor.Execute(rwDeadline(t), rwSession(rwTenantA), rwGrant); !errors.Is(err, recovery.ErrNoDispatch) {
		t.Errorf("Execute: %v; want recovery.ErrNoDispatch", err)
	}
	if _, err := p.Verifier.Verify(rwDeadline(t), rwSession(rwTenantA), "dec_rw01", rwGrant); err != nil {
		t.Fatal(err)
	}
	for _, tb := range tables {
		if after := w.count(t, tb); after != before[tb] {
			t.Errorf("%s changed from %d to %d rows", tb, before[tb], after)
		}
	}
}

// INV-12, REQ-SEC-004: the grant is read as the session's tenant only. Tenant B asking for tenant A's grant gets no request and the verifier is never asked; a tenant with no outcome login
// is refused before any read.
func TestRecoveryReadsTheSessionsTenantOnly(t *testing.T) {
	w := newRWWorld(t)
	p := w.ports(t)
	if _, err := p.Verifier.Verify(rwDeadline(t), rwSession(rwTenantB), "dec_rw01", rwGrant); !errors.Is(err, request.ErrNotFound) {
		t.Errorf("another tenant's grant: %v; want request.ErrNotFound", err)
	}
	if _, err := p.Verifier.Verify(rwDeadline(t), rwSession("ten_none"), "dec_rw01", rwGrant); !errors.Is(err, ErrNoOutcomeForTenant) {
		t.Errorf("a tenant with no login: %v; want ErrNoOutcomeForTenant", err)
	}
	if len(w.verifier.seen) != 0 {
		t.Errorf("the verifier was asked %d times for a refused call", len(w.verifier.seen))
	}
}

// REQ-RCV-003: the Journal is the Record's, read as the session's tenant: a grant with no GRANT_SPENT event on the tenant's chain is not spent, and a tenant with no Record login is an error.
func TestRecoveryTheJournalReadsTheTenantsOwnRecord(t *testing.T) {
	w := newRWWorld(t)
	p := w.ports(t)
	prog, err := p.Journal.Progress(rwDeadline(t), rwSession(rwTenantA), "inv_rw01", rwGrant)
	if err != nil || prog.GrantSpent || prog.DispatchAttempted {
		t.Errorf("progress %+v, err %v; want none spent on an empty Record", prog, err)
	}
	if _, err := p.Journal.Progress(rwDeadline(t), rwSession("ten_none"), "inv_rw01", rwGrant); err == nil {
		t.Error("a tenant with no Record login was read")
	}
}

// A part that is missing is a named gap and builds no ports: a write path that cannot do everything does nothing. The gaps are fixed texts that carry no value.
func TestAMissingPartIsANamedGapAndBuildsNoPorts(t *testing.T) {
	w := newRWWorld(t)
	for want, edit := range map[string]func(s *recoveryWriteSeams, r *reconstruct.TenantDBs, p **executionParts){
		"recovery write: record reader missing":    func(_ *recoveryWriteSeams, r *reconstruct.TenantDBs, _ **executionParts) { *r = nil },
		"recovery write: outcome verifier missing": func(s *recoveryWriteSeams, _ *reconstruct.TenantDBs, _ **executionParts) { s.Verifier = nil },
		"recovery write: bindings missing":         func(s *recoveryWriteSeams, _ *reconstruct.TenantDBs, _ **executionParts) { s.Bindings = nil },
		"outcome requests: signature check missing": func(s *recoveryWriteSeams, _ *reconstruct.TenantDBs, _ **executionParts) {
			s.Requests.Signatures = nil
		},
		"outcome requests: pack outcomes missing":    func(s *recoveryWriteSeams, _ *reconstruct.TenantDBs, _ **executionParts) { s.Requests.Outcomes = nil },
		"outcome requests: execution not configured": func(_ *recoveryWriteSeams, _ *reconstruct.TenantDBs, p **executionParts) { *p = nil },
	} {
		s, records, parts := w.seams(), reconstruct.TenantDBs(w.records), w.parts
		edit(&s, &records, &parts)
		p, gaps := buildRecoveryWritePorts(records, parts, s)
		if p != (orchestrator.WritePorts{}) || len(gaps) != 1 || gaps[0] != want {
			t.Errorf("%s: ports %+v, gaps %v", want, p, gaps)
		}
	}
}
