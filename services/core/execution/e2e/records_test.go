package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

// A noema#491 review finding (P1, atomicity): the complaint capability is CONDITIONAL_WRITE on "no complaint for the subject and epoch", but mock-case's create is
// keyed by idempotency key alone, so two executions with distinct keys could both pass E7 and write two irreversible regulatory complaints. Until the target creates
// conditionally and atomically, the capability is undispatchable: its grant is voided at E7 (REVALIDATION_UNAVAILABLE), nothing is spent and nothing reaches the case.
func TestAComplaintGrantIsVoidedAndNothingReachesTheCase(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	caseID := escalatedCase(t, st)
	g, err := st.trust.IssueRecord(bg(t), bridge.CapLogComplaintReference, caseID, "LOG_COMPLAINT", nil)
	if err != nil {
		t.Fatal(err)
	}
	out := execute(t, st, g)
	if out.Void != controller.VoidRevalidationFailed || out.Receipt != nil {
		t.Fatalf("complaint: %+v; want a void grant (REVALIDATION_UNAVAILABLE) and no receipt", out)
	}
	if n := len(st.complaintsOf(t, caseID)); n != 0 {
		t.Fatalf("mock-case holds %d complaints, want 0", n)
	}
	if st.count(t, `SELECT count(*) FROM grant_spend WHERE grant_id = $1`, g) != 0 {
		t.Fatal("the voided complaint spent its grant")
	}
}

// The reviewer's concurrent regression: eight complaint grants of the same case under distinct keys, executed at once, create no complaint. (When the target creates
// conditionally and atomically, this test becomes "exactly one"; until then "at most one" holds only because none is written.)
func TestConcurrentComplaintsOfOneCaseCreateNone(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	caseID := escalatedCase(t, st)
	const n = 8
	grants := make([]string, n)
	for i := range grants {
		g, err := st.trust.IssueRecord(bg(t), bridge.CapLogComplaintReference, caseID, fmt.Sprintf("LOG_COMPLAINT_%d", i), nil)
		if err != nil {
			t.Fatal(err)
		}
		grants[i] = g
	}
	var wg sync.WaitGroup
	outs := make([]controller.Outcome, n)
	for i := range grants {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := st.ctl.Execute(bg(t), tenant, grants[i])
			if err != nil {
				t.Error(err)
			}
			outs[i] = out
		}()
	}
	wg.Wait()
	for i, out := range outs {
		if out.Receipt != nil {
			t.Errorf("execution %d produced a receipt: %+v", i, out.Receipt)
		}
	}
	if got := len(st.complaintsOf(t, caseID)); got != 0 {
		t.Fatalf("mock-case holds %d complaints after %d concurrent executions, want 0", got, n)
	}
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
