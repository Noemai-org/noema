package reconcile_test

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

// REQ-RES-001, REQ-API-001; OD-P-10, A272 (the transaction classification is its own closed list, so an Execution Receipt state such as OUTCOME_UNKNOWN cannot be assigned to it):
// the classifications Trust's reconciliation produces are exactly the contract's TransactionClassification.
func TestTheClassificationsOfTheReconcilerAreExactlyTheContractsTransactionClassification(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/schemas/enums.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	listed := schema.Defs["TransactionClassification"].Enum
	if len(listed) == 0 {
		t.Fatal("enums.v1.json has no closed list TransactionClassification")
	}
	have := []string{}
	for _, c := range []reconcile.Classification{reconcile.SuccessConfirmed, reconcile.RejectedConfirmed, reconcile.DebitPostedCreditUnconfirmed, reconcile.SettlementPending,
		reconcile.ReversalPending, reconcile.ReversedConfirmed, reconcile.PossibleDuplicate, reconcile.Partial, reconcile.ExecutionOutcomeUnknown, reconcile.ContradictoryState,
		reconcile.EventOrderUncertain, reconcile.InsufficientEvidence} {
		have = append(have, string(c))
	}
	sort.Strings(have)
	want := append([]string(nil), listed...)
	sort.Strings(want)
	if len(have) != len(want) {
		t.Fatalf("the reconciler has %d classifications, the contract lists %d: %v / %v", len(have), len(want), have, want)
	}
	for i := range have {
		if have[i] != want[i] {
			t.Errorf("classification %d: the reconciler has %s, the contract %s", i, have[i], want[i])
		}
	}
}
