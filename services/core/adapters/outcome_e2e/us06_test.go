package outcomee2e_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const faultFixture = "../../../../contracts/fixtures/valid/fault-plan/mock_case_commit_then_drop.json"

func bg(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return c
}

func theKey() string {
	return crypto.IdempotencyKey(crypto.IdempotencyKeyFields{TenantID: tenant, PrincipalID: principal, TransactionID: txRef, LogicalAction: "ESCALATE_CASE",
		ReasonCode: reasonCode, PolicyScope: policyScope, LogicalResolutionEpoch: "1"})
}

// dispatch issues the create grant and executes it once through the controller (E1 to E11) and returns its receipt.
func (s *stack) dispatch(t *testing.T) *controller.Receipt {
	t.Helper()
	grant, err := s.trust.issueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.ctl.Execute(bg(t), tenant, grant)
	if err != nil {
		t.Fatal(err)
	}
	if out.Receipt == nil || out.Void != "" || out.Replayed {
		t.Fatalf("the create did not reach a receipt: %+v", out)
	}
	return out.Receipt
}

// verify jumps past the settle window and verifies the receipt.
func (s *stack) verify(t *testing.T, receiptID string) (verifier.Result, error) {
	t.Helper()
	s.clk.jump(time.Duration(s.def.SettleWindowSecs+1) * time.Second)
	req := s.request
	req.ReceiptID = receiptID
	req.VerificationID = "obx_" + strings.TrimPrefix(receiptID, "rcp_") // the id of the outcome.verify delivery: the outbox ID, `obx_<ULID>`, as the ledger mints it (A184: a core-minted ID, not a prefix over a prefix)
	return s.v.Verify(bg(t), req)
}

func (s *stack) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.admin.QueryRow(bg(t), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
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

func after(events []string, marker string) []string {
	for i, e := range events {
		if e == marker {
			return events[i+1:]
		}
	}
	return nil
}

// US-06 (REQ-RCV-002, REQ-VER-001, INV-04, INV-07): the target commits the create and the response is lost, with the real
// FaultPlan fixture. The receipt says OUTCOME_UNKNOWN. Outcome finds the committed case by the idempotency key and returns
// ACHIEVED, with exactly one dispatch and one logical case, and no second create.
func TestUS06TheTargetCommitsAndTheResponseIsLostThenOutcomeFindsTheCaseByKey(t *testing.T) {
	mc := startMockCase(t)
	s := newStack(t, mc)
	plan, err := os.ReadFile(faultFixture)
	if err != nil {
		t.Fatal(err)
	}
	s.bench.loadPlan(t, plan)

	rc := s.dispatch(t)
	if rc.State != controller.StateOutcomeUnknown || rc.TransportResult != "TIMEOUT" {
		t.Fatalf("receipt %+v, want OUTCOME_UNKNOWN after a TIMEOUT: the response was lost (INV-07)", rc)
	}
	s.bench.advance(t, "after_receipt")

	// The failure is real, not mocked: the target committed the create and suppressed the response.
	received, committed := creates(s.bench.commands(t), theKey())
	if received != 1 || committed != 1 {
		t.Fatalf("the target received %d creates and committed %d under the key, want 1 and 1", received, committed)
	}
	var dropped bool
	for _, c := range s.bench.commands(t) {
		if c.Operation == "create_case" && c.ResponseSuppressed && c.FaultEffect != nil && *c.FaultEffect == "TIMEOUT_AFTER_COMMIT" {
			dropped = true
		}
	}
	if !dropped {
		t.Fatal("the fault trace shows no TIMEOUT_AFTER_COMMIT: the response was not really lost")
	}

	res, err := s.verify(t, rc.ReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != verifier.Achieved || res.Reason != verifier.ReasonPredicateTrue {
		t.Fatalf("verdict %s/%s, want ACHIEVED/PREDICATE_TRUE", res.Verdict, res.Reason)
	}

	// Exactly one dispatch and one logical case, no second create.
	if n := s.count(t, `SELECT count(*) FROM grant_spend`); n != 1 {
		t.Fatalf("grant spends = %d, want 1", n)
	}
	if n := s.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'DISPATCH_ATTEMPTED'`); n != 1 {
		t.Fatalf("dispatches on the Record = %d, want 1", n)
	}
	cases := s.bench.cases(t)
	if len(cases) != 1 || cases[0].IdempotencyKey != theKey() || cases[0].CreatedByIntent != intentRef {
		t.Fatalf("the target holds %+v, want one case under the key", cases)
	}
	if received, _ := creates(s.bench.commands(t), theKey()); received != 1 {
		t.Fatalf("the target received %d creates after verification, want still 1", received)
	}

	// Auditable from the Record: the receipt, then Outcome's reads and verdict, and nothing from Execution after the verdict.
	events := s.eventTypes(t)
	tail := after(events, "RECEIPT")
	if got := strings.Join(tail, ","); got != "VERIFICATION_READ,VERIFICATION_READ,VERDICT" {
		t.Fatalf("events after the receipt: %s", got)
	}
	if _, err := chain.Verify(bg(t), s.admin, tenant); err != nil {
		t.Fatalf("the Record chain does not verify: %v", err)
	}
	if n := s.count(t, `SELECT count(*) FROM recovery_request WHERE verdict_id IS NOT NULL`); n != 0 {
		t.Fatalf("ACHIEVED raised %d recovery requests", n)
	}

	// The outbox delivers at least once: the same verification again returns the same verdict and touches nothing.
	again, err := s.verify(t, rc.ReceiptID)
	if err != nil || !again.Replayed || again.VerdictID != res.VerdictID {
		t.Fatalf("second delivery: %+v %v", again, err)
	}
	if n := s.count(t, `SELECT count(*) FROM outcome_verdict`); n != 1 {
		t.Fatalf("verdicts after a second delivery = %d, want 1", n)
	}
	if received, _ := creates(s.bench.commands(t), theKey()); received != 1 {
		t.Fatalf("creates received after a second delivery = %d, want 1", received)
	}
}

// planWith is the committed fixture with its faults replaced: the same plan format, the same scenario, never a stand-in
// for the target.
func planWith(t *testing.T, id string, faults ...map[string]any) []byte {
	t.Helper()
	raw, err := os.ReadFile(faultFixture)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := jsonUnmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	plan["fault_plan_id"] = id
	list := make([]any, len(faults))
	for i, f := range faults {
		list[i] = f
	}
	plan["faults"] = list
	out, err := jsonMarshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func fault(op, effect string, params map[string]any) map[string]any {
	return faultAt(op, effect, 1, params)
}

func faultAt(op, effect string, nth int, params map[string]any) map[string]any {
	return map[string]any{"target": "mock-case", "operation": op, "match": map[string]any{"nth_call": nth}, "effect": effect, "params": params}
}

// outcomesFirstRead is the nth find_cases call that is Outcome's first read: Execution's revalidation (E7) makes the first.
const outcomesFirstRead = 2

func (s *stack) recoveryFor(t *testing.T, verdictID string) int {
	return s.count(t, `SELECT count(*) FROM recovery_request WHERE verdict_id = $1 AND grant_id IS NULL AND status = 'OPEN'`, verdictID)
}

// US-16, REQ-VER-003: the write never reached the target and the client gave up. The receipt says OUTCOME_UNKNOWN; Outcome
// verifies by key that the effect is absent: NOT_ACHIEVED, one RecoveryRequest to Trust, and still no second create.
func TestUS16TheWriteNeverCommittedSoOutcomeVerifiesItAbsent(t *testing.T) {
	s := newStack(t, startMockCase(t))
	s.bench.loadPlan(t, planWith(t, "fp_case_create_timeout_before_commit_v1", fault("create_case", "TIMEOUT_BEFORE_COMMIT", map[string]any{"delay_ms": 3500})))
	rc := s.dispatch(t)
	if rc.State != controller.StateOutcomeUnknown {
		t.Fatalf("receipt %+v, want OUTCOME_UNKNOWN", rc)
	}
	if received, committed := creates(s.bench.commands(t), theKey()); received != 1 || committed != 0 {
		t.Fatalf("the target received %d creates and committed %d, want 1 and 0", received, committed)
	}
	res, err := s.verify(t, rc.ReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != verifier.NotAchieved || res.Reason != verifier.ReasonVerifiedAbsent {
		t.Fatalf("verdict %s/%s, want NOT_ACHIEVED/VERIFIED_ABSENT", res.Verdict, res.Reason)
	}
	if n := s.recoveryFor(t, res.VerdictID); n != 1 {
		t.Fatalf("open recovery requests for the verdict = %d, want 1", n)
	}
	if got := strings.Join(after(s.eventTypes(t), "RECEIPT"), ","); got != "VERIFICATION_READ,VERIFICATION_READ,VERDICT,RECOVERY_REQUESTED" {
		t.Fatalf("events after the receipt: %s", got)
	}
	if n := s.count(t, `SELECT count(*) FROM grant_spend`); n != 1 {
		t.Fatalf("grant spends = %d: Outcome retried (AGENT-08)", n)
	}
	if received, _ := creates(s.bench.commands(t), theKey()); received != 1 {
		t.Fatalf("creates received after verification = %d: a blind retry happened (REQ-RCV-002)", received)
	}
	if len(s.bench.cases(t)) != 0 {
		t.Fatal("the target holds a case although nothing was committed")
	}
}

// EC-E03, AGENT-21, INV-04: the target answers 200 with a semantic PENDING and creates nothing. Execution already records
// that as OUTCOME_UNKNOWN (an HTTP 200 is not an outcome); Outcome verifies by key that nothing exists: NOT_ACHIEVED, never
// ACHIEVED, with a RecoveryRequest.
func TestEC03AnHTTP200IsNotAnOutcome(t *testing.T) {
	s := newStack(t, startMockCase(t))
	s.bench.loadPlan(t, planWith(t, "fp_case_create_200_pending_v1", fault("create_case", "HTTP_200_SEMANTIC_PENDING", map[string]any{})))
	rc := s.dispatch(t)
	if rc.State != controller.StateOutcomeUnknown || rc.TransportResult != "ACK_2XX" || rc.RawCode != "200:PENDING" {
		t.Fatalf("receipt %+v, want OUTCOME_UNKNOWN for ACK_2XX 200:PENDING", rc)
	}
	res, err := s.verify(t, rc.ReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != verifier.NotAchieved || res.Reason != verifier.ReasonVerifiedAbsent {
		t.Fatalf("verdict %s/%s, want NOT_ACHIEVED/VERIFIED_ABSENT", res.Verdict, res.Reason)
	}
	if n := s.recoveryFor(t, res.VerdictID); n != 1 {
		t.Fatalf("open recovery requests = %d, want 1", n)
	}
	if len(s.bench.cases(t)) != 0 {
		t.Fatal("the target holds a case although it created nothing")
	}
}

// AGENT-16, PERD 29.2: a 5xx on Outcome's own first read is retried after 1 s of simulated time, and the verdict still
// follows the target. The fault is the real mock-case's, on the read path.
func TestAReadThatFailsOnceIsRetriedAndStillAchieves(t *testing.T) {
	s := newStack(t, startMockCase(t))
	s.bench.loadPlan(t, planWith(t, "fp_us06_with_read_5xx_v1",
		fault("create_case", "TIMEOUT_AFTER_COMMIT", map[string]any{"delay_ms": 3500}),
		faultAt("find_cases", "HTTP_5XX", outcomesFirstRead, map[string]any{"status": 503})))
	rc := s.dispatch(t)
	before := s.clk.Now()
	res, err := s.verify(t, rc.ReceiptID)
	if err != nil || res.Verdict != verifier.Achieved {
		t.Fatalf("%+v %v", res, err)
	}
	if waited := s.clk.Now().Sub(before); waited < (time.Duration(s.def.SettleWindowSecs)+2)*time.Second {
		t.Fatalf("the simulated clock moved only %s: the 1 s backoff was not waited", waited)
	}
	if got := strings.Join(after(s.eventTypes(t), "RECEIPT"), ","); got != "VERIFICATION_READ,VERIFICATION_READ,VERIFICATION_READ,VERDICT" {
		t.Fatalf("events after the receipt: %s (the failed read must be on the Record too)", got)
	}
}

// AGENT-09: text the target puts in a field is data. An injected reason_code on Outcome's reads changes nothing.
func TestInjectedTextInTheTargetsAnswerChangesNothing(t *testing.T) {
	s := newStack(t, startMockCase(t))
	s.bench.loadPlan(t, planWith(t, "fp_us06_with_injection_v1",
		fault("create_case", "TIMEOUT_AFTER_COMMIT", map[string]any{"delay_ms": 3500}),
		faultAt("find_cases", "INJECTION_PAYLOAD", outcomesFirstRead, map[string]any{"field": "reason_code"})))
	rc := s.dispatch(t)
	res, err := s.verify(t, rc.ReceiptID)
	if err != nil || res.Verdict != verifier.Achieved {
		t.Fatalf("%+v %v", res, err)
	}
	var doc string
	if err := s.admin.QueryRow(bg(t), `SELECT json::text FROM outcome_verdict WHERE verdict_id = $1`, res.VerdictID).Scan(&doc); err != nil || strings.Contains(strings.ToLower(doc), "ignore all previous") {
		t.Fatalf("the verdict carries target text: %v %s", err, doc)
	}
}
