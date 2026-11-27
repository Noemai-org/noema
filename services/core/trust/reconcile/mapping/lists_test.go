package mapping_test

// TRU-07: the list entities of the evidence mapping, fin.map.v1@2 (ruling A438, interim; A407, A411): a `rows_from` construct for list entities, a completeness marker
// (`complete`) so that "no case" is concluded only from a list marked complete (anything else is UNKNOWN, never "none"; INV-19), the case and complaint_ref rows, and the clock
// anchor complaint_ref.effective_at. Written before the code: this file fails until ListRows, WithScope and the version 2 file exist. SYNTHETIC data only.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

const caseSystem = "fin.src.case_system"

// packScope is the pack's own declaration of the entities of its evidence sources (evidence_descriptors of fintech_v1.json), read the way a composition root would.
func packScope(t *testing.T) predicate.Scope {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(pack, "..", "fintech_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Descriptors []struct {
			Entity string `json:"entity"`
			Fields map[string]struct {
				Type   string   `json:"type"`
				Values []string `json:"values"`
			} `json:"fields"`
		} `json:"evidence_descriptors"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]predicate.Type{"Reference": predicate.Reference, "Integer": predicate.Integer, "String": predicate.String, "Timestamp": predicate.Timestamp, "Boolean": predicate.Boolean}
	sc := predicate.Scope{Entities: map[string]map[string]predicate.Type{}, Variables: map[string]predicate.Type{}}
	for _, d := range p.Descriptors {
		sc.Entities[d.Entity] = map[string]predicate.Type{}
		for name, f := range d.Fields {
			if f.Type == "Enum" {
				sc.Entities[d.Entity][name] = predicate.EnumOf(f.Values...)
			} else {
				sc.Entities[d.Entity][name] = kinds[f.Type]
			}
		}
	}
	return sc
}

// listDoc is a version 2 mapping: one rule-set row and two list entities of the case system.
func listDoc(rsID string, mutate func(string) string) []byte {
	doc := `{"mapping_id":"fin.map.v1","version":2,"label":"SYNTHETIC","ruleset":"` + rsID + `","note":"n","rows":[
 {"entity":"switch","source":"fin.src.switch","fields":{"state":{"path":"semantic_status","map":{"SUCCESS":"SUCCESSFUL"}}}},
 {"entity":"case","source":"fin.src.case_system","rows_from":"items","complete":"complete","fields":{
   "id":{"path":"case_id"},"subject":{"path":"transaction_ref"},"status":{"path":"status","map":{"OPEN":"OPEN","ROUTED":"ROUTED"}},
   "created_by_intent":{"path":"created_by_intent"},"owner_queue":{"path":"owner_queue"},"epoch":{"path":"epoch"}}},
 {"entity":"complaint_ref","source":"fin.src.case_system","rows_from":"items","complete":"complete","fields":{"effective_at":{"path":"clock_started_at"}}}]}`
	if mutate != nil {
		doc = mutate(doc)
	}
	return []byte(doc)
}

func parseLists(t *testing.T, mutate func(string) string) (*mapping.Mapping, error) {
	t.Helper()
	return mapping.Parse(listDoc(ruleSet(t).ID(), mutate), ruleSet(t), append(sources(t), caseSystem), "fin.map.v1@2", limits, mapping.WithScope(packScope(t)))
}

func mustLists(t *testing.T) *mapping.Mapping {
	t.Helper()
	m, err := parseLists(t, nil)
	if err != nil {
		t.Fatalf("a version 2 mapping with list entities must load: %v", err)
	}
	return m
}

func casePage(complete any, items ...map[string]any) map[string]any {
	list := make([]any, 0, len(items))
	for _, it := range items {
		list = append(list, it)
	}
	page := map[string]any{"items": list}
	if complete != nil {
		page["complete"] = complete
	}
	return page
}

func aCase(id, status string) map[string]any {
	return map[string]any{"case_id": id, "reference": "R", "status": status, "owner_queue": "fin.queue.payments_ops", "priority": "NORMAL", "transaction_ref": "fin.transaction:TX1",
		"created_by_intent": "int_aa", "epoch": json.Number("3"), "created_at": "2026-11-14T09:00:00Z"}
}

// --- loading -------------------------------------------------------------------------------------------------------------------------------

func TestAListEntityLoadsAgainstTheScopeItIsGivenAndStillNamesItsVersion(t *testing.T) {
	m := mustLists(t)
	if m.ID() != "fin.map.v1@2" {
		t.Errorf("ID = %q", m.ID())
	}
}

func TestAMappingWhoseListRowIsNotWholeOrNotInAScopeIsRefusedAtLoad(t *testing.T) {
	rs := ruleSet(t)
	if _, err := parseLists(t, nil); err != nil {
		t.Fatalf("control: the unmutated document must load, or every refusal below proves nothing: %v", err)
	}
	bad := map[string]func(string) string{
		"complete marker missing": func(s string) string {
			return strings.Replace(s, `"rows_from":"items","complete":"complete",`, `"rows_from":"items",`, 1)
		},
		"rows_from empty": func(s string) string {
			return strings.Replace(s, `"rows_from":"items","complete":"complete","fields":{"effective_at"`, `"rows_from":"","complete":"complete","fields":{"effective_at"`, 1)
		},
		"complete without rows_from": func(s string) string {
			return strings.Replace(s, `"rows_from":"items","complete":"complete","fields":{"effective_at"`, `"complete":"complete","fields":{"effective_at"`, 1)
		},
		"complete names the list itself": func(s string) string {
			return strings.Replace(s, `"rows_from":"items","complete":"complete","fields":{"effective_at"`, `"rows_from":"items","complete":"items","fields":{"effective_at"`, 1)
		},
		"an entity of neither scope": func(s string) string { return strings.Replace(s, `"entity":"complaint_ref"`, `"entity":"ghost"`, 1) },
		"a field the entity does not have": func(s string) string {
			return strings.Replace(s, `"effective_at":{"path":"clock_started_at"}`, `"ghost":{"path":"clock_started_at"}`, 1)
		},
		"an enum field with no map": func(s string) string {
			return strings.Replace(s, `"status":{"path":"status","map":{"OPEN":"OPEN","ROUTED":"ROUTED"}}`, `"status":{"path":"status"}`, 1)
		},
		"a map to a value the enum lacks": func(s string) string { return strings.Replace(s, `"ROUTED":"ROUTED"}`, `"ROUTED":"NOPE"}`, 1) },
		"a copy field with a map": func(s string) string {
			return strings.Replace(s, `"epoch":{"path":"epoch"}`, `"epoch":{"path":"epoch","map":{"1":"1"}}`, 1)
		},
		"a quantifier in a list row": func(s string) string {
			return strings.Replace(s, `"epoch":{"path":"epoch"}`, `"epoch":{"any_of":"x","where":{"a":"b"},"value":"OPEN"}`, 1)
		},
		"the same list entity mapped twice": func(s string) string { return strings.Replace(s, `"entity":"complaint_ref"`, `"entity":"case"`, 1) },
		"a source that is not the pack's": func(s string) string {
			return strings.Replace(s, `"source":"fin.src.case_system","rows_from":"items","complete":"complete","fields":{"effective_at"`, `"source":"fin.src.nowhere","rows_from":"items","complete":"complete","fields":{"effective_at"`, 1)
		},
	}
	for name, mutate := range bad {
		if _, err := parseLists(t, mutate); err == nil {
			t.Errorf("%s: loaded, want a refusal", name)
		}
	}
	// the same document without the extra scope names entities the rule set does not have: refused, never half-loaded
	if _, err := mapping.Parse(listDoc(rs.ID(), nil), rs, append(sources(t), caseSystem), "fin.map.v1@2", limits); err == nil {
		t.Error("a list entity with no scope to type it in loaded")
	}
	// an entity of the rule set cannot also be a list entity of the extra scope: one entity, one meaning
	both := predicate.Scope{Entities: map[string]map[string]predicate.Type{"switch": {"state": predicate.EnumOf("SUCCESSFUL")}}}
	listOfSwitch := `{"mapping_id":"fin.map.v1","version":2,"label":"SYNTHETIC","ruleset":"` + rs.ID() + `","note":"n","rows":[
 {"entity":"switch","source":"fin.src.case_system","rows_from":"items","complete":"complete","fields":{"state":{"path":"status","map":{"OPEN":"SUCCESSFUL"}}}}]}`
	if _, err := mapping.Parse([]byte(listOfSwitch), rs, append(sources(t), caseSystem), "fin.map.v1@2", limits, mapping.WithScope(both)); err == nil {
		t.Error("an entity that is in both scopes loaded")
	}
	// the same document with the entity in the extra scope only is whole: the refusal above is for the right reason
	other := predicate.Scope{Entities: map[string]map[string]predicate.Type{"ghost": {"state": predicate.EnumOf("SUCCESSFUL")}}}
	if _, err := mapping.Parse([]byte(strings.Replace(listOfSwitch, `"entity":"switch"`, `"entity":"ghost"`, 1)), rs, append(sources(t), caseSystem), "fin.map.v1@2", limits, mapping.WithScope(other)); err != nil {
		t.Errorf("control: the same list row of an entity of the extra scope only must load: %v", err)
	}
	// complete belongs to a list entity; on a plain row it is refused, not ignored
	plain := `{"mapping_id":"fin.map.v1","version":2,"label":"SYNTHETIC","ruleset":"` + rs.ID() + `","note":"n","rows":[
 {"entity":"switch","source":"fin.src.switch","complete":"complete","fields":{"state":{"path":"semantic_status","map":{"SUCCESS":"SUCCESSFUL"}}}}]}`
	if _, err := mapping.Parse([]byte(plain), rs, sources(t), "fin.map.v1@2", limits, mapping.WithScope(packScope(t))); err == nil {
		t.Error("a plain row with a complete marker loaded")
	}
	if _, err := mapping.Parse([]byte(strings.Replace(plain, `"complete":"complete",`, ``, 1)), rs, sources(t), "fin.map.v1@2", limits, mapping.WithScope(packScope(t))); err != nil {
		t.Errorf("control: the plain row without the marker must load: %v", err)
	}
}

// --- "no case" only from a list marked complete (A438; INV-19) --------------------------------------------------------------------------

func TestAnEmptyListMarkedCompleteIsNoCaseAndAnythingElseIsUnknown(t *testing.T) {
	m := mustLists(t)
	rows, err := m.ListRows(caseSystem, "case", casePage(true))
	if err != nil || len(rows) != 0 {
		t.Fatalf("an empty page marked complete = no case: got %v, %v", rows, err)
	}
	notComplete := map[string]map[string]any{
		"false and empty":      casePage(false),
		"false with a case":    casePage(false, aCase("case_1", "OPEN")),
		"marker missing empty": casePage(nil),
		"marker missing, case": casePage(nil, aCase("case_1", "OPEN")),
		"marker a string":      casePage("true"),
		"marker a number":      casePage(json.Number("1")),
		"marker null":          casePage(nil), // replaced below
	}
	notComplete["marker null"]["complete"] = nil
	for name, page := range notComplete {
		rows, err := m.ListRows(caseSystem, "case", page)
		if !errors.Is(err, mapping.ErrIncomplete) || rows != nil {
			t.Errorf("%s: rows %v, err %v; want ErrIncomplete and no rows (UNKNOWN, never none)", name, rows, err)
		}
	}
}

func TestAPayloadThatIsNotTheListTheMappingNamesIsRefusedNotTakenForNone(t *testing.T) {
	m := mustLists(t)
	bad := map[string]map[string]any{
		"no list":             {"complete": true},
		"list is a string":    {"items": "none", "complete": true},
		"list is an object":   {"items": map[string]any{}, "complete": true},
		"element is a string": {"items": []any{"case_1"}, "complete": true},
		"element is null":     {"items": []any{nil}, "complete": true},
		"payload empty":       {},
	}
	for name, payload := range bad {
		rows, err := m.ListRows(caseSystem, "case", payload)
		if err == nil || rows != nil || (!errors.Is(err, mapping.ErrListShape) && !errors.Is(err, mapping.ErrIncomplete)) {
			t.Errorf("%s: rows %v, err %v; want a refusal and no rows", name, rows, err)
		}
	}
}

func TestAListEntityIsAddressedByItsSourceAndName(t *testing.T) {
	m := mustLists(t)
	for _, c := range []struct{ source, entity string }{{caseSystem, "switch"}, {caseSystem, "ghost"}, {"fin.src.switch", "case"}, {"", "case"}, {caseSystem, ""}} {
		if rows, err := m.ListRows(c.source, c.entity, casePage(true)); !errors.Is(err, mapping.ErrNoSuchList) || rows != nil {
			t.Errorf("%+v: rows %v, err %v; want ErrNoSuchList", c, rows, err)
		}
	}
}

// --- the rows --------------------------------------------------------------------------------------------------------------------------------

func TestTheCaseRowsCarryTheirTypedFieldsInThePagesOrder(t *testing.T) {
	m := mustLists(t)
	rows, err := m.ListRows(caseSystem, "case", casePage(true, aCase("case_1", "OPEN"), aCase("case_2", "ROUTED")))
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %v, err %v", rows, err)
	}
	want := predicate.Row{"id": predicate.Ref("case_1"), "subject": predicate.Ref("fin.transaction:TX1"), "status": predicate.Enum("OPEN"), "created_by_intent": predicate.Ref("int_aa"),
		"owner_queue": predicate.Ref("fin.queue.payments_ops"), "epoch": predicate.Int(3)}
	if !reflect.DeepEqual(rows[0], want) {
		t.Errorf("row 0 = %v, want %v", rows[0], want)
	}
	if rows[1]["id"] != predicate.Ref("case_2") || rows[1]["status"] != predicate.Enum("ROUTED") {
		t.Errorf("row 1 = %v", rows[1])
	}
	again, _ := m.ListRows(caseSystem, "case", casePage(true, aCase("case_1", "OPEN"), aCase("case_2", "ROUTED")))
	if !reflect.DeepEqual(rows, again) {
		t.Error("the same page gave other rows (INV-21)")
	}
}

func TestAnIntegerFieldIsAWholeNumberInRangeInAnyOfTheDecodersShapes(t *testing.T) {
	m := mustLists(t)
	ok := map[string]any{"json.Number": json.Number("7"), "int": 7, "int64": int64(7), "json.Number with leading zeros": json.Number("007")}
	for name, v := range ok {
		c := aCase("case_1", "OPEN")
		c["epoch"] = v
		rows, err := m.ListRows(caseSystem, "case", casePage(true, c))
		if err != nil || len(rows) != 1 || rows[0]["epoch"] != predicate.Int(7) {
			t.Errorf("%s: %v, %v", name, rows, err)
		}
	}
	for name, v := range map[string]any{"a string": "3", "a bool": true, "null": nil, "json.Number fraction": json.Number("2.5"), "json.Number exponent": json.Number("1e2"), "too large": json.Number("1152921504606846976"), "just over the bound": json.Number("9007199254740992"), "empty": json.Number("")} {
		c := aCase("case_1", "OPEN")
		c["epoch"] = v
		rows, err := m.ListRows(caseSystem, "case", casePage(true, c))
		if err != nil || len(rows) != 1 {
			t.Errorf("%s: %v, %v", name, rows, err)
			continue
		}
		if _, set := rows[0]["epoch"]; set {
			t.Errorf("%s: epoch was set to %v; a value that is not a whole number in range is left UNSET, never defaulted (INV-19)", name, rows[0]["epoch"])
		}
	}
}

func TestAFieldTheElementDoesNotGiveOrGivesAsAnotherShapeIsLeftUnset(t *testing.T) {
	m := mustLists(t)
	bad := map[string]func(map[string]any){
		"status not listed":     func(c map[string]any) { c["status"] = "CLOSED" },
		"status not a string":   func(c map[string]any) { c["status"] = 1 },
		"id missing":            func(c map[string]any) { delete(c, "case_id") },
		"id empty":              func(c map[string]any) { c["case_id"] = "" },
		"id a number":           func(c map[string]any) { c["case_id"] = json.Number("5") },
		"id has a control char": func(c map[string]any) { c["case_id"] = "case\n1" },
		"queue empty":           func(c map[string]any) { c["owner_queue"] = "" },
		"intent missing":        func(c map[string]any) { delete(c, "created_by_intent") },
	}
	fieldOf := map[string]string{"status not listed": "status", "status not a string": "status", "id missing": "id", "id empty": "id", "id a number": "id", "id has a control char": "id", "queue empty": "owner_queue", "intent missing": "created_by_intent"}
	for name, mutate := range bad {
		c := aCase("case_1", "OPEN")
		mutate(c)
		rows, err := m.ListRows(caseSystem, "case", casePage(true, c))
		if err != nil || len(rows) != 1 {
			t.Errorf("%s: %v, %v", name, rows, err)
			continue
		}
		if _, set := rows[0][fieldOf[name]]; set {
			t.Errorf("%s: %s was set; it must be left UNSET", name, fieldOf[name])
		}
		if len(rows[0]) != 5 {
			t.Errorf("%s: the other five fields must stay: %v", name, rows[0])
		}
	}
}

func TestTheClockAnchorIsAnRFC3339InstantOrUnset(t *testing.T) {
	m := mustLists(t)
	at := func(s any) predicate.Value {
		rows, err := m.ListRows(caseSystem, "complaint_ref", casePage(true, map[string]any{"complaint_ref": "cmp_1", "case_id": "case_1", "clock_started_at": s}))
		if err != nil || len(rows) != 1 {
			t.Fatalf("%v: %v, %v", s, rows, err)
		}
		return rows[0]["effective_at"]
	}
	utc := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC).Unix()
	if got := at("2026-11-14T09:00:00Z"); got != predicate.Time(utc) {
		t.Errorf("Z = %v", got)
	}
	if got := at("2026-11-14T10:00:00+01:00"); got != predicate.Time(utc) {
		t.Errorf("+01:00 = %v, want the same instant", got)
	}
	for _, bad := range []any{"2026-11-14", "2026-11-14T09:00:00", "", "yesterday", json.Number("1"), nil} {
		if got := at(bad); !got.IsNull() {
			t.Errorf("%v gave %v; an instant the mapping cannot read is left UNSET, never defaulted", bad, got)
		}
	}
}

// --- the rule set's rows are untouched -------------------------------------------------------------------------------------------------------

func TestRowsOfTheRuleSetSkipTheListEntitiesAndStayTheSame(t *testing.T) {
	m := mustLists(t)
	rows, err := m.Rows(caseSystem, casePage(true, aCase("case_1", "OPEN")))
	if err != nil || len(rows) != 0 {
		t.Errorf("a list entity is not a reconciliation row: %v, %v (the Reconciler errors on an entity outside its rule set)", rows, err)
	}
	rows, err = m.Rows("fin.src.switch", map[string]any{"semantic_status": "SUCCESS"})
	if err != nil || len(rows) != 1 || rows[0].Entity != "switch" {
		t.Errorf("the switch row changed: %v, %v", rows, err)
	}
}

// --- the real pack data ----------------------------------------------------------------------------------------------------------------------

func TestThePackDeclaresTheClockAnchorOfAComplaintRef(t *testing.T) {
	ty, ok := packScope(t).Entities["complaint_ref"]["effective_at"]
	if !ok || ty.Kind != predicate.KindTimestamp {
		t.Errorf("complaint_ref.effective_at in the pack's evidence_descriptors = %v, %v; A438 makes it the clock anchor, a Timestamp", ty, ok)
	}
}

func TestTheRealMappingKeepsTheCaseAndComplaintRowsOfVersion2(t *testing.T) {
	m, err := mapping.Parse(realFile(t), ruleSet(t), sources(t), "fin.map.v1@3", limits, mapping.WithScope(packScope(t)))
	if err != nil {
		t.Fatalf("the pack's mapping must be fin.map.v1@3 (which keeps the version 2 list entities) and load with the pack's own scope: %v", err)
	}
	if m.ID() != "fin.map.v1@3" || m.RuleSet() != ruleSet(t).ID() {
		t.Errorf("ID %q, rule set %q", m.ID(), m.RuleSet())
	}
	rows, err := m.ListRows(caseSystem, "case", casePage(true, aCase("case_1", "ROUTED")))
	if err != nil || len(rows) != 1 || rows[0]["epoch"] != predicate.Int(3) || rows[0]["status"] != predicate.Enum("ROUTED") || rows[0]["created_by_intent"] != predicate.Ref("int_aa") {
		t.Errorf("the real mapping's case row: %v, %v", rows, err)
	}
	c, err := m.ListRows(caseSystem, "complaint_ref", casePage(true, map[string]any{"complaint_ref": "cmp_1", "case_id": "case_1", "logged_at": "2026-11-14T09:05:00Z", "clock_started_at": "2026-11-14T09:00:00Z"}))
	if err != nil || len(c) != 1 || c[0]["effective_at"] != predicate.Time(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC).Unix()) {
		t.Errorf("the real mapping's clock anchor must be clock_started_at (the target's own clock), not logged_at: %v, %v", c, err)
	}
	// the three reconciliation rows are unchanged by the bump
	for src, payload := range map[string]map[string]any{ledger: entries(entry("DEBIT", "POSTED")), switchSrc: {"semantic_status": "SUCCESS"}, destination: {"credit_state": "CREDITED"}} {
		if rows, err := m.Rows(src, payload); err != nil || len(rows) != 1 {
			t.Errorf("%s: %v, %v", src, rows, err)
		}
	}
}

// --- the stand-in's lists already carry the marker (A438: "the stand-in's case list gets that marker") ---------------------------------------

func TestTheMockCasePagesCarryTheCompletenessMarkerTheMappingReads(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "synthetic", "mock_case", "internal", "api", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Required []string `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CasePage", "ComplaintPage"} {
		req := doc.Components.Schemas[name].Required
		has := func(k string) bool {
			for _, r := range req {
				if r == k {
					return true
				}
			}
			return false
		}
		if !has("items") || !has("complete") {
			t.Errorf("%s requires %v: the mapping reads `items` and the marker `complete`", name, req)
		}
	}
}

var _ = reconcile.Known
