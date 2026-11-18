package intakeadapters

// The policy book: the pack's policy loaded as data, and the facts its rules read (REQ-TRU-004; INV-14, INV-19). The pack is the shipped fintech pack.

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestTheShippedPackLoadsAsAPolicyBookAndNamesTheFactsEachCapabilityReads(t *testing.T) {
	b := book(t, realPack(t))
	if b.PolicyID() != "fin.pol" {
		t.Fatalf("policy id %q", b.PolicyID())
	}
	if _, ok := b.versions["3.1"]; !ok || len(b.versions) != 1 {
		t.Fatalf("versions %v", b.versions)
	}
	for target, want := range map[string]FactNeed{
		// a write on a subject reads the subject, its resolution status, the epoch and the case system's rows
		"fin.cap.create_escalation_case":        {Variables: []string{"logical_resolution_epoch", "subject_resource", "subject_resource.status"}, Entities: []string{"case"}},
		"fin.cap.record_investigation_evidence": {Variables: []string{"subject_resource.status"}},
		"fin.cap.route_to_internal_owner":       {Variables: []string{"case_id"}},
		// a read, a prohibited capability and the complaint rules have conditions that read nothing
		"fin.cap.lookup_transaction":       {},
		"fin.cap.transfer_funds":           {},
		"fin.cap.log_complaint_reference":  {},
		"fin.cap.a_capability_no_rule_has": {},
	} {
		got := b.need(target)
		if len(got.Variables) == 0 && len(want.Variables) == 0 && len(got.Entities) == 0 && len(want.Entities) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s needs %+v, want %+v", target, got, want)
		}
	}
}

// A policy that cannot be used is refused whole, and at load (fail closed): the adapters never evaluate half a document.
func TestAPackWhoseDocumentsCannotBeUsedIsRefused(t *testing.T) {
	rules := func(f func([]any) []any) []byte { return variantPack(t, "3.2", f) }
	dropKey := func(key string) []byte {
		var m map[string]any
		if err := json.Unmarshal(realPack(t), &m); err != nil {
			t.Fatal(err)
		}
		delete(m, key)
		out, _ := json.Marshal(m)
		return out
	}
	for name, packs := range map[string][][]byte{
		"no pack at all":                  nil,
		"not JSON":                        {[]byte(`{`)},
		"a pack with no policy":           {dropKey("policy")},
		"a pack with no capabilities":     {dropKey("capabilities")},
		"a pack with no variables":        {dropKey("predicate_variables")},
		"a pack with no descriptors":      {dropKey("evidence_descriptors")},
		"a rule for an intent node":       {rules(withRules(rule("fin.pol.node", "WORKFLOW", "AT_DECISION", node, "true", "REJECT", "fin.reason.permitted", nil)))},
		"a rule for an unknown target":    {rules(withRules(rule("fin.pol.ghost", "WORKFLOW", "AT_DECISION", "fin.cap.ghost", "true", "REJECT", "fin.reason.permitted", nil)))},
		"a condition naming nothing real": {rules(withRules(rule("fin.pol.bad", "WORKFLOW", "AT_DECISION", "fin.cap.lookup_account", "nowhere > 1", "REJECT", "fin.reason.permitted", nil)))},
		"the same version twice":          {realPack(t), realPack(t)},
		"two policies in one book": {realPack(t), func() []byte {
			var m map[string]any
			_ = json.Unmarshal(realPack(t), &m)
			m["policy"].(map[string]any)["policy_id"] = "fin.other"
			out, _ := json.Marshal(m)
			return out
		}()},
		"two vocabularies in one book": {realPack(t), func() []byte {
			var m map[string]any
			_ = json.Unmarshal(realPack(t), &m)
			m["policy"].(map[string]any)["version"] = "3.2"
			m["predicate_variables"].(map[string]any)["extra_variable"] = map[string]any{"type": "Integer"}
			out, _ := json.Marshal(m)
			return out
		}()},
	} {
		if b, err := LoadPolicyBook(packs...); !errors.Is(err, ErrPack) || b != nil {
			t.Errorf("%s: book %v, err %v, want ErrPack and no book", name, b, err)
		}
	}
}

// Older pack versions load beside the current one, each under its own version, so a tenant admitted under one is still evaluated under it (D8).
func TestSeveralVersionsOfThePolicyLoadInOneBook(t *testing.T) {
	b := book(t, realPack(t), variantPack(t, "3.2", nil), variantPack(t, "3.10", nil))
	for _, v := range []string{"3.1", "3.2", "3.10"} {
		if _, ok := b.versions[v]; !ok {
			t.Errorf("version %s is not in the book", v)
		}
	}
}
