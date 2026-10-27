package e2e_test

import (
	"context"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/workflows"
)

func bg(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return c
}

// runEscalate runs the EscalateCase workflow in the Temporal SDK's test environment, with the real activities:
// the controller over Postgres, the real case client and the real mock-case.
func runEscalate(t *testing.T, st *stack, createGrant string) workflows.EscalateResult {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	suite.SetLogger(log.NewStructuredLogger(slog.New(slog.DiscardHandler)))
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(workflows.EscalateCase, workflow.RegisterOptions{Name: workflows.EscalateWorkflowName})
	env.RegisterActivity(st.acts)
	env.ExecuteWorkflow(workflows.EscalateWorkflowName, workflows.EscalateInput{TenantID: tenant, CorrelationID: correlation, CreateGrantID: createGrant,
		PlannedQueue: plannedQueue, ActivityTimeout: activityLimit})
	if !env.IsWorkflowCompleted() {
		t.Fatal("the workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res workflows.EscalateResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatal(err)
	}
	return res
}

func (s *stack) eventTypes(t *testing.T) []string {
	t.Helper()
	rows, err := s.admin.Query(bg(t), `SELECT event_type FROM record_event WHERE tenant_id = $1 ORDER BY seq`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func (s *stack) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.admin.QueryRow(bg(t), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (s *stack) casesFor(t *testing.T, key string) []adapters.CaseRecord {
	t.Helper()
	page, err := s.cases.FindCases(bg(t), adapters.CaseQuery{TenantID: tenant, IdempotencyKey: key})
	if err != nil || !page.Complete {
		t.Fatalf("find by key: %+v, %v", page, err)
	}
	return page.Items
}

// The first end-to-end case, G2 escalate (graph fin.graph.escalate), through the core adapter against the real mock-case:
// create with an idempotency key, read back with the version token, route with that token. Every write goes through the
// controller (E1 to E11) over Postgres, every effect is in the Record, and nothing says succeeded: Outcome does not exist yet.
// REQ-EXE-001..007, REQ-ENV-001, REQ-AUD-001, REQ-VER-004; INV-03, INV-04, INV-17, INV-30.
func TestG2EscalateThroughTheCoreAdapterAgainstMockCase(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	createGrant, err := st.trust.IssueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}

	res := runEscalate(t, st, createGrant)
	if res.Status != workflows.StatusAwaitingVerification || res.Reason != "" || res.Route == nil || res.ReadBack == nil {
		t.Fatalf("workflow result %+v", res)
	}

	// 1. Create with the idempotency key of PERD 27.2, computed here independently of the controller.
	wantKey := crypto.IdempotencyKey(crypto.IdempotencyKeyFields{TenantID: tenant, PrincipalID: principal, TransactionID: txRef, LogicalAction: "ESCALATE_CASE",
		ReasonCode: reasonCode, PolicyScope: policyScope, LogicalResolutionEpoch: "1"})
	c := res.Create
	if c.IdempotencyKey != wantKey || c.State != "ACKNOWLEDGED" || c.TransportClass != "ACK_2XX" || c.RawCode != "201" || c.Attempt != 1 || c.ExternalAck == "" {
		t.Fatalf("create run %+v, want key %s, ACKNOWLEDGED, ACK_2XX, raw 201, attempt 1", c, wantKey)
	}

	// 2. Read back by that key: exactly one case, with its version token.
	cases := st.casesFor(t, wantKey)
	if len(cases) != 1 {
		t.Fatalf("mock-case holds %d cases for the key, want 1", len(cases))
	}
	if res.ReadBack.CaseID != cases[0].CaseID || res.ReadBack.Reference != c.ExternalAck || res.ReadBack.Status != "OPEN" || res.ReadBack.VersionToken == "" {
		t.Fatalf("read-back %+v does not match the case mock-case holds (%+v) and the create's acknowledgement %q", res.ReadBack, cases[0], c.ExternalAck)
	}

	// 3. Route with that token: the grant carries it, the controller sent it, mock-case applied it once.
	var grantJSON struct {
		TargetRef      string `json:"target_ref"`
		TargetVersions []struct {
			VersionToken string `json:"version_token"`
		} `json:"target_versions"`
	}
	var raw []byte
	if err := st.admin.QueryRow(bg(t), `SELECT json FROM decision_grant WHERE grant_id = $1`, res.RouteGrantID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := jsonUnmarshal(raw, &grantJSON); err != nil {
		t.Fatal(err)
	}
	if grantJSON.TargetRef != res.ReadBack.CaseID || len(grantJSON.TargetVersions) != 1 || grantJSON.TargetVersions[0].VersionToken != res.ReadBack.VersionToken {
		t.Fatalf("the route grant %+v does not carry the case and token that were read back (%+v)", grantJSON, res.ReadBack)
	}
	r := *res.Route
	if r.State != "ACKNOWLEDGED" || r.TransportClass != "ACK_2XX" || r.Attempt != 1 || r.IdempotencyKey == wantKey {
		t.Fatalf("route run %+v", r)
	}
	after, err := st.cases.GetCase(bg(t), tenant, res.ReadBack.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "ROUTED" || after.OwnerQueue != plannedQueue || after.VersionToken == res.ReadBack.VersionToken {
		t.Fatalf("case after the route: %+v (read back %+v); want ROUTED to %s with a new version token", after, res.ReadBack, plannedQueue)
	}

	// 4. The Record, the ledger and the chain.
	wantEvents := []string{"REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT"}
	if got := st.eventTypes(t); !reflect.DeepEqual(got, wantEvents) {
		t.Fatalf("record events %v, want %v", got, wantEvents)
	}
	if n, err := chain.Verify(bg(t), st.admin, tenant); err != nil || n != int64(len(wantEvents)) {
		t.Fatalf("chain verification: %d events, %v", n, err)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM grant_spend`:                                              2,
		`SELECT count(*) FROM decision_grant WHERE status = 'SPENT'`:                    2,
		`SELECT count(*) FROM execution_receipt WHERE execution_state = 'ACKNOWLEDGED'`: 2,
		`SELECT count(*) FROM idempotency_record WHERE status = 'RECEIPTED'`:            2,
		`SELECT count(*) FROM outbox_event WHERE topic = 'outcome.verify'`:              2,
		`SELECT count(*) FROM recovery_request`:                                         0,
	} {
		if got := st.count(t, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}

	t.Logf("G2 escalate, SYNTHETIC, tenant %s, correlation %s", tenant, correlation)
	t.Logf("  create : grant %s, key %s, attempt %d, %s %s, receipt %s, case reference %s", createGrant, c.IdempotencyKey, c.Attempt, c.TransportClass, c.RawCode, c.ReceiptID, c.ExternalAck)
	t.Logf("  readback: case %s, status %s, queue %s, version token %s", res.ReadBack.CaseID, res.ReadBack.Status, res.ReadBack.OwnerQueue, res.ReadBack.VersionToken)
	t.Logf("  route  : grant %s, expected token %s, key %s, %s %s, receipt %s", res.RouteGrantID, grantJSON.TargetVersions[0].VersionToken, r.IdempotencyKey, r.TransportClass, r.RawCode, r.ReceiptID)
	t.Logf("  after  : case %s, status %s, queue %s, version token %s", after.CaseID, after.Status, after.OwnerQueue, after.VersionToken)
	t.Logf("  record : %d events, chain verified; workflow status %s", len(wantEvents), res.Status)
}

// EC-E05, REQ-EXE-001: the same grant executed again writes nothing, and mock-case still holds one case. Here the
// case now exists, so E7 (the open-cases read) stops the second run before E8 could answer "replay"; either way there
// is no second write, the grant stays SPENT (a spent grant is never marked void) and no RecoveryRequest is raised.
func TestG2ASpentCreateGrantExecutedAgainMakesNoSecondCase(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	createGrant, err := st.trust.IssueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	res := runEscalate(t, st, createGrant)
	spends := st.count(t, `SELECT count(*) FROM grant_spend`)
	again, err := bridge.GrantExecutor{Controller: st.ctl}.ExecuteGrant(bg(t), tenant, createGrant)
	if err != nil || again.ReceiptID != "" || (!again.Replayed && again.Void != "STATE_CHANGED") {
		t.Fatalf("second execute: %+v, %v; want no receipt and either a replay or a stop at revalidation", again, err)
	}
	if n := len(st.casesFor(t, res.Create.IdempotencyKey)); n != 1 {
		t.Fatalf("mock-case holds %d cases for the key after a repeated grant, want 1", n)
	}
	if st.count(t, `SELECT count(*) FROM grant_spend`) != spends || st.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'SPENT'`, createGrant) != 1 ||
		st.count(t, `SELECT count(*) FROM recovery_request`) != 0 {
		t.Fatal("a repeated spent grant changed the ledger")
	}
}

// REQ-EXE-003, US-19, the case-dedupe rule: a second create for the same transaction is voided at E7 before any write.
func TestG2ASecondCreateForTheSameTransactionIsVoidedAtRevalidation(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	first, _ := st.trust.IssueCreate(bg(t))
	res := runEscalate(t, st, first)
	second, err := st.trust.IssueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	run, err := bridge.GrantExecutor{Controller: st.ctl}.ExecuteGrant(bg(t), tenant, second)
	if err != nil || run.Void != "STATE_CHANGED" || run.ReceiptID != "" {
		t.Fatalf("second create: %+v, %v; want a void grant (STATE_CHANGED) and no receipt", run, err)
	}
	if n := len(st.casesFor(t, res.Create.IdempotencyKey)); n != 1 {
		t.Fatalf("mock-case holds %d cases, want 1", n)
	}
	if st.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'REVALIDATION_FAILED'`) != 1 ||
		st.count(t, `SELECT count(*) FROM recovery_request WHERE reason = 'STATE_CHANGED' AND grant_id = $1`, second) != 1 {
		t.Fatal("no REVALIDATION_FAILED event, or no RecoveryRequest, for the voided create")
	}
}

// raceDispatcher lets another writer create a case for the same transaction and epoch after revalidation (E7) and before
// the dispatch (E8): the window E7 cannot close. It makes the other write through the same adapter, with another key.
type raceDispatcher struct {
	inner controller.Dispatcher
	rival func()
}

func (r raceDispatcher) Dispatch(ctx context.Context, req controller.DispatchRequest) controller.TransportResult {
	r.rival()
	return r.inner.Dispatch(ctx, req)
}

// REQ-EXE-004, PERD 27.1 E7/E10, the create is CONDITIONAL_WRITE: revalidation found no case, then another writer made one.
// The target enforces "create only if absent" atomically, so the grant's create is refused (412), not a second case; the
// receipt is REJECTED_STALE_VERSION, the refusal is kept raw, and mock-case holds one case. Guarantee: the mock target's only.
func TestG2ACreateRacedByAnotherWriterAfterRevalidationMakesNoSecondCase(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	grant, err := st.trust.IssueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	var rival adapters.WriteResult
	ctl := *st.ctl
	ctl.Out = raceDispatcher{inner: st.ctl.Out, rival: func() {
		rival = st.cases.CreateCase(bg(t), adapters.CreateCaseInput{IdempotencyKey: "idk_rival_writer", TenantID: tenant, PrincipalID: principal,
			TransactionRef: txRef, ReasonCode: reasonCode, CreatedByIntent: "int_rival", Epoch: 1, Timeout: 5 * time.Second})
	}}
	run, err := bridge.GrantExecutor{Controller: &ctl}.ExecuteGrant(bg(t), tenant, grant)
	if err != nil {
		t.Fatal(err)
	}
	if rival.Class != adapters.ClassAck2xx || rival.RawCode != "201" {
		t.Fatalf("the rival create must have won the race: %+v", rival)
	}
	if run.Void != "" || run.State != "REJECTED_STALE_VERSION" || run.TransportClass != "VERSION_CONFLICT_409_412" || run.RawCode != "412:PRECONDITION_FAILED" || run.ExternalAck != "" {
		t.Fatalf("raced create: %+v, want REJECTED_STALE_VERSION from a 412:PRECONDITION_FAILED and no acknowledgement", run)
	}
	page, err := st.cases.FindCases(bg(t), adapters.CaseQuery{TenantID: tenant, PrincipalID: principal, TransactionRef: txRef})
	if err != nil || !page.Complete || len(page.Items) != 1 || page.Items[0].IdempotencyKey != "idk_rival_writer" {
		t.Fatalf("mock-case must hold exactly the rival's case: %+v, %v", page, err)
	}
	if got := st.casesFor(t, run.IdempotencyKey); len(got) != 0 {
		t.Fatalf("the refused create left %d cases under its own key", len(got))
	}
	if st.count(t, `SELECT count(*) FROM grant_spend`) != 1 || st.count(t, `SELECT count(*) FROM execution_receipt WHERE execution_state = 'REJECTED_STALE_VERSION'`) != 1 {
		t.Fatal("the grant must be spent once, with a REJECTED_STALE_VERSION receipt")
	}
}

// US-19, REQ-EXE-004: a route grant bound to a version token that has since moved is voided at E7; nothing is written.
func TestG2ARouteGrantBoundToAStaleTokenIsVoidedAtRevalidation(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	createGrant, _ := st.trust.IssueCreate(bg(t))
	res := runEscalate(t, st, createGrant) // routes the case: its version token moves on
	stale, err := st.trust.IssueRouteGrant(bg(t), workflows.RouteGrantRequest{TenantID: tenant, CorrelationID: correlation, CaseID: res.ReadBack.CaseID,
		VersionToken: res.ReadBack.VersionToken, PlannedQueue: "FRAUD_REVIEW"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := bridge.GrantExecutor{Controller: st.ctl}.ExecuteGrant(bg(t), tenant, stale)
	if err != nil || run.Void != "STATE_CHANGED" || run.ReceiptID != "" {
		t.Fatalf("stale route: %+v, %v; want a void grant (STATE_CHANGED)", run, err)
	}
	now, err := st.cases.GetCase(bg(t), tenant, res.ReadBack.CaseID)
	if err != nil || now.OwnerQueue != plannedQueue {
		t.Fatalf("the stale route changed the case: %+v, %v", now, err)
	}
}

// REQ-EXE-008: with the tenant kill switch on, no grant is spent and mock-case receives nothing.
func TestG2TheKillSwitchStopsTheCaseBeforeAnyWrite(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	if _, err := st.admin.Exec(bg(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = $1`, tenant); err != nil {
		t.Fatal(err)
	}
	createGrant, _ := st.trust.IssueCreate(bg(t))
	res := runEscalate(t, st, createGrant)
	if res.Status != workflows.StatusStopped || res.Reason != workflows.ReasonCreateVoid || res.Create.Void != "WRITES_DISABLED" {
		t.Fatalf("result %+v", res)
	}
	page, err := st.cases.FindCases(bg(t), adapters.CaseQuery{TenantID: tenant, PrincipalID: principal})
	if err != nil || !page.Complete || len(page.Items) != 0 {
		t.Fatalf("mock-case holds %+v (%v) although writes were disabled", page, err)
	}
	if st.count(t, `SELECT count(*) FROM grant_spend`) != 0 {
		t.Fatal("a grant was spent while the kill switch was on")
	}
}
