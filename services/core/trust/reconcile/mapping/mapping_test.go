package mapping_test

// ENV-04 slice 2: the evidence mapping, versioned pack data that turns a source's stored payload into rows of the reconciliation rule set's vocabulary (ruling A208, relayed;
// REQ-RES-001, REQ-CTX-001; INV-19, INV-21, AGENT-16). The rule set speaks of `debit`, `credit` and `switch` with enum fields; the Evidence Layer stores the ports' own words
// (`entries`, `semantic_status`, `credit_state`). Nothing mapped one to the other, so the adapter's Reconciler could not classify.
//
// Written before the package exists: this does not compile until `mapping.Parse` does. SYNTHETIC data only. Rules the tests pin: a field with no matching value is left UNSET,
// never defaulted (absence is never a negative fact, INV-19); the same input gives the same rows (INV-21); a mapping that is not whole fails at load, not at the first read.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

const (
	ledger      = "fin.src.core_ledger"
	switchSrc   = "fin.src.switch"
	destination = "fin.src.destination"
)

var pack = filepath.Join("..", "..", "..", "..", "..", "packs", "fintech")

func ruleSet(t *testing.T) *reconcile.RuleSet {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(pack, "recon_rules", "rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	rs, err := reconcile.ParseRuleSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func sources(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(pack, "..", "fintech_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		EvidenceSources []struct {
			SourceID string `json:"source_id"`
		} `json:"evidence_sources"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range p.EvidenceSources {
		out = append(out, s.SourceID)
	}
	return out
}

func realFile(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(pack, "evidence_mapping", "mapping.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func load(t *testing.T) *mapping.Mapping {
	t.Helper()
	m, err := mapping.Parse(realFile(t), ruleSet(t), sources(t))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// field returns the value of one field of the one row of an entity, and whether it is set.
func field(t *testing.T, rows []mapping.Row, entity, name string) (string, bool) {
	t.Helper()
	var found *mapping.Row
	for i := range rows {
		if rows[i].Entity == entity {
			if found != nil {
				t.Fatalf("two rows for entity %s", entity)
			}
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatalf("no row for entity %s in %v", entity, rows)
	}
	v, ok := found.Fields[name]
	return v.S, ok
}

func entries(es ...map[string]any) map[string]any {
	rows := make([]any, 0, len(es))
	for _, e := range es {
		rows = append(rows, e)
	}
	return map[string]any{"transaction_ref": "fin.transaction:TX1", "entries": rows}
}

func entry(kind, status string) map[string]any {
	return map[string]any{"entry_id": "E1", "account_ref": "fin.account:A1", "entry_type": kind, "amount_minor": 2000000, "currency": "NGN", "posting_status": status, "posted_at": "2026-11-14T09:00:00Z", "version": 1}
}

// --- the real pack data ------------------------------------------------------------------------------------------------------------------

func TestTheRealMappingLoadsAgainstTheRealRuleSetAndNamesItsVersion(t *testing.T) {
	m := load(t)
	if got := m.ID(); got != "fin.map.v1@1" {
		t.Errorf("ID = %q", got)
	}
	if got := m.RuleSet(); got != ruleSet(t).ID() {
		t.Errorf("RuleSet = %q, want %q (the reconciliation records both versions, INV-21)", got, ruleSet(t).ID())
	}
}

func TestADebitIsPostedOnlyWhenOneEntryIsADebitThatIsPosted(t *testing.T) {
	m := load(t)
	cases := map[string]struct {
		payload map[string]any
		want    string
		set     bool
	}{
		"a posted debit":              {entries(entry("DEBIT", "POSTED")), "POSTED", true},
		"a posted debit among others": {entries(entry("CREDIT", "POSTED"), entry("DEBIT", "POSTED")), "POSTED", true},
		"a pending debit":             {entries(entry("DEBIT", "PENDING")), "", false},
		"a reversed debit":            {entries(entry("DEBIT", "REVERSED")), "", false},
		"only a posted credit":        {entries(entry("CREDIT", "POSTED")), "", false},
		"no entries (an absence)":     {entries(), "", false},
	}
	for name, c := range cases {
		rows, err := m.Rows(ledger, c.payload)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, ok := field(t, rows, "debit", "state")
		if ok != c.set || got != c.want {
			t.Errorf("%s: debit.state = %q set=%v, want %q set=%v", name, got, ok, c.want, c.set)
		}
	}
}

func TestASwitchStatusIsMappedThroughAClosedTableAndAnythingElseIsUnset(t *testing.T) {
	m := load(t)
	for status, want := range map[string]string{"SUCCESS": "SUCCESSFUL", "REJECTED": "FAILED", "TIMED_OUT": "TIMED_OUT", "UNKNOWN": "EXECUTION_OUTCOME_UNKNOWN"} {
		rows, err := m.Rows(switchSrc, map[string]any{"semantic_status": status, "is_terminal": true})
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := field(t, rows, "switch", "state"); !ok || got != want {
			t.Errorf("%s: switch.state = %q set=%v, want %q", status, got, ok, want)
		}
	}
	for _, status := range []string{"ACCEPTED", "PENDING", "SOMETHING_NEW", "success", ""} {
		rows, err := m.Rows(switchSrc, map[string]any{"semantic_status": status})
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := field(t, rows, "switch", "state"); ok {
			t.Errorf("%q: switch.state = %q, want it left unset (an unlisted value is never guessed)", status, got)
		}
	}
}

func TestACreditStateIsMappedAndAnUnknownOrNotApplicableOneIsUnset(t *testing.T) {
	m := load(t)
	for state, want := range map[string]string{"CREDITED": "CREDITED", "NOT_CREDITED": "NOT_CREDITED", "PENDING": "PENDING"} {
		rows, err := m.Rows(destination, map[string]any{"credit_state": state, "settlement_state": "SETTLED"})
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := field(t, rows, "credit", "state"); !ok || got != want {
			t.Errorf("%s: credit.state = %q set=%v, want %q", state, got, ok, want)
		}
	}
	for _, state := range []string{"UNKNOWN", "NOT_APPLICABLE", "", "credited"} {
		rows, err := m.Rows(destination, map[string]any{"credit_state": state})
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := field(t, rows, "credit", "state"); ok {
			t.Errorf("%q: credit.state = %q, want unset", state, got)
		}
	}
}

func TestOnlyTheThreeEntitiesTheStandInsPortsProvideAreMapped(t *testing.T) {
	// No source gives a settlement or a reversal row today, so a classification that needs one is INSUFFICIENT_EVIDENCE, never a guess (the shape proposed on noema#579).
	m := load(t)
	for _, src := range []string{ledger, switchSrc, destination} {
		rows, err := m.Rows(src, map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.Entity == "settlement" || r.Entity == "reversal" {
				t.Errorf("%s gave a %s row", src, r.Entity)
			}
		}
	}
}

func TestASourceTheMappingDoesNotKnowGivesNoRows(t *testing.T) {
	rows, err := load(t).Rows("fin.src.case_system", map[string]any{"credit_state": "CREDITED"})
	if err != nil || len(rows) != 0 {
		t.Errorf("rows = %v, err = %v; want none and no error", rows, err)
	}
}

func TestAHostilePayloadNeverPanicsAndLeavesTheFieldUnset(t *testing.T) {
	m := load(t)
	payloads := []map[string]any{
		nil, {}, {"entries": "not a list"}, {"entries": []any{"not an object"}}, {"entries": []any{map[string]any{"entry_type": 7, "posting_status": []any{}}}},
		{"semantic_status": 7}, {"semantic_status": nil}, {"credit_state": map[string]any{}}, {"entries": []any{nil}},
	}
	for _, p := range payloads {
		for _, src := range []string{ledger, switchSrc, destination} {
			rows, err := m.Rows(src, p)
			if err != nil {
				t.Errorf("%s %v: %v", src, p, err)
			}
			for _, r := range rows {
				for name, v := range r.Fields {
					if v.S != "" {
						t.Errorf("%s %v: %s.%s = %q from a payload that holds no valid value", src, p, r.Entity, name, v.S)
					}
				}
			}
		}
	}
}

func TestTheSameInputGivesTheSameRows(t *testing.T) {
	m := load(t)
	p := entries(entry("DEBIT", "POSTED"), entry("CREDIT", "POSTED"))
	first, err1 := m.Rows(ledger, p)
	second, err2 := m.Rows(ledger, p)
	if err1 != nil || err2 != nil || !reflect.DeepEqual(first, second) {
		t.Errorf("rows differ between two calls: %v %v (%v %v)", first, second, err1, err2)
	}
}

func TestTheValuesAreTypedEnumValuesOfTheRuleSet(t *testing.T) {
	m := load(t)
	rows, _ := m.Rows(ledger, entries(entry("DEBIT", "POSTED")))
	for _, r := range rows {
		for name, v := range r.Fields {
			if v.Kind != predicate.KindEnum {
				t.Errorf("%s.%s has kind %v, want an enum value", r.Entity, name, v.Kind)
			}
		}
	}
}

// --- a mapping that is not whole fails at load ----------------------------------------------------------------------------------------------

func edit(t *testing.T, change func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(realFile(t), &m); err != nil {
		t.Fatal(err)
	}
	change(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func rowsOf(m map[string]any) []any { return m["rows"].([]any) }

func firstRowFor(m map[string]any, entity string) map[string]any {
	for _, r := range rowsOf(m) {
		if row := r.(map[string]any); row["entity"] == entity {
			return row
		}
	}
	return nil
}

func TestAMappingThatIsNotWholeIsRefusedAtLoad(t *testing.T) {
	rs, src := ruleSet(t), sources(t)
	bad := map[string]func(m map[string]any){
		"an unknown top-level member": func(m map[string]any) { m["surprise"] = 1 },
		"no rows":                     func(m map[string]any) { m["rows"] = []any{} },
		"another rule set":            func(m map[string]any) { m["ruleset"] = "fin.recon.rules@2" },
		"no rule set":                 func(m map[string]any) { delete(m, "ruleset") },
		"an unknown entity":           func(m map[string]any) { firstRowFor(m, "debit")["entity"] = "invented" },
		"an unknown field": func(m map[string]any) {
			firstRowFor(m, "debit")["fields"] = map[string]any{"invented": map[string]any{"path": "x", "map": map[string]any{"a": "POSTED"}}}
		},
		"a source the pack does not list": func(m map[string]any) { firstRowFor(m, "debit")["source"] = "fin.src.invented" },
		"a value that is not in the enum": func(m map[string]any) {
			firstRowFor(m, "switch")["fields"].(map[string]any)["state"].(map[string]any)["map"].(map[string]any)["SUCCESS"] = "WORKED"
		},
		"a quantifier value that is not in the enum": func(m map[string]any) {
			firstRowFor(m, "debit")["fields"].(map[string]any)["state"].(map[string]any)["value"] = "WORKED"
		},
		"an empty map": func(m map[string]any) {
			firstRowFor(m, "switch")["fields"].(map[string]any)["state"].(map[string]any)["map"] = map[string]any{}
		},
		"a map key that is empty": func(m map[string]any) {
			firstRowFor(m, "switch")["fields"].(map[string]any)["state"].(map[string]any)["map"].(map[string]any)[""] = "FAILED"
		},
		"a field with both forms": func(m map[string]any) {
			firstRowFor(m, "switch")["fields"].(map[string]any)["state"].(map[string]any)["any_of"] = "entries"
		},
		"a field with neither form": func(m map[string]any) {
			firstRowFor(m, "switch")["fields"].(map[string]any)["state"] = map[string]any{}
		},
		"an unknown member in a field": func(m map[string]any) {
			firstRowFor(m, "switch")["fields"].(map[string]any)["state"].(map[string]any)["default"] = "FAILED"
		},
		"a row repeated": func(m map[string]any) { m["rows"] = append(rowsOf(m), firstRowFor(m, "switch")) },
		"a field that is not an enum": func(m map[string]any) {
			firstRowFor(m, "debit")["fields"] = map[string]any{"amount": map[string]any{"path": "amount_minor", "map": map[string]any{"1": "2"}}}
		},
		"a match naming no field": func(m map[string]any) {
			firstRowFor(m, "debit")["fields"].(map[string]any)["state"].(map[string]any)["where"] = map[string]any{}
		},
		"a mapping id that is not a mapping id": func(m map[string]any) { m["mapping_id"] = "" },
		"a version that is not positive":        func(m map[string]any) { m["version"] = 0 },
	}
	for name, change := range bad {
		if _, err := mapping.Parse(edit(t, change), rs, src); err == nil {
			t.Errorf("%s: a mapping that is not whole was accepted", name)
		}
	}
	for name, data := range map[string][]byte{"not JSON": []byte("{"), "two documents": append(realFile(t), realFile(t)...), "an array": []byte("[]"), "empty": nil} {
		if _, err := mapping.Parse(data, rs, src); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAMappingNamesItsOwnRuleSetOnly(t *testing.T) {
	// A mapping written for one rule set must not be used with another: the rows' meaning is the rule set's (A208).
	rs := ruleSet(t)
	var spec map[string]any
	if err := json.Unmarshal(realFile(t), &spec); err != nil {
		t.Fatal(err)
	}
	if got, want := spec["ruleset"], rs.ID(); got != want {
		t.Errorf("the file names %v, the rule set is %v", got, want)
	}
	if !strings.Contains(string(realFile(t)), "SYNTHETIC") {
		t.Error("the pack data is not labelled SYNTHETIC (AGENT-25)")
	}
}

func TestARepeatedMemberAtAnyDepthIsRefused(t *testing.T) {
	rs, src := ruleSet(t), sources(t)
	good := string(realFile(t))
	for name, data := range map[string]string{
		"at the top": strings.Replace(good, `"version": 1,`, `"version": 1, "version": 2,`, 1),
		"in a field": strings.Replace(good, `"value": "POSTED"}`, `"value": "POSTED", "value": "POSTED"}`, 1),
		"in a map":   strings.Replace(good, `"SUCCESS": "SUCCESSFUL",`, `"SUCCESS": "SUCCESSFUL", "SUCCESS": "FAILED",`, 1),
		"in a where": strings.Replace(good, `"entry_type": "DEBIT",`, `"entry_type": "DEBIT", "entry_type": "CREDIT",`, 1),
	} {
		if data == good {
			t.Fatalf("%s: the edit did not change the document", name)
		}
		if _, err := mapping.Parse([]byte(data), rs, src); err == nil {
			t.Errorf("%s: a repeated member was accepted", name)
		}
	}
}
