package outcomee2e_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
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
	"github.com/Noemai-org/noema/services/core/outcome/receiptsig"
	"github.com/Noemai-org/noema/services/core/outcome/store"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	tenant      = "ten_aa"
	principal   = "usr_100"
	correlation = "corr_us06a"
	txRef       = "TX1"
	reasonCode  = "CUSTOMER_DISPUTE_UNRECOGNIZED"
	intentRef   = "int_demo01"
	execKeyID   = "k_synthetic"
	// receiptKeyID is core-execution's Ed25519 key (DEC-2026-11-01-receipt-and-record-signing D4). The signer behind it is a
	// SYNTHETIC stand-in for EXE-07; the key is generated for each run and is never mounted anywhere else.
	receiptKeyID = "core-execution.ed25519.1"
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
func (s *trustStandIn) ActivePolicyVersion(_ context.Context, g controller.Grant) (string, error) {
	return g.PolicyVersion, nil
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
		PolicyVersion: policyScope + "@1.0", PackVersion: "fintech@1", NotBefore: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		Nonce: fmt.Sprintf("nonce_us%04d", n), SingleUse: true, TenantID: tenant, CorrelationID: correlation}
	canon, err := crypto.CanonicalJSON(g)
	if err != nil {
		return "", err
	}
	sig, err := s.keys.SignWith(execKeyID, crypto.GrantSigningMessage(canon))
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

// receiptRing is the controller's key source in this test: HMAC for grants as today, and Ed25519 for receipts under
// receiptKeyID, signing the domain tag followed by the canonical receipt (D6). It stands in for EXE-07's signer.
type receiptRing struct {
	hmac *crypto.KeyRing
	priv ed25519.PrivateKey
}

func (r receiptRing) SignWith(keyID string, msg []byte) (string, error) {
	if keyID != receiptKeyID {
		return r.hmac.SignWith(keyID, msg)
	}
	return hex.EncodeToString(ed25519.Sign(r.priv, append([]byte("noema/receipt/v1\n"), msg...))), nil
}

func (r receiptRing) VerifyWith(keyID string, msg []byte, sigHex string) bool {
	if keyID == receiptKeyID {
		return false // nothing but Outcome's checker verifies a receipt here
	}
	return r.hmac.VerifyWith(keyID, msg, sigHex)
}

// publicKeys is Outcome's verify-only view of Execution's keys (a stand-in for the signing_key reader, W4).
type publicKeys map[string]receiptsig.PublicKey

func (p publicKeys) Lookup(_ context.Context, id string) (receiptsig.PublicKey, bool, error) {
	k, ok := p[id]
	return k, ok, nil
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
	pubs    publicKeys // Outcome's view of Execution's public keys; a test replaces an entry to show a receipt is refused
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
	for _, role := range []string{"execution_rw", "trust_rw", "outcome_rw"} {
		pool[role] = testpg.TenantLogin(t, admin, dsn, role, tenant) // the tenant's own logins (DEBT-04)
	}
	clk := &tickClock{now: time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)} // SYNTHETIC
	gen := ids.New(clk, nil)
	execKeys := crypto.NewKeyRing(map[string][]byte{execKeyID: []byte("synthetic-test-key-not-a-secret")})
	receiptPub, receiptPriv, err := ed25519.GenerateKey(rand.Reader) // a random key per run
	if err != nil {
		t.Fatal(err)
	}
	ring := receiptRing{hmac: execKeys, priv: receiptPriv}
	outKeys := crypto.NewKeyRing(map[string][]byte{outcomeKeyID: []byte("synthetic-outcome-key-not-a-secret")})
	led := &ledger.Ledger{Pool: pool["execution_rw"], Clock: clk, IDs: gen, Rec: &chain.Appender{Clock: clk, IDs: gen}}
	cases, err := synthetic_tushe.NewCaseClient(mc.URL, mc.TokenFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	trust := &trustStandIn{pool: pool["trust_rw"], keys: execKeys, clk: clk, led: led, plans: map[string]controller.PlanStep{}}
	ctl := &controller.Controller{Clock: clk, Keys: ring, ReceiptKeyID: receiptKeyID, Grants: led, Bind: trust, Reval: bridge.CaseRevalidator{Case: cases},
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
	pubs := publicKeys{receiptKeyID: {Key: receiptPub, Status: receiptsig.Active}}
	v, err := verifier.New(verifier.Config{Clock: clk, Waiter: waiter{clk}, IDs: gen, Cases: reader{cases}, Store: st, Signer: outcomeSigner{outKeys}, Authenticity: receiptsig.Checker{Keys: pubs}, Scope: pack.Scope,
		Budget: verifier.ReadBudget{Backoffs: []time.Duration{time.Second, 3 * time.Second, 9 * time.Second}, MaxPages: 3}})
	if err != nil {
		t.Fatal(err)
	}
	return &stack{admin: admin, clk: clk, ctl: ctl, trust: trust, cases: cases, v: v, bench: newBench(t, mc), def: def, pubs: pubs,
		request: verifier.Request{TenantID: tenant, TransactionRef: txRef, QueryableByKey: true,
			Bindings:   map[string]string{"subject_resource": txRef, "intent_id": intentRef},
			Definition: verifier.Definition{OutcomeID: def.OutcomeID, SuccessPredicate: def.SuccessPredicate, SettleWindowSeconds: def.SettleWindowSecs}}}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }
