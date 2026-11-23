package trustpg_test

// STM-01 follow-up (wire_trust): Postgres-backed readers for the Trust adapter of noema#710. Real PostgreSQL, real tenant logins, the real Record. All data is SYNTHETIC
// (AGENT-25). These tests need pgsodium and run in CI only. Proves REQ-TRU-009, REQ-RCV-003, REQ-SEC-004; INV-03, INV-12.

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const (
	tenantA    = "ten_aa"
	tenantB    = "ten_bb"
	corr       = "corr_ABC123"
	intentID   = "int_01J9X3"
	capability = "fin.cap.create_escalation_case"
	target     = "fin.transaction:TX88213"
	principal  = "usr_88213"
	timeout    = 20 * time.Second
)

var epoch = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

type world struct {
	admin    *pgxpool.Pool
	trustA   *pgxpool.Pool
	trustB   *pgxpool.Pool
	execA    *pgxpool.Pool
	readerA  *pgxpool.Pool
	readerB  *pgxpool.Pool
	pools    trustpg.Pools
	writer   *recorder.Writer
	appender *chain.Appender
	allowed  recorder.Result // an ALLOW of tenant A on intent version 2, one grant
}

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

func hash(c byte) contracts.CommonV1Hash {
	s := "sha256:"
	for i := 0; i < 64; i++ {
		s += string("0123456789abcdef"[int(c)%16])
	}
	return contracts.CommonV1Hash(s)
}

func basis(t *testing.T) *decide.BasisBuilder {
	t.Helper()
	b, err := decide.NewBasisBuilder("fin.pack@1.0.0", epoch.Unix())
	if err != nil {
		t.Fatal(err)
	}
	f := decide.Freshness{EvidenceID: "ev_1", EffectiveAt: epoch.Unix() - 30, Source: "fin.src.ledger", AgeSeconds: 30, EffectiveLimitSeconds: 300, LimitSetBy: decide.LimitByTrustProfile}
	if err := b.RecordEvidence("evs_01ABC", []decide.Freshness{f}); err != nil {
		t.Fatal(err)
	}
	if err := b.RecordPolicy([]decide.PolicyRuleVersion{{RuleID: "fin.pol.default.permit_case_writes", PolicyID: "fin.pol", Version: "3.1"}}); err != nil {
		t.Fatal(err)
	}
	return b
}

func allow(t *testing.T, b *decide.BasisBuilder) decide.Decision {
	t.Helper()
	r, ok := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	if !ok {
		t.Fatal("not resolved")
	}
	d, err := r.Allow(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
	if err != nil {
		t.Fatal(err)
	}
	d, err = b.Attach(d)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func step(t *testing.T) grants.Step {
	t.Helper()
	p, err := reconcile.NewFreezeGate(nil).PermitTarget(false, capability, target)
	if err != nil {
		t.Fatal(err)
	}
	return grants.Step{PrincipalID: principal, CapabilityID: capability, TargetRef: target, ParamsHash: hash(2), EvidenceSetHash: hash(3), PlanHash: hash(4), PlanStep: 1,
		TargetVersions: []contracts.DecisionGrantV1TargetVersionsItem{{ResourceRef: target, Source: "fin.src.ledger", VersionToken: "v7"}}, Permission: p}
}

// disposition records the ESCALATE disposition of an action decision through the real Writer, as its own Trust decision with no step and no grant (A503), and returns its decision
// ID. The action decision of the same intent names it: steps if and only if a disposition.
func (w *world) disposition(t *testing.T, id string) string {
	t.Helper()
	b := basis(t)
	reason, err := decide.Core("NOT_AUTHORIZED")
	if err != nil {
		t.Fatal(err)
	}
	u, ok := decide.Admit(decide.IntentView{Version: 2, MaterialFlags: []string{"X"}}).Unresolved()
	if !ok {
		t.Fatal("not unresolved")
	}
	d, err := u.Escalate(reason)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = b.Attach(d); err != nil {
		t.Fatal(err)
	}
	tx, err := w.trustA.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.writer.Write(ctx(t), tx, recorder.Request{TenantID: tenantA, CorrelationID: corr, IntentID: id, IntentHash: hash(200), PolicyID: "fin.pol", Decision: d, Evaluated: b})
	if err != nil {
		_ = tx.Rollback(ctx(t))
		t.Fatalf("the disposition: %v", err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return string(res.Record.DecisionId)
}

func (w *world) write(t *testing.T, id string, h contracts.CommonV1Hash) recorder.Result {
	t.Helper()
	disp := w.disposition(t, id)
	b := basis(t)
	tx, err := w.trustA.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.writer.Write(ctx(t), tx, recorder.Request{TenantID: tenantA, CorrelationID: corr, IntentID: id, IntentHash: h, PolicyID: "fin.pol", Decision: allow(t, b), Evaluated: b, Steps: []grants.Step{step(t)}, DispositionDecisionID: disp})
	if err != nil {
		_ = tx.Rollback(ctx(t))
		t.Fatal(err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return res
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	keys, err := grants.NewKeys([]grants.Key{{ID: "k2", Secret: secret}})
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(epoch)
	idg := ids.New(clk, rand.New(rand.NewSource(1)))
	issuer, err := grants.NewIssuer(keys, idg, rand.New(rand.NewSource(2)), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	app := &chain.Appender{Clock: clk, IDs: idg, Signers: recordsig.For(t, admin, "trustpg", nil, "core-trust", "core-execution")}
	w := &world{
		admin:    admin,
		trustA:   testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantA),
		trustB:   testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantB),
		execA:    testpg.TenantLogin(t, admin, dsn, "execution_rw", tenantA),
		readerA:  testpg.TenantLogin(t, admin, dsn, "record_read", tenantA),
		readerB:  testpg.TenantLogin(t, admin, dsn, "record_read", tenantB),
		appender: app,
		writer:   &recorder.Writer{Keys: keys, Issuer: issuer, Appender: app, Clock: clk, IDs: idg, RecordLifetime: 15 * time.Minute, Escalation: recorder.NewEscalationSet(capability)},
	}
	w.pools, err = trustpg.NewPools(map[string]*pgxpool.Pool{tenantA: w.trustA, tenantB: w.trustB})
	if err != nil {
		t.Fatal(err)
	}
	// The admitted intent version 2 of the first decision (SYNTHETIC row, inserted as the administrator; version 1 comes first because version n supersedes n-1).
	for v := 1; v <= 2; v++ {
		h := hash(byte(9 + v))
		if v == 2 {
			h = hash(1)
		}
		sup := "NULL"
		supersedes := ""
		if v == 2 {
			supersedes = intentID + "@1"
			sup = "'" + supersedes + "'"
		}
		doc := `{"intent_id":"` + intentID + `","tenant_id":"` + tenantA + `","correlation_id":"` + corr + `","version":` + string(rune('0'+v)) + `,"status":"VALIDATED","intent_node":"fin.dispute"` + func() string {
			if v == 2 {
				return `,"supersedes":"` + supersedes + `"`
			}
			return `,"supersedes":null`
		}() + `}`
		if _, err := admin.Exec(ctx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json) VALUES ($1,$2,$3,`+sup+`,'fin.dispute',$4,$5,$6::jsonb)`,
			tenantA, intentID, v, h, corr, doc); err != nil {
			t.Fatalf("seed admitted intent %d: %v", v, err)
		}
	}
	w.allowed = w.write(t, intentID, hash(1))
	return w
}

// dispatch appends one DISPATCH_ATTEMPTED event as the tenant's Execution login, as Spend does.
func (w *world) dispatch(t *testing.T, tenant, correlation string, payload any) {
	t.Helper()
	pool := w.execA
	if tenant == tenantB {
		t.Fatal("tenant B has no execution login in this world")
	}
	tx, err := pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := w.appender.Append(ctx(t), tx, chain.Event{TenantID: tenant, CorrelationID: correlation, WrittenBy: "core-execution", Actor: "core-execution", EventType: "DISPATCH_ATTEMPTED", ObjectRef: "grt_direct", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
}
