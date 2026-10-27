package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/adapters/synthetic_tushe"
	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/workflows"
)

const (
	tenant        = "ten_aa"
	principal     = "usr_100"
	correlation   = "corr_g2case1"
	txRef         = "TX1"
	reasonCode    = "CUSTOMER_DISPUTE_UNRECOGNIZED"
	intentRef     = "int_demo01"
	plannedQueue  = "DISPUTES"
	keyID         = "k_synthetic"
	policyScope   = "pol_escalate"
	dispatchMS    = 5000
	activityLimit = 30 * time.Second
)

// tickClock moves forward by one millisecond on every read: a synthetic clock (AGENT-25), so a run is ordered and
// reproducible.
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

// trustStandIn plays the parts of Trust and the planner that are not built: it signs grants with a synthetic key,
// stores them as the trust_rw role would, and answers the controller's Binder reads from the plan it recorded. It is a
// test fixture. It decides nothing about policy and no code outside tests uses it.
type trustStandIn struct {
	mu    sync.Mutex
	pool  *pgxpool.Pool // runs as trust_rw
	keys  *crypto.KeyRing
	clk   *tickClock
	led   *ledger.Ledger
	plans map[string]controller.PlanStep
	n     int
}

var (
	_ controller.Binder          = (*trustStandIn)(nil)
	_ workflows.RouteGrantIssuer = (*trustStandIn)(nil)
)

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

// issue signs and stores one grant for a plan step.
func (s *trustStandIn) issue(ctx context.Context, capability, target string, params map[string]any, fields controller.KeyFields, versions []controller.TargetVersion, conditional bool) (string, error) {
	s.mu.Lock()
	s.n++
	n := s.n
	s.mu.Unlock()
	paramsHash, err := crypto.HashOf(params)
	if err != nil {
		return "", err
	}
	now := s.clk.Now()
	g := controller.Grant{SchemaVersion: "1.0", GrantID: fmt.Sprintf("grt_g2%04d", n), DecisionID: "dec_g2case", PrincipalID: principal, IntentHash: hash(1),
		CapabilityID: capability, TargetRef: target, ParamsHash: paramsHash, EvidenceSetHash: hash(2), PlanHash: hash(3), PlanStep: int64(n), TargetVersions: versions,
		PolicyVersion: policyScope + "@1", PackVersion: "fintech@1", NotBefore: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		Nonce: fmt.Sprintf("nonce_g2%04d", n), SingleUse: true, TenantID: tenant, CorrelationID: correlation}
	canon, err := crypto.CanonicalJSON(g)
	if err != nil {
		return "", err
	}
	sig, err := s.keys.SignWith(keyID, canon)
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, '1', $7, $8, $9)`, g.GrantID, tenant, g.DecisionID, g.Nonce, string(canon), policyScope, g.ExpiresAt, keyID, sig); err != nil {
		return "", fmt.Errorf("store grant: %w", err)
	}
	s.mu.Lock()
	s.plans[g.GrantID] = controller.PlanStep{PlanHash: g.PlanHash, Step: g.PlanStep, CapabilityID: capability, Registered: true, AdapterExists: true,
		Params: params, KeyFields: fields, TimeoutMS: dispatchMS, Conditional: conditional}
	s.mu.Unlock()
	return g.GrantID, nil
}

// IssueCreate issues the grant for create_escalation_case: no expected version (there is no case yet).
func (s *trustStandIn) IssueCreate(ctx context.Context) (string, error) {
	params := map[string]any{"transaction_ref": txRef, "reason_code": reasonCode, "created_by_intent": intentRef, "epoch": int64(1)}
	return s.issue(ctx, bridge.CapCreateEscalationCase, txRef, params,
		controller.KeyFields{TransactionID: txRef, LogicalAction: "ESCALATE_CASE", ReasonCode: reasonCode, PolicyScope: policyScope, LogicalResolutionEpoch: "1"}, nil, false)
}

// IssueRouteGrant implements workflows.RouteGrantIssuer: the route grant names the case and the version token that was read back.
func (s *trustStandIn) IssueRouteGrant(ctx context.Context, req workflows.RouteGrantRequest) (string, error) {
	params := map[string]any{"owner_queue": req.PlannedQueue}
	return s.issue(ctx, bridge.CapRouteToInternalOwner, req.CaseID, params,
		controller.KeyFields{TransactionID: txRef, LogicalAction: "ROUTE_CASE", ReasonCode: reasonCode, PolicyScope: policyScope, LogicalResolutionEpoch: "1"},
		[]controller.TargetVersion{{ResourceRef: "case:" + req.CaseID, Source: "mock-case", VersionToken: req.VersionToken}}, true)
}

// stack is the core's side of the first end-to-end case, wired as the production code is wired, around a real mock-case.
type stack struct {
	admin *pgxpool.Pool
	exec  *pgxpool.Pool
	led   *ledger.Ledger
	ctl   *controller.Controller
	trust *trustStandIn
	cases *synthetic_tushe.CaseClient
	acts  *workflows.EscalateActivities
	clk   *tickClock
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
	exec, err := db.OpenAsRole(bg, dsn, "execution_rw")
	if err != nil {
		t.Fatal(err)
	}
	trustPool, err := db.OpenAsRole(bg, dsn, "trust_rw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Close(); trustPool.Close() })

	clk := &tickClock{now: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)} // SYNTHETIC
	gen := ids.New(clk, nil)
	keys := crypto.NewKeyRing(map[string][]byte{keyID: []byte("synthetic-test-key-not-a-secret")})
	led := &ledger.Ledger{Pool: exec, Clock: clk, IDs: gen, Rec: &chain.Appender{Clock: clk, IDs: gen}}
	cases, err := synthetic_tushe.NewCaseClient(mc.URL, mc.TokenFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	trust := &trustStandIn{pool: trustPool, keys: keys, clk: clk, led: led, plans: map[string]controller.PlanStep{}}
	ctl := &controller.Controller{Clock: clk, Keys: keys, ReceiptKeyID: keyID, Grants: led, Bind: trust, Reval: bridge.CaseRevalidator{Case: cases},
		Ledger: led, Out: bridge.CaseDispatcher{Case: cases}, IDs: gen}
	return &stack{admin: admin, exec: exec, led: led, ctl: ctl, trust: trust, cases: cases, clk: clk,
		acts: &workflows.EscalateActivities{Executor: bridge.GrantExecutor{Controller: ctl}, Reader: bridge.CaseReadBack{Case: cases}, Issuer: trust}}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
