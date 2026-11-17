package decide_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// REQ-TRU-001, REQ-API-001; INV-03, INV-07 (A254, interim; PERD 23.3): the core reason codes Trust may write are exactly the closed list of contracts/schemas/enums.v1.json
// (CoreReasonCode). A code in one and not the other is a Decision Record the contract refuses or a code the contract allows that Trust cannot write.
func TestTheCoreReasonCodesTrustAcceptsAreExactlyTheContractsClosedList(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/schemas/enums.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			CoreReasonCode struct {
				Enum []string `json:"enum"`
			} `json:"CoreReasonCode"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	listed := schema.Defs.CoreReasonCode.Enum
	seen := map[string]bool{}
	for _, code := range listed {
		if seen[code] {
			t.Errorf("the contract lists %s twice", code)
		}
		seen[code] = true
		if _, err := decide.Core(code); err != nil {
			t.Errorf("the contract lists %s and Trust refuses it as a core code", code)
		}
	}
	for _, code := range []string{"TARGET_FROZEN", "CLOCK_BREACHED_UNROUTED", "DISPATCH_BUDGET_EXHAUSTED", "DECLINED_BY_APPROVER"} {
		if !seen[code] {
			t.Errorf("A254: the contract does not list %s", code)
		}
	}
	// a code the contract does not list is refused
	if _, err := decide.Core("NOT_A_CODE"); err == nil {
		t.Error("Trust accepts a code no contract lists")
	}
	if len(listed) != 20 {
		t.Errorf("the contract lists %d core codes, want 20: the 17 of v1.2 and the three of A254 (the fourth name of A254 is DECLINED_BY_APPROVER, already one of the 17; the Architect confirms 20)", len(listed))
	}
}
