package outcomee2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/adapters/synthetic_tushe"
	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/outcome/predicate"
	"github.com/Noemai-org/noema/services/core/outcome/store"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	tenant       = "ten_aa"
	principal    = "usr_100"
	correlation  = "corr_us06a"
	txRef        = "TX1"
	reasonCode   = "CUSTOMER_DISPUTE_UNRECOGNIZED"
	intentRef    = "int_demo01"
	execKeyID    = "k_synthetic"
	outcomeKeyID = "k_outcome_test"
	policyScope  = "pol_escalate"
	dispatchMS   = 1000 // shorter than the fault's 3500 ms hold, so the client gives up first
	caseOpened   = "fin.outcome.exception.case_opened"
)

// tickClock moves forward by one millisecond on every read and jumps on request: a synthetic clock (AGENT-25). One clock
// serves Execution and Outcome, as one clock source serves the core.
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

func (c *tickClock) jump(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// waiter is the simulated-time wait between read retries: it moves the clock and costs no real time.
type waiter struct{ clk *tickClock }

func (w waiter) Wait(_ context.Context, d time.Duration) error { w.clk.jump(d); return nil }

// trustStandIn plays the parts of Trust and the planner that are not built: it signs a grant with a synthetic key and
// stores it as the trust_rw role would, and answers the controller's Binder reads. A test fixture; it decides nothing.
type trustStandIn struct {
	mu    sync.Mutex
	pool  *pgxpool.Pool // trust_rw
	keys  *crypto.KeyRing
	clk   *tickClock
	led   *ledger.Ledger
	plans map[string]controller.PlanStep
	n     int
}

var _ controller.Binder = (*trustStandIn)(nil)

func (s *trustStandIn) LatestIntentHash(_ context.Context, g controller.Grant) (string, error) {
	return g.IntentHash, nil
}
func (s *trustStandIn) DecisionStillAllowed(context.Context, controller.Grant) (bool, error) {
	return true, nil
}
func (s *trustStandIn) Plan(_ context.Context, g controller.Grant) (controller.PlanStep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[g.GrantID]
	if !ok {
		return controller.PlanStep{}, fmt.Errorf("no plan recorded for %s", g.GrantID)
	}
	return p, nil
}
func (s *trustStandIn) WritesEnabled(ctx context.Context, tenantID string) (bool, error) {
	return s.led.WritesEnabled(ctx, tenantID)
}

func hash(n int) string { return fmt.Sprintf("sha256:%064x", n) }

// issueCreate issues the grant for create_escalation_case: no expected version, there is no case yet.
func (s *trustStandIn) issueCreate(ctx context.Context) (string, error) {
	s.mu.Lock()
	s.n++
	n := s.n
	s.mu.Unlock()
	params := map[string]any{"transaction_ref": txRef, "reason_code": reasonCode, "created_by_intent": intentRef, "epoch": int64(1)}
	paramsHash, err := crypto.HashOf(params)
	if err != nil {
		return "", err
	}
	now := s.clk.Now()
	g := controller.Grant{SchemaVersion: "1.0", GrantID: fmt.Sprintf("grt_us%04d", n), DecisionID: "dec_us06", PrincipalID: principal, IntentHash: hash(1),
		CapabilityID: bridge.CapCreateEscalationCase, TargetRef: txRef, ParamsHash: paramsHash, EvidenceSetHash: hash(2), PlanHash: hash(3), PlanStep: int64(n),
		PolicyVersion: policyScope + "@1", PackVersion: "fintech@1", NotBefore: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		Nonce: fmt.Sprintf("nonce_us%04d", n), SingleUse: true, TenantID: tenant, CorrelationID: correlation}
	canon, err := crypto.CanonicalJSON(g)
	if err != nil {
		return "", err
	}
	sig, err := s.keys.SignWith(execKeyID, canon)
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, '1', $7, $8, $9)`, g.GrantID, tenant, g.DecisionID, g.Nonce, string(canon), policyScope, g.ExpiresAt, execKeyID, sig); err != nil {
		return "", fmt.Errorf("store grant: %w", err)
	}
	s.mu.Lock()
	s.plans[g.GrantID] = controller.PlanStep{PlanHash: g.PlanHash, Step: g.PlanStep, CapabilityID: g.CapabilityID, Registered: true, AdapterExists: true, Params: params,
		KeyFields: controller.KeyFields{TransactionID: txRef, LogicalAction: "ESCALATE_CASE", ReasonCode: reasonCode, PolicyScope: policyScope, LogicalResolutionEpoch: "1"},
		TimeoutMS: dispatchMS}
	s.mu.Unlock()
	return g.GrantID, nil
}

// reader is Outcome's read-only view of the case system: the verifier's port over the core's case client. It exposes
// FindCases and nothing else, so Outcome has no write method to call.
type reader struct{ c adapters.Case }

func (r reader) FindCases(ctx context.Context, q verifier.CaseQuery) (verifier.CasePage, error) {
	page, err := r.c.FindCases(ctx, adapters.CaseQuery{TenantID: q.TenantID, IdempotencyKey: q.IdempotencyKey, PrincipalID: q.PrincipalID, TransactionRef: q.TransactionRef, Cursor: q.Cursor})
	if err != nil {
		var re *adapters.ReadError
		if !errors.As(err, &re) {
			return verifier.CasePage{}, &verifier.ReadError{Kind: verifier.ReadUnavailable}
		}
		switch re.Failure {
		case adapters.ReadUnavailable:
			return verifier.CasePage{}, &verifier.ReadError{Kind: verifier.ReadUnavailable}
		case adapters.ReadMalformed:
			return verifier.CasePage{}, &verifier.ReadError{Kind: verifier.ReadMalformed}
		default:
			return verifier.CasePage{}, &verifier.ReadError{Kind: verifier.ReadRefused}
		}
	}
	out := verifier.CasePage{Complete: page.Complete, NextCursor: page.NextCursor}
	for _, c := range page.Items {
		out.Items = append(out.Items, verifier.Case{CaseID: c.CaseID, TenantID: c.TenantID, PrincipalID: c.PrincipalID, IdempotencyKey: c.IdempotencyKey, TransactionRef: c.TransactionRef,
			Status: c.Status, OwnerQueue: c.OwnerQueue, CreatedByIntent: c.CreatedByIntent, Epoch: c.Epoch})
	}
	return out, nil
}

type outcomeSigner struct{ keys *crypto.KeyRing }

func (o outcomeSigner) Sign(b []byte) (verifier.Sig, error) {
	v, err := o.keys.SignWith(outcomeKeyID, b)
	return verifier.Sig{KeyID: outcomeKeyID, Value: v}, err
}

type notChecked struct{}

func (notChecked) Check(context.Context, verifier.Receipt) (verifier.Authenticity, error) {
	return verifier.AuthNotChecked, nil
}

// stack is Execution and Outcome wired as production is wired, around a real mock-case and a real PostgreSQL.
type stack struct {
	admin   *pgxpool.Pool
	clk     *tickClock
	ctl     *controller.Controller
	trust   *trustStandIn
	cases   *synthetic_tushe.CaseClient
	v       *verifier.Verifier
	bench   bench
	def     predicate.Definition
	request verifier.Request
}

func newStack(t *testing.T, mc mockCase) *stack {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	bg, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := db.Migrate(bg, admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(bg, `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic bank A')`, tenant); err != nil {
		t.Fatal(err)
	}
	pool := map[string]*pgxpool.Pool{}
	for _, role := range []string{"execution_rw", "trust_rw"} {
		p, err := db.OpenAsRole(bg, dsn, role)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		pool[role] = p
	}
	pool["outcome_rw"] = outcomeLogin(t, admin, dsn)
	clk := &tickClock{now: time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)} // SYNTHETIC
	gen := ids.New(clk, nil)
	execKeys := crypto.NewKeyRing(map[string][]byte{execKeyID: []byte("synthetic-test-key-not-a-secret")})
	outKeys := crypto.NewKeyRing(map[string][]byte{outcomeKeyID: []byte("synthetic-outcome-key-not-a-secret")})
	led := &ledger.Ledger{Pool: pool["execution_rw"], Clock: clk, IDs: gen, Rec: &chain.Appender{Clock: clk, IDs: gen}}
	cases, err := synthetic_tushe.NewCaseClient(mc.URL, mc.TokenFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	trust := &trustStandIn{pool: pool["trust_rw"], keys: execKeys, clk: clk, led: led, plans: map[string]controller.PlanStep{}}
	ctl := &controller.Controller{Clock: clk, Keys: execKeys, ReceiptKeyID: execKeyID, Grants: led, Bind: trust, Reval: bridge.CaseRevalidator{Case: cases},
		Ledger: led, Out: bridge.CaseDispatcher{Case: cases}, IDs: gen}

	raw, err := os.ReadFile("../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	pack, err := predicate.LoadPack(raw)
	if err != nil {
		t.Fatal(err)
	}
	def := pack.Definitions[caseOpened]
	st := &store.Store{Pool: pool["outcome_rw"], Clock: clk, IDs: gen, Rec: &chain.Appender{Clock: clk, IDs: gen}}
	v, err := verifier.New(verifier.Config{Clock: clk, Waiter: waiter{clk}, IDs: gen, Cases: reader{cases}, Store: st, Signer: outcomeSigner{outKeys}, Authenticity: notChecked{}, Scope: pack.Scope,
		Budget: verifier.ReadBudget{Backoffs: []time.Duration{time.Second, 3 * time.Second, 9 * time.Second}, MaxPages: 3}})
	if err != nil {
		t.Fatal(err)
	}
	return &stack{admin: admin, clk: clk, ctl: ctl, trust: trust, cases: cases, v: v, bench: newBench(t, mc), def: def,
		request: verifier.Request{TenantID: tenant, TransactionRef: txRef, QueryableByKey: true,
			Bindings:   map[string]string{"subject_resource": txRef, "intent_id": intentRef},
			Definition: verifier.Definition{OutcomeID: def.OutcomeID, SuccessPredicate: def.SuccessPredicate, SettleWindowSeconds: def.SettleWindowSecs}}}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }

var outcomeLogins atomic.Int64

// outcomeLogin opens Outcome's pool as a deployment would: a login that holds the outcome_rw power and no other, running as
// outcome_rw. outcome_record_verdict refuses any other login (a superuser, or one that also holds another power).
func outcomeLogin(t *testing.T, admin *pgxpool.Pool, dsn string) *pgxpool.Pool {
	t.Helper()
	bg, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name := fmt.Sprintf("outcome_e2e_login_%d_%d", time.Now().UnixNano(), outcomeLogins.Add(1))
	const password = "synthetic-test-password" // SYNTHETIC
	for _, q := range []string{`CREATE ROLE ` + name + ` LOGIN NOSUPERUSER PASSWORD '` + password + `'`, `GRANT outcome_rw TO ` + name} {
		if _, err := admin.Exec(bg, q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), `DROP ROLE IF EXISTS `+name) })
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(name, password)
	p, err := db.OpenAsRole(bg, u.String(), "outcome_rw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}
