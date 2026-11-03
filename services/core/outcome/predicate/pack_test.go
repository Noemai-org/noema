package predicate_test

import (
	"os"
	"strings"
	"testing"

	ip "github.com/Noemai-org/noema/services/core/internal/predicate"
	op "github.com/Noemai-org/noema/services/core/outcome/predicate"
)

func fintech(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// REQ-VER-002: every outcome definition of the fintech pack type-checks against its own descriptors at load, and the
// case_opened definition is the SoT 15.3 one.
func TestTheFintechPackLoadsAndEveryOutcomeTypeChecks(t *testing.T) {
	p, err := op.LoadPack(fintech(t))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := p.Definitions["fin.outcome.exception.case_opened"]
	if !ok {
		t.Fatal("fin.outcome.exception.case_opened is not loaded")
	}
	if d.SettleWindowSecs != 30 {
		t.Fatalf("settle window = %d, want 30 (SoT 15.3)", d.SettleWindowSecs)
	}
	if _, ok := p.Scope.Entities["case"]["created_by_intent"]; !ok {
		t.Fatal("the case descriptor lost created_by_intent")
	}
}

// REQ-VER-002 acceptance: a predicate referencing an unknown field fails at load, and a non-Boolean predicate does too.
func TestAPredicateWithAnUnknownFieldOrTypeDoesNotLoad(t *testing.T) {
	good := string(fintech(t))
	for name, mutate := range map[string]func(string) string{
		"an unknown field": func(s string) string {
			return strings.Replace(s, "case.created_by_intent == intent_id", "case.made_by == intent_id", 1)
		},
		"an unknown entity": func(s string) string { return strings.Replace(s, "count(case where", "count(ticket where", 1) },
		"an unknown variable": func(s string) string {
			return strings.Replace(s, "case.created_by_intent == intent_id", "case.created_by_intent == who", 1)
		},
		"a non-Boolean result": func(s string) string {
			return strings.Replace(s, "count(case where case.subject == subject_resource and case.created_by_intent == intent_id) == 1", "count(case where case.subject == subject_resource)", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := mutate(good)
			if bad == good {
				t.Fatal("the mutation did not change the pack")
			}
			if _, err := op.LoadPack([]byte(bad)); err == nil {
				t.Fatal("the pack loaded")
			}
		})
	}
}

func TestCompileRefusesAProgramThatIsNotBoolean(t *testing.T) {
	sc := ip.Scope{Entities: map[string]map[string]ip.Type{"case": {"id": ip.Reference}}, Variables: map[string]ip.Type{}}
	if _, err := op.Compile("count(case where case.id == case.id)", sc); err == nil {
		t.Fatal("a predicate that returns an Integer compiled")
	}
}

// A pack that is ambiguous or misspelled does not load: no later duplicate wins, no unknown field is ignored, no settle
// window is guessed (REQ-VER-002, fail closed).
func TestAnAmbiguousOrMisspelledPackDoesNotLoad(t *testing.T) {
	good := string(fintech(t))
	dupOutcome := func(s string) string {
		i := strings.Index(s, `"outcome_definitions": [`) + len(`"outcome_definitions": [`)
		j := strings.Index(s[i:], "{")
		k := i + j
		return s[:k] + `{"outcome_id": "fin.outcome.exception.case_opened", "intent_nodes": ["fin.payments.exception.investigate"], "success_predicate": "exists(case where case.id == case.id)", "intermediate_states": [], "terminal_states": ["ACHIEVED"], "settle_window_seconds": 0, "verification_sources": [{"source": "fin.src.case_system", "priority": 1}], "compensation": "none_required", "explanation_templates": ["x"]}, ` + s[k:]
	}
	for name, mutate := range map[string]func(string) string{
		"a duplicate outcome ID": dupOutcome,
		"a misspelled definition field": func(s string) string {
			return strings.Replace(s, `"settle_window_seconds": 30`, `"settle_window_second": 30`, 1)
		},
		"a negative settle window": func(s string) string {
			return strings.Replace(s, `"settle_window_seconds": 30`, `"settle_window_seconds": -30`, 1)
		},
		"a missing settle window": func(s string) string { return strings.Replace(s, `"settle_window_seconds": 30,`, ``, 1) },
		"a duplicate descriptor entity": func(s string) string {
			return strings.Replace(s, `"entity": "attachment"`, `"entity": "case"`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := mutate(good)
			if bad == good {
				t.Fatal("the mutation did not change the pack")
			}
			if _, err := op.LoadPack([]byte(bad)); err == nil {
				t.Fatal("the pack loaded")
			}
		})
	}
}
