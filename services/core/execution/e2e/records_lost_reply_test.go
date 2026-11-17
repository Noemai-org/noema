package e2e_test

import (
	"encoding/json"
	"os"
	"testing"
)

// EXE-09, A185 (the Security Reviewer's note 1 on noema#588): the target COMMITS the complaint and the reply is lost, with the REAL mock-case faulting through a FaultPlan loaded
// by the test-only bench client (AGENT-12: no mocked failure mode). REQ-EXE-003, REQ-EXE-006, REQ-EXE-007; INV-03, INV-04, INV-07; AGENT-07, AGENT-08. All data is SYNTHETIC.
// The first complaint create is held and dropped (TIMEOUT_AFTER_COMMIT): the receipt is OUTCOME_UNKNOWN, never an acknowledgement, yet the complaint exists. No retry is made
// without a new grant (AGENT-08), and a new grant, whatever its key, never makes a second complaint.

func complaintLostReplyPlan(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(faultBase)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	plan["fault_plan_id"] = "fp_complaint_reply_lost_v1"
	plan["faults"] = []any{map[string]any{"target": "mock-case", "operation": "create_complaint", "match": map[string]any{"nth_call": 1},
		"effect": "TIMEOUT_AFTER_COMMIT", "params": map[string]any{"delay_ms": 60000}}}
	out, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAComplaintTheTargetCommittedButWhoseReplyWasLostIsUnknownAndNeverCreatedTwice(t *testing.T) {
	mc := startMockCase(t)
	st := newStack(t, mc)
	caseID := escalatedCase(t, st)
	newBenchClient(t, mc).loadPlan(t, complaintLostReplyPlan(t))

	lost, err := st.trust.IssueRecord(bg(t), "fin.cap.log_complaint_reference", caseID, "LOG_COMPLAINT_LOST", nil)
	if err != nil {
		t.Fatal(err)
	}
	first := execute(t, st, lost)
	if first.Receipt == nil || first.Receipt.State != "OUTCOME_UNKNOWN" || first.Receipt.ExternalAck != nil {
		t.Fatalf("the receipt of a write whose reply was lost: %+v, want OUTCOME_UNKNOWN and no acknowledgement (INV-07)", first.Receipt)
	}
	if n := len(st.complaintsOf(t, caseID)); n != 1 {
		t.Fatalf("mock-case holds %d complaints after the committed write whose reply was lost, want 1: the fault did not commit before it dropped the reply", n)
	}

	// the same grant presented again writes nothing (AGENT-08: no retry without a new grant)
	if again := execute(t, st, lost); again.Receipt != nil && again.Receipt.ReceiptID != first.Receipt.ReceiptID {
		t.Fatalf("the spent grant produced another receipt: %+v", again)
	}
	if n := len(st.complaintsOf(t, caseID)); n != 1 {
		t.Fatalf("%d complaints after the spent grant was presented again, want 1", n)
	}

	// a NEW grant for another logical write (another key): E7 reads the complaint the target committed and voids it, so nothing is sent (STATE_CHANGED)
	other, err := st.trust.IssueRecord(bg(t), "fin.cap.log_complaint_reference", caseID, "LOG_COMPLAINT_AFTER_LOSS", nil)
	if err != nil {
		t.Fatal(err)
	}
	if second := execute(t, st, other); second.Receipt != nil || second.Void != "STATE_CHANGED" {
		t.Fatalf("a new grant after the loss, with the case showing its complaint: receipt %+v, void %q, want the grant voided at E7 (STATE_CHANGED) and no receipt", second.Receipt, second.Void)
	}
	if n := len(st.complaintsOf(t, caseID)); n != 1 {
		t.Fatalf("%d complaints after a new grant, want 1", n)
	}

	// and with E7 made blind (the window between E7 and E8, certain instead of waited for), the TARGET refuses it: 412, never a second complaint, never an acknowledgement
	st.ctl.Reval = blindRevalidator{}
	blind, err := st.trust.IssueRecord(bg(t), "fin.cap.log_complaint_reference", caseID, "LOG_COMPLAINT_BLIND", nil)
	if err != nil {
		t.Fatal(err)
	}
	third := execute(t, st, blind)
	if third.Receipt == nil || third.Receipt.State != "REJECTED_BY_TARGET" || third.Receipt.ExternalAck != nil || third.Receipt.TransportResult != "DUPLICATE_CREATE" || third.Receipt.RawCode != "412:PRECONDITION_FAILED" {
		t.Fatalf("a new grant after the loss, with E7 blind: %+v (void %q), want REJECTED_BY_TARGET, DUPLICATE_CREATE, 412 and no acknowledgement", third.Receipt, third.Void)
	}
	if n := len(st.complaintsOf(t, caseID)); n != 1 {
		t.Fatalf("%d complaints after the target refused a new grant, want 1", n)
	}
}
