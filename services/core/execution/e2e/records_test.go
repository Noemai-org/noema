package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// EXE-05 slice 2 (ruling A157), the Write path for US-06: evidence and a complaint reference appended to a case of the REAL mock-case, through the
// controller (E1 to E11) over Postgres. REQ-EXE-003, REQ-EXE-004, REQ-EXE-006, REQ-AUD-001; INV-03, INV-04, INV-17. All data is SYNTHETIC.
// Not claimed here: that the outcome is ACHIEVED. That verdict is Outcome's (OUT-02); a receipt is "the target acknowledged", never "it worked".

const evidencePayload = `{"note":"SYNTHETIC statement of card use at 14:02","kind":"statement"}`

// IssueRecord issues the grant of one record capability for a case. action makes the idempotency key: two grants with the same action name the same logical
// write, two with another action are two writes.
func (s *trustStandIn) IssueRecord(ctx context.Context, capability, caseID, action string, extra map[string]any) (string, error) {
	params := map[string]any{"case_id": caseID}
	for k, v := range extra {
		params[k] = v
	}
	return s.issue(ctx, capability, caseID, params,
		controller.KeyFields{TransactionID: txRef, LogicalAction: action, ReasonCode: reasonCode, PolicyScope: policyScope, LogicalResolutionEpoch: "1"}, nil, false)
}

// escalatedCase runs the escalate case and returns the case the record capabilities act on.
func escalatedCase(t *testing.T, st *stack) string {
	t.Helper()
	g, err := st.trust.IssueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	res := runEscalate(t, st, g)
	if res.ReadBack == nil || res.ReadBack.CaseID == "" {
		t.Fatalf("escalate: %+v", res)
	}
	return res.ReadBack.CaseID
}

func execute(t *testing.T, st *stack, grant string) controller.Outcome {
	t.Helper()
	out, err := st.ctl.Execute(bg(t), tenant, grant)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (st *stack) evidenceOf(t *testing.T, caseID string) []adapters.EvidenceRecord {
	t.Helper()
	page, err := st.cases.FindEvidence(bg(t), adapters.RecordQuery{TenantID: tenant, CaseID: caseID})
	if err != nil || !page.Complete {
		t.Fatalf("find evidence: %+v, %v", page, err)
	}
	return page.Items
}

func (st *stack) complaintsOf(t *testing.T, caseID string) []adapters.ComplaintRecord {
	t.Helper()
	page, err := st.cases.FindComplaints(bg(t), adapters.RecordQuery{TenantID: tenant, CaseID: caseID})
	if err != nil || !page.Complete {
		t.Fatalf("find complaints: %+v, %v", page, err)
	}
	return page.Items
}

// US-06, REQ-EXE-006: one grant, one attachment on the case, found by the case and holding the exact payload; the receipt says acknowledged and the Record
// holds the spend, the attempt and the receipt. The same grant executed again makes no second attachment (INV-03).
func TestEvidenceIsAppendedOnceThroughTheControllerToTheRealMockCase(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	caseID := escalatedCase(t, st)
	g, err := st.trust.IssueRecord(bg(t), bridge.CapRecordInvestigationEvidence, caseID, "RECORD_EVIDENCE", map[string]any{"payload": evidencePayload})
	if err != nil {
		t.Fatal(err)
	}
	before := st.eventTypes(t)
	out := execute(t, st, g)
	if out.Void != "" || out.Receipt == nil || out.Receipt.State != "ACKNOWLEDGED" || out.Receipt.ExternalAck == nil || *out.Receipt.ExternalAck == "" {
		t.Fatalf("execute: %+v", out)
	}
	items := st.evidenceOf(t, caseID)
	sum := sha256.Sum256([]byte(evidencePayload))
	if len(items) != 1 || items[0].PayloadHash != "sha256:"+hex.EncodeToString(sum[:]) || items[0].CaseID != caseID || items[0].AttachmentID != *out.Receipt.ExternalAck {
		t.Fatalf("evidence on the case: %+v, receipt %+v", items, out.Receipt)
	}
	want := map[string]int{"GRANT_SPENT": 1, "DISPATCH_ATTEMPTED": 1, "RECEIPT": 1, "REVALIDATION_PASSED": 1}
	got := map[string]int{}
	for _, e := range st.eventTypes(t)[len(before):] {
		got[e]++
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("Record events of the write: %v, want %d of %s", got, n, k)
		}
	}
	again := execute(t, st, g)
	if again.Receipt != nil && again.Receipt.ReceiptID != out.Receipt.ReceiptID {
		t.Fatalf("the spent grant produced another receipt: %+v", again)
	}
	if n := len(st.evidenceOf(t, caseID)); n != 1 {
		t.Fatalf("mock-case holds %d attachments after a repeated grant, want 1", n)
	}
}

// EXE-09, A185 (the complaint half of US-06): one complaint grant, through the controller to the REAL mock-case, creates one complaint on the case, found by the case
// and carrying the reference the receipt acknowledged. The Record holds the same events as every write. The same grant executed again writes nothing (INV-03).
func TestAComplaintIsLoggedOnceThroughTheControllerToTheRealMockCase(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	caseID := escalatedCase(t, st)
	g, err := st.trust.IssueRecord(bg(t), bridge.CapLogComplaintReference, caseID, "LOG_COMPLAINT", nil)
	if err != nil {
		t.Fatal(err)
	}
	before := st.eventTypes(t)
	out := execute(t, st, g)
	if out.Void != "" || out.Receipt == nil || out.Receipt.State != "ACKNOWLEDGED" || out.Receipt.ExternalAck == nil || *out.Receipt.ExternalAck == "" {
		t.Fatalf("execute: %+v", out)
	}
	items := st.complaintsOf(t, caseID)
	if len(items) != 1 || items[0].CaseID != caseID || items[0].ComplaintRef != *out.Receipt.ExternalAck {
		t.Fatalf("complaints on the case: %+v, receipt %+v", items, out.Receipt)
	}
	want := map[string]int{"GRANT_SPENT": 1, "DISPATCH_ATTEMPTED": 1, "RECEIPT": 1, "REVALIDATION_PASSED": 1}
	got := map[string]int{}
	for _, e := range st.eventTypes(t)[len(before):] {
		got[e]++
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("Record events of the write: %v, want %d of %s", got, n, k)
		}
	}
	again := execute(t, st, g)
	if again.Receipt != nil && again.Receipt.ReceiptID != out.Receipt.ReceiptID {
		t.Fatalf("the spent grant produced another receipt: %+v", again)
	}
	if n := len(st.complaintsOf(t, caseID)); n != 1 {
		t.Fatalf("mock-case holds %d complaints after a repeated grant, want 1", n)
	}
}

// outcomeKinds counts what eight concurrent complaint executions came to: the receipts that say ACKNOWLEDGED, the receipts that say REJECTED_BY_TARGET, and the
// grants voided before any write. Anything else is returned as "other".
func complaintOutcomes(outs []controller.Outcome) (acked, rejected, voided, other int) {
	for _, o := range outs {
		switch {
		case o.Receipt != nil && o.Receipt.State == "ACKNOWLEDGED":
			acked++
		case o.Receipt != nil && o.Receipt.State == "REJECTED_BY_TARGET":
			rejected++
		case o.Receipt == nil && o.Void != "":
			voided++
		default:
			other++
		}
	}
	return
}

// runAll executes every grant at once. Concurrent spends of one tenant contend for the head of its Record chain, and the database may refuse to serialise one of
// them: the ledger documents that as "nothing was spent, the caller may try again" (execution/ledger/spend.go), so a refused spend is presented again, a bounded
// number of times. The refusal happens before anything is dispatched, so it never makes a second write.
func runAll(t *testing.T, st *stack, grants []string) []controller.Outcome {
	t.Helper()
	const attempts = 30
	var wg sync.WaitGroup
	outs := make([]controller.Outcome, len(grants))
	for i := range grants {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := 1; ; a++ {
				out, err := st.ctl.Execute(bg(t), tenant, grants[i])
				if err == nil {
					outs[i] = out
					return
				}
				if !strings.Contains(err.Error(), "did not commit") || a == attempts {
					t.Errorf("grant %s, presentation %d: %v", grants[i], a, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	return outs
}

func complaintGrants(t *testing.T, st *stack, caseID string, n int) []string {
	t.Helper()
	grants := make([]string, n)
	for i := range grants {
		g, err := st.trust.IssueRecord(bg(t), bridge.CapLogComplaintReference, caseID, fmt.Sprintf("LOG_COMPLAINT_%d", i), nil)
		if err != nil {
			t.Fatal(err)
		}
		grants[i] = g
	}
	return grants
}

// The reviewer's concurrent regression of noema#491, now "exactly one" (A185): eight complaint grants of the same case under distinct keys, executed at once,
// create exactly one complaint. Exactly one receipt acknowledges; every other execution is voided at E7 or refused by the target; none is ever counted as a success.
func TestConcurrentComplaintsOfOneCaseCreateExactlyOne(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	caseID := escalatedCase(t, st)
	const n = 8
	outs := runAll(t, st, complaintGrants(t, st, caseID, n))
	acked, rejected, voided, other := complaintOutcomes(outs)
	if got := len(st.complaintsOf(t, caseID)); got != 1 {
		t.Fatalf("mock-case holds %d complaints after %d concurrent executions, want exactly 1", got, n)
	}
	if acked != 1 || other != 0 || acked+rejected+voided != n {
		t.Fatalf("outcomes: %d acknowledged, %d refused by the target, %d voided, %d other; want exactly one acknowledged and no other kind", acked, rejected, voided, other)
	}
}

// The target is the guard, not E7 (A185): with a revalidation that sees nothing (the window between E7 and E8 made certain, instead of waiting for the race), eight
// distinct-key executions all dispatch, mock-case creates one complaint and refuses the other seven with 412, and the seven receipts say REJECTED_BY_TARGET: not
// acknowledged, not unknown, and none carries a complaint reference as its own. Without the conditional create this test sees eight complaints.
func TestTheTargetIsTheGuardWhenRevalidationSeesNothing(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	caseID := escalatedCase(t, st)
	st.ctl.Reval = blindRevalidator{}
	const n = 8
	outs := runAll(t, st, complaintGrants(t, st, caseID, n))
	acked, rejected, voided, other := complaintOutcomes(outs)
	if got := len(st.complaintsOf(t, caseID)); got != 1 {
		t.Fatalf("mock-case holds %d complaints, want exactly 1", got)
	}
	if acked != 1 || rejected != n-1 || voided != 0 || other != 0 {
		t.Fatalf("outcomes: %d acknowledged, %d refused, %d voided, %d other; want 1, %d, 0, 0", acked, rejected, voided, other, n-1)
	}
	for _, o := range outs {
		if o.Receipt != nil && o.Receipt.State == "REJECTED_BY_TARGET" && (o.Receipt.ExternalAck != nil || o.Receipt.RawCode != "412:PRECONDITION_FAILED" || o.Receipt.TransportResult != "DUPLICATE_CREATE") {
			t.Errorf("a refused complaint's receipt: %+v", o.Receipt)
		}
	}
}

// blindRevalidator passes every grant: it stands for E7 having read "no complaint" just before a rival wrote one.
type blindRevalidator struct{}

func (blindRevalidator) Revalidate(context.Context, controller.Grant, controller.PlanStep) (controller.Revalidation, error) {
	return controller.Revalidation{Passed: true}, nil
}

// A grant for one case cannot be used to write on another: the plan's case_id must be the grant's target. The refusal is an adapter defect, no call is
// made and nothing is attached to either case (INV-03).
func TestAGrantForOneCaseCannotAttachEvidenceToAnother(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	caseID := escalatedCase(t, st)
	g, err := st.trust.issue(bg(t), bridge.CapRecordInvestigationEvidence, caseID, map[string]any{"case_id": "case_other", "payload": evidencePayload},
		controller.KeyFields{TransactionID: txRef, LogicalAction: "RECORD_EVIDENCE", ReasonCode: reasonCode, PolicyScope: policyScope, LogicalResolutionEpoch: "1"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	out := execute(t, st, g)
	if out.Receipt == nil || out.Receipt.State != "FAILED_TERMINAL" || out.Receipt.TransportResult != "ADAPTER_NON_RETRYABLE_DEFECT" || out.Receipt.RawCode != "TARGET_MISMATCH" {
		t.Fatalf("a plan for another case than the grant's: %+v; want a terminal adapter defect, TARGET_MISMATCH, and no call to the target", out.Receipt)
	}
	if n := len(st.evidenceOf(t, caseID)); n != 0 {
		t.Fatalf("mock-case holds %d attachments on the grant's case, want 0", n)
	}
}
