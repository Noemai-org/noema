package reconcile_test

// PCK-05 (A674, A659; REQ-RES-001, REQ-AG-001; INV-19, INV-21, AGENT-16): from rule set version 2 every compatibility-matrix entry carries one `convergence_window_seconds`, a positive
// whole number; a version 2 rule set with an entry that has none, or a value that is not above zero, never loads (fail closed). Version 1 parses as it did: it has no window, and a
// version 1 file that names one is still an unknown field. The shipped fintech rule set is version 2 with exactly the three computable pairs of A674, 300 seconds each (SYNTHETIC);
// the reversal pair is not in the active list (Tushe has not stated the reversal shape). SYNTHETIC.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

// ruleSetWith builds a minimal rule set of the given version whose matrix is the given JSON array text.
func ruleSetWith(version int, matrix string) []byte {
	return []byte(fmt.Sprintf(`{"ruleset_id":"zz.rules","version":%d,"label":"SYNTHETIC",
"entities":{"switch":{"state":{"type":"Enum","values":["FAILED","SUCCESSFUL"]}},"credit":{"state":{"type":"Enum","values":["CREDITED","NOT_CREDITED"]}}},
"incompatible":%s,"single_valued":[],"derived":[],
"rules":[{"id":"R1","classification":"CONTRADICTORY_STATE","uses":["contradiction"],"condition":"exists(contradiction where true)"}],
"fallback":{"id":"R0","classification":"INSUFFICIENT_EVIDENCE","uses":[]},
"certainty":{"SUCCESS_CONFIRMED":2,"REJECTED_CONFIRMED":2,"REVERSED_CONFIRMED":2,"CONTRADICTORY_STATE":2,"POSSIBLE_DUPLICATE":2,"DEBIT_POSTED_CREDIT_UNCONFIRMED":1,"SETTLEMENT_PENDING":1,
"REVERSAL_PENDING":1,"EXECUTION_OUTCOME_UNKNOWN":1,"PARTIAL":1,"EVENT_ORDER_UNCERTAIN":1,"INSUFFICIENT_EVIDENCE":1},"pending_markers":[]}`, version, matrix))
}

func entryWith(window string) string {
	w := ""
	if window != "" {
		w = `,"convergence_window_seconds":` + window
	}
	return `{"label":"switch_failed_credit_credited","a":{"entity":"switch","field":"state","value":"FAILED"},"b":{"entity":"credit","field":"state","value":"CREDITED"}` + w + `}`
}

// PCK-05, A674 (2): a version 2 entry names its window, and the loaded matrix gives it back.
func TestAVersion2EntryCarriesItsWindowAndTheMatrixGivesItBack(t *testing.T) {
	rs, err := reconcile.ParseRuleSet(ruleSetWith(2, "["+entryWith("300")+"]"))
	if err != nil {
		t.Fatalf("a version 2 rule set with a window did not load: %v", err)
	}
	m := rs.Matrix()
	if len(m) != 1 || m[0].WindowSeconds != 300 {
		t.Fatalf("matrix %+v, want one entry with WindowSeconds 300", m)
	}
}

// PCK-05, A674 (2), A659: version 2 REQUIRES a window above zero. Every shape that is not one is refused at load.
func TestAVersion2EntryWithoutAPositiveWholeWindowNeverLoads(t *testing.T) {
	for name, window := range map[string]string{
		"missing": "", "zero": "0", "negative": "-300", "a fraction": "300.5", "a string": `"300"`, "null": "null", "a boolean": "true",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := reconcile.ParseRuleSet(ruleSetWith(2, "["+entryWith(window)+"]")); err == nil {
				t.Fatalf("a version 2 entry with window %q loaded", window)
			}
		})
	}
	t.Run("the second entry has none", func(t *testing.T) {
		second := strings.Replace(entryWith("300"), "switch_failed_credit_credited", "other", 1)
		bad := strings.Replace(second, `,"convergence_window_seconds":300`, "", 1)
		if _, err := reconcile.ParseRuleSet(ruleSetWith(2, "["+entryWith("300")+","+bad+"]")); err == nil {
			t.Fatal("a version 2 rule set loaded with one entry that has no window")
		}
	})
	t.Run("a later version needs it too", func(t *testing.T) {
		if _, err := reconcile.ParseRuleSet(ruleSetWith(3, "["+entryWith("")+"]")); err == nil {
			t.Fatal("a version 3 entry with no window loaded")
		}
		if _, err := reconcile.ParseRuleSet(ruleSetWith(3, "["+entryWith("300")+"]")); err != nil {
			t.Fatalf("a version 3 entry with a window did not load: %v", err)
		}
	})
}

// PCK-05, A659: version 1 parsing is unchanged. No window is needed, and the member is still an unknown field there.
func TestVersion1ParsingIsUnchanged(t *testing.T) {
	rs, err := reconcile.ParseRuleSet(ruleSetWith(1, "["+entryWith("")+"]"))
	if err != nil {
		t.Fatalf("a version 1 rule set with no window no longer loads: %v", err)
	}
	if m := rs.Matrix(); len(m) != 1 || m[0].WindowSeconds != 0 {
		t.Fatalf("matrix %+v, want one entry with no window", m)
	}
	if _, err := reconcile.ParseRuleSet(ruleSetWith(1, "["+entryWith("300")+"]")); err == nil {
		t.Fatal("a version 1 entry that names a window loaded; the member is unknown in version 1")
	}
}

// PCK-05, A674 (3): the shipped rule set is version 2, with the three computable pairs only, each window 300 seconds (the smaller freshness_sla_seconds of the pair's two sources, SYNTHETIC).
// The reversal pair is not in the active list. The entity-to-source link is not in the rule file (A674 (1)), so the entry has no member for it: the strict parse is the guard.
func TestTheShippedRuleSetIsVersion2WithTheThreeComputablePairs(t *testing.T) {
	rs := theRuleSet(t)
	if rs.ID() != "fin.recon.rules@2" {
		t.Fatalf("the shipped rule set is %s, want fin.recon.rules@2", rs.ID())
	}
	want := []string{"switch_failed_credit_credited", "credit_without_debit", "switch_successful_credit_not_credited"}
	got := rs.MatrixLabels()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("matrix labels %v, want exactly %v (no reversal pair, A674)", got, want)
	}
	for _, m := range rs.Matrix() {
		if m.WindowSeconds != 300 {
			t.Errorf("%s: window %d, want 300", m.Label, m.WindowSeconds)
		}
	}
}
