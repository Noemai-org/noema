package ledger_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	tenant    = "ten_aa"
	keyID     = "k_test"
	principal = "usr_100"
	capab     = "fin.cap.create_escalation_case"
	corr      = "corr_abc123"
)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

// tickClock moves forward by one millisecond on every read, so that "created before dispatched" is a strict
// comparison and a simulated run is reproducible. It is a synthetic clock (AGENT-25).
type tickClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *tickClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Millisecond)
	return c.now
}

// fakeTarget is the dispatcher: it counts calls and answers as told. It reads the database from a separate
// connection at dispatch time, to show what had already been committed.
type fakeTarget struct {
	mu        sync.Mutex
	admin     *pgxpool.Pool
	calls     []controller.DispatchRequest
	answer    controller.TransportResult
	seenSpent []bool
}

func (f *fakeTarget) Dispatch(ctx context.Context, req controller.DispatchRequest) controller.TransportResult {
	var spent bool
	_ = f.admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM grant_spend gs JOIN idempotency_record ir ON ir.idempotency_key = gs.idempotency_key
		JOIN record_event re ON re.object_ref = gs.grant_id AND re.event_type = 'DISPATCH_ATTEMPTED'
		WHERE gs.grant_id = $1 AND ir.status = 'DISPATCHING')`, req.GrantID).Scan(&spent)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	f.seenSpent = append(f.seenSpent, spent)
	return f.answer
}

func (f *fakeTarget) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// stand-ins for the parts of Trust and Evidence that are not built: they read what the test planted.
type binder struct {
	l      *ledger.Ledger
	params map[string]any
	fields controller.KeyFields
	onE6   func()
}

func (b *binder) LatestIntentHash(_ context.Context, g controller.Grant) (string, error) {
	return g.IntentHash, nil
}
func (b *binder) DecisionStillAllowed(context.Context, controller.Grant) (bool, error) {
	return true, nil
}
func (b *binder) Plan(_ context.Context, g controller.Grant) (controller.PlanStep, error) {
	return controller.PlanStep{PlanHash: g.PlanHash, Step: g.PlanStep, CapabilityID: g.CapabilityID, Registered: true, AdapterExists: true,
		Params: b.params, KeyFields: b.fields, TimeoutMS: int64(time.Second / time.Millisecond), Conditional: true}, nil
}
func (b *binder) WritesEnabled(ctx context.Context, tenantID string) (bool, error) {
	ok, err := b.l.WritesEnabled(ctx, tenantID)
	if b.onE6 != nil {
		b.onE6()
	}
	return ok, err
}

type reval struct{ pass bool }

func (r reval) Revalidate(context.Context, controller.Grant, controller.PlanStep) (controller.Revalidation, error) {
	return controller.Revalidation{Passed: r.pass, ChangedFacts: []string{"case_status"}}, nil
}

type world struct {
	admin  *pgxpool.Pool
	exec   *pgxpool.Pool
	clk    *tickClock
	keys   *crypto.KeyRing
	led    *ledger.Ledger
	target *fakeTarget
	bind   *binder
	ctl    *controller.Controller
	ids    *ids.Generator
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'A')`, tenant); err != nil {
		t.Fatal(err)
	}
	exec, err := db.OpenAsRole(ctx(t), dsn, "execution_rw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(exec.Close)
	clk := &tickClock{now: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)}
	gen := ids.New(clk, nil)
	keys := crypto.NewKeyRing(map[string][]byte{keyID: []byte("synthetic-test-key-not-a-secret")})
	led := &ledger.Ledger{Pool: exec, Clock: clk, IDs: gen, Rec: &chain.Appender{Clock: clk, IDs: gen}}
	params := map[string]any{"transaction_ref": "TX1", "reason_code": "CUSTOMER_DISPUTE_UNRECOGNIZED", "epoch": int64(1)}
	b := &binder{l: led, params: params, fields: controller.KeyFields{TransactionID: "TX1", LogicalAction: "ESCALATE_CASE", ReasonCode: "CUSTOMER_DISPUTE_UNRECOGNIZED",
		PolicyScope: "pol_escalate", LogicalResolutionEpoch: "1"}}
	target := &fakeTarget{admin: admin, answer: controller.TransportResult{Class: controller.TransportAck2xx, ExternalAck: "CASE-1", RawCode: "201"}}
	w := &world{admin: admin, exec: exec, clk: clk, keys: keys, led: led, target: target, bind: b, ids: gen}
	w.ctl = &controller.Controller{Clock: clk, Keys: keys, ReceiptKeyID: keyID, Grants: led, Bind: b, Reval: reval{pass: true}, Ledger: led, Out: target, IDs: gen}
	return w
}

// issue plays Trust: it signs a grant for the plan in the binder and stores it, as trust_rw would.
func (w *world) issue(t *testing.T, n int, mutate ...func(*controller.Grant)) controller.Grant {
	t.Helper()
	ph, err := crypto.HashOf(w.bind.params)
	if err != nil {
		t.Fatal(err)
	}
	now := w.clk.Now()
	g := controller.Grant{SchemaVersion: "1.0", GrantID: fmt.Sprintf("grt_%04d", n), DecisionID: "dec_0001", PrincipalID: principal,
		IntentHash: "sha256:" + fmt.Sprintf("%064d", 1), CapabilityID: capab, TargetRef: "TX1", ParamsHash: ph, EvidenceSetHash: "sha256:" + fmt.Sprintf("%064d", 2),
		PlanHash: "sha256:" + fmt.Sprintf("%064d", 3), PlanStep: 1, TargetVersions: []controller.TargetVersion{{ResourceRef: "case:TX1", Source: "mock-case", VersionToken: "v1"}},
		PolicyVersion: "pol_escalate@1", PackVersion: "fin@1", NotBefore: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		Nonce: fmt.Sprintf("nonce_%04d", n), SingleUse: true, TenantID: tenant, CorrelationID: corr}
	for _, m := range mutate {
		m(&g)
	}
	canon, err := crypto.CanonicalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := w.keys.SignWith(keyID, canon)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		VALUES ($1, $2, $3, $4, $5::jsonb, 'pol_escalate', '1', $6, $7, $8)`, g.GrantID, g.TenantID, g.DecisionID, g.Nonce, string(canon), g.ExpiresAt, keyID, sig); err != nil {
		t.Fatal(err)
	}
	return g
}

func (w *world) events(t *testing.T) []string {
	t.Helper()
	rows, err := w.admin.Query(ctx(t), `SELECT event_type FROM record_event WHERE tenant_id = $1 ORDER BY seq`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func (w *world) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(t), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
