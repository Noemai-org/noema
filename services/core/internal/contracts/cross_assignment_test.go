package contracts_test

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// CON-06 (OD-P-10, DEC-2026-10-17 section 3; REQ-API-001): every closed list of enums.v1.json is its own type, and a value of one list is refused by every other list that does not
// hold the same word. The names stay as the SoT writes them (OUTCOME_UNKNOWN is an Execution Receipt state, EXECUTION_OUTCOME_UNKNOWN a transaction classification; S0 to S3 are
// finding severities, S3_financial_pii a data sensitivity). The generated Go types do not check patterns (tools/codegen/README.md), so DataSensitivity, which is a pattern and not
// a list, is covered by the Python test (services/model/tests/test_cross_assignment.py) and by the contract validator; the registry below names every closed list, so a new one
// fails here until it is added.

func decoderOf[T any](raw []byte) error {
	var v T
	return json.Unmarshal(raw, &v)
}

var closedEnums = map[string]func([]byte) error{
	"RetrySafety":               decoderOf[contracts.EnumsV1RetrySafety],
	"ConcurrencyControl":        decoderOf[contracts.EnumsV1ConcurrencyControl],
	"ConsequenceLevel":          decoderOf[contracts.EnumsV1ConsequenceLevel],
	"Irreversibility":           decoderOf[contracts.EnumsV1Irreversibility],
	"EvaluationTime":            decoderOf[contracts.EnumsV1EvaluationTime],
	"IntentStatus":              decoderOf[contracts.EnumsV1IntentStatus],
	"DecisionOutcome":           decoderOf[contracts.EnumsV1DecisionOutcome],
	"AuthorityResult":           decoderOf[contracts.EnumsV1AuthorityResult],
	"CoreReasonCode":            decoderOf[contracts.EnumsV1CoreReasonCode],
	"LimitSetBy":                decoderOf[contracts.EnumsV1LimitSetBy],
	"ConfirmationResponse":      decoderOf[contracts.EnumsV1ConfirmationResponse],
	"ConfirmationTerminalState": decoderOf[contracts.EnumsV1ConfirmationTerminalState],
	"FactStatus":                decoderOf[contracts.EnumsV1FactStatus],
	"ExecutionState":            decoderOf[contracts.EnumsV1ExecutionState],
	"TransactionClassification": decoderOf[contracts.EnumsV1TransactionClassification],
	"Verdict":                   decoderOf[contracts.EnumsV1Verdict],
	"FindingSeverity":           decoderOf[contracts.EnumsV1FindingSeverity],
	"Mode":                      decoderOf[contracts.EnumsV1Mode],
}

func enumValues(t *testing.T) map[string][]string {
	t.Helper()
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
	out := map[string][]string{}
	for name, d := range schema.Defs {
		if len(d.Enum) > 0 {
			out[name] = d.Enum
		}
	}
	return out
}

func quoted(s string) []byte { b, _ := json.Marshal(s); return b }

func TestEveryClosedListOfTheEnumsContractIsRegisteredForTheCrossAssignmentTest(t *testing.T) {
	for name := range enumValues(t) {
		if _, ok := closedEnums[name]; !ok {
			t.Errorf("enums.v1.json#$defs/%s is a closed list with no entry in closedEnums (add its generated type)", name)
		}
	}
	for name := range closedEnums {
		if _, ok := enumValues(t)[name]; !ok {
			t.Errorf("closedEnums names %s, which enums.v1.json no longer holds as a closed list", name)
		}
	}
}

func TestAValueOfOneClosedListIsRefusedByEveryOtherListThatDoesNotHoldIt(t *testing.T) {
	values := enumValues(t)
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sort.Strings(names)
	held := map[string]map[string]bool{}
	for n, vs := range values {
		held[n] = map[string]bool{}
		for _, v := range vs {
			held[n][v] = true
		}
	}
	checked := 0
	for _, a := range names {
		for _, v := range values[a] {
			for _, b := range names {
				if a == b || held[b][v] {
					continue // the same word in two lists is the same word, by the SoT's own naming
				}
				checked++
				if err := closedEnums[b](quoted(v)); err == nil {
					t.Errorf("%s value %q was accepted as a %s", a, v, b)
				}
				if err := closedEnums[a](quoted(v)); err != nil {
					t.Errorf("%s refuses its own value %q: %v", a, v, err)
				}
			}
		}
	}
	if checked < 500 {
		t.Fatalf("only %d cross-assignments were tried: the matrix is not reading the contract", checked)
	}
}

// The two pairs of OD-P-10 by name, so a reader sees them.
func TestTheSimilarNamesOfOD_P_10AreNotInterchangeable(t *testing.T) {
	exec, sev := closedEnums["ExecutionState"], closedEnums["FindingSeverity"]
	if err := exec(quoted("OUTCOME_UNKNOWN")); err != nil {
		t.Errorf("ExecutionState refuses OUTCOME_UNKNOWN: %v", err)
	}
	if err := exec(quoted("EXECUTION_OUTCOME_UNKNOWN")); err == nil {
		t.Error("ExecutionState accepted EXECUTION_OUTCOME_UNKNOWN, the synthetic transaction classification")
	}
	for _, s := range []string{"S0", "S1", "S2", "S3"} {
		if err := sev(quoted(s)); err != nil {
			t.Errorf("FindingSeverity refuses %s: %v", s, err)
		}
	}
	for _, s := range []string{"S3_financial_pii", "S4", "S3 "} {
		if err := sev(quoted(s)); err == nil {
			t.Errorf("FindingSeverity accepted the data sensitivity %q", s)
		}
	}
}
