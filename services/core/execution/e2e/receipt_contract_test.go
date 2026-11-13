package e2e_test

// Ruling of 8 Nov 2026 on noema#330: the execution-receipt.v1 schema text decides what a receipt's logical_action holds (a PackId). This test reads the
// RECEIPT events the real controller wrote through the real stack, decodes each payload with the generated contract type (required members, no unknown
// member, no null), and checks logical_action against the PackId pattern read from common.v1.json: the generated type is a plain string and does not
// check patterns itself. REQ-EXE-007, REQ-AUD-001 (SoT §43.3). All data is SYNTHETIC.

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// packIDPattern reads the PackId pattern from the contract, so the test follows the schema text rather than a copy of it.
func packIDPattern(t *testing.T) *regexp.Regexp {
	t.Helper()
	raw, err := os.ReadFile("../../../../contracts/schemas/common.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Defs map[string]struct {
			Pattern string `json:"pattern"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	p := s.Defs["PackId"].Pattern
	if p == "" {
		t.Fatal("common.v1.json has no PackId pattern")
	}
	return regexp.MustCompile(p)
}

func TestEveryReceiptTheRealControllerWritesIsAValidExecutionReceipt(t *testing.T) {
	packID := packIDPattern(t)
	if packID.MatchString("ESCALATE_CASE") {
		t.Fatal("the PackId pattern accepts an action label; the check below would prove nothing")
	}
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
		if !packID.MatchString(string(r.LogicalAction)) {
			t.Errorf("logical_action %q is not a PackId (execution-receipt.v1, noema#330)", r.LogicalAction)
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
