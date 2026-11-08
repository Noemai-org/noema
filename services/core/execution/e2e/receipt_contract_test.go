package e2e_test

// Ruling of 8 Nov 2026 on noema#330: the execution-receipt.v1 schema text decides what a receipt's logical_action holds (a PackId). This test reads the
// RECEIPT events the real controller wrote through the real stack and decodes each payload with the generated contract type, which validates every required
// field and the PackId pattern. REQ-EXE-007, REQ-AUD-001 (SoT §43.3). All data is SYNTHETIC.

import (
	"encoding/json"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

func TestEveryReceiptTheRealControllerWritesIsAValidExecutionReceipt(t *testing.T) {
	st := newStack(t, startMockCase(t))
	first, err := st.trust.IssueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	runEscalate(t, st, first)
	rows, err := st.admin.Query(bg(t), `SELECT payload FROM record_event WHERE event_type = 'RECEIPT' ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		// The event payload is the signed receipt document plus the raw status, which travels beside it unsigned (REQ-EXE-007; the controller's Document()
		// leaves it out of the signature). That one member is outside execution-receipt.v1, which allows no other; every other member must satisfy the contract.
		var members map[string]json.RawMessage
		if err := json.Unmarshal(raw, &members); err != nil {
			t.Fatal(err)
		}
		if _, ok := members["raw_status"]; !ok {
			t.Errorf("the RECEIPT event carries no raw_status beside the receipt (REQ-EXE-007): %s", raw)
		}
		delete(members, "raw_status")
		doc, err := json.Marshal(members)
		if err != nil {
			t.Fatal(err)
		}
		var r contracts.ExecutionReceiptV1
		if err := json.Unmarshal(doc, &r); err != nil {
			t.Errorf("a RECEIPT event written by the real controller is not a valid execution-receipt.v1: %v\n%s", err, doc)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("the real write produced no RECEIPT event to check")
	}
}
