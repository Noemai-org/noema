package trustport_test

// A370: the capability an admitted intent asks for is a field of intake.AdmittedIntent (RequestedCapability), filled by the Intents adapter from the stored document and the pack.
// It replaces the second port (trustpack.Remedies) that read the same slot. REQ-TRU-004, REQ-TRU-003, REQ-CAP-002; INV-14, INV-16, INV-17. Real PostgreSQL, real gate, real store.
// SYNTHETIC. Every helper of this file starts with `rc` so that the other tests of the package cannot collide with it.

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const (
	rcCapability = "fin.cap.create_escalation_case"
	rcProhibited = "fin.cap.transfer_funds"
)

// rcWithRemedy is the fintech proposal with the node's capability slot set (the name of the slot is the fixture's; the adapter under test must find it from the pack).
func rcWithRemedy(slot map[string]any) func(m map[string]any) {
	return func(m map[string]any) { m["slots"].(map[string]any)["requested_remedy"] = slot }
}

func rcRef(value string) map[string]any { return map[string]any{"type": "Reference", "value": value} }

// rcAround stores a copy of a saved intent under a new ID, its capability slot replaced by raw JSON, around the gate and the store (as the administrator): the shapes the gate would
// refuse, which the adapter must still not read as "none".
func rcAround(w *world, id, rawSlot string) {
	w.t.Helper()
	if _, err := w.admin.Exec(tctx(w.t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, intent_node, intent_hash, correlation_id, json)
		SELECT tenant_id, $2, 1, intent_node, intent_hash, 'corr_' || substr($2, 5),
		jsonb_set(jsonb_set(jsonb_set(json, '{intent_id}', to_jsonb($2::text)), '{correlation_id}', to_jsonb('corr_' || substr($2, 5))), '{slots,requested_remedy}', $3::jsonb)
		FROM admitted_intent WHERE intent_id = $1`, intentID, id, rawSlot); err != nil {
		w.t.Fatal(err)
	}
}

// The capability the node's capability slot names is carried, as the document holds it: whether it is registered or prohibited is T4's question, not the store's.
func TestGetCarriesTheCapabilityTheNodesCapabilitySlotNames(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	s := w.intents(w.trustPools(), pack)
	for id, value := range map[string]string{"int_rcreg": rcCapability, "int_rcprohib": rcProhibited, "int_rcunknown": "fin.cap.not_in_the_pack"} {
		w.save(pack, tenantA, proposal(tenantA, id, 1, rcWithRemedy(rcRef(value))))
		got, err := s.Get(tctx(t), tenantA, id, 1)
		if err != nil || got.RequestedCapability != value {
			t.Errorf("%s: RequestedCapability = %q, %v; want %q", id, got.RequestedCapability, err, value)
		}
	}
}

// An intent that names none asks for none: no slot, and a slot that says it is absent.
func TestGetOfAnIntentThatAsksForNoCapabilityCarriesNone(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	s := w.intents(w.trustPools(), pack)
	w.save(pack, tenantA, proposal(tenantA, intentID, 1, nil))
	if got, err := s.Get(tctx(t), tenantA, intentID, 1); err != nil || got.RequestedCapability != "" {
		t.Errorf("no slot: %q, %v; want none", got.RequestedCapability, err)
	}
	rcAround(w, "int_rcabsent", `{"type":"Reference","value":"`+rcCapability+`","status":"ABSENT"}`)
	if got, err := s.Get(tctx(t), tenantA, "int_rcabsent", 1); err != nil || got.RequestedCapability != "" {
		t.Errorf("a slot that says ABSENT: %q, %v; want none", got.RequestedCapability, err)
	}
}

// A slot that is there but cannot be read as one capability is an error (the intake ends UNKNOWN), never "none": reading it as none would shrink the authorized set silently.
func TestGetOfACapabilitySlotThatCannotBeReadIsNotUsable(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, intentID, 1, nil))
	s := w.intents(w.trustPools(), pack)
	for name, raw := range map[string]string{
		"rcempty":     `{"type":"Reference","value":""}`,
		"rcnumber":    `{"type":"Reference","value":7}`,
		"rcnovalue":   `{"type":"Reference"}`,
		"rcwrongtype": `{"type":"Resource","value":"` + rcCapability + `"}`,
		"rcambiguous": `{"type":"Reference","value":"` + rcCapability + `","status":"AMBIGUOUS"}`,
		"rcunres":     `{"type":"Reference","value":"` + rcCapability + `","status":"UNRESOLVED"}`,
		"rcodd":       `{"type":"Reference","value":"` + rcCapability + `","status":"MAYBE"}`,
		"rcnull":      `null`,
		"rcstring":    `"` + rcCapability + `"`,
	} {
		id := "int_" + name
		rcAround(w, id, raw)
		if got, err := s.Get(tctx(t), tenantA, id, 1); !errors.Is(err, trustport.ErrStoredIntent) {
			t.Errorf("%s: got %+v, %v; want ErrStoredIntent", name, got, err)
		}
	}
}

// The slot is found from the pack (a Reference slot whose values are capability IDs), never by a name written in the core (INV-14). A node with two such slots: one value twice is that
// value, one absent and one present is the present one, and two different values are not usable (the intent has one capability).
func TestTheCapabilitySlotIsFoundFromThePackAndTwoDifferentOnesAreNotUsable(t *testing.T) {
	w := newWorld(t)
	var doc map[string]any
	if err := json.Unmarshal(fintechPack(t), &doc); err != nil {
		t.Fatal(err)
	}
	for _, n := range doc["intent_nodes"].([]any) {
		node := n.(map[string]any)
		if node["node_id"] == "fin.payments.exception.investigate" {
			node["slots"] = append(node["slots"].([]any), map[string]any{"name": "also_requested", "type": "Reference", "required": false, "references": "capability"})
		}
	}
	pack, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	s := w.intents(w.trustPools(), pack)
	both := func(a, b string) func(m map[string]any) {
		return func(m map[string]any) {
			slots := m["slots"].(map[string]any)
			slots["requested_remedy"], slots["also_requested"] = rcRef(a), rcRef(b)
		}
	}
	w.save(pack, tenantA, proposal(tenantA, "int_rctwice", 1, both(rcCapability, rcCapability)))
	if got, err := s.Get(tctx(t), tenantA, "int_rctwice", 1); err != nil || got.RequestedCapability != rcCapability {
		t.Errorf("one value twice: %q, %v", got.RequestedCapability, err)
	}
	w.save(pack, tenantA, proposal(tenantA, "int_rcdiff", 1, both(rcCapability, rcProhibited)))
	if got, err := s.Get(tctx(t), tenantA, "int_rcdiff", 1); !errors.Is(err, trustport.ErrStoredIntent) {
		t.Errorf("two different values: %q, %v; want ErrStoredIntent", got.RequestedCapability, err)
	}
	w.save(pack, tenantA, proposal(tenantA, "int_rcone", 1, func(m map[string]any) {
		m["slots"].(map[string]any)["also_requested"] = rcRef(rcProhibited)
	}))
	if got, err := s.Get(tctx(t), tenantA, "int_rcone", 1); err != nil || got.RequestedCapability != rcProhibited {
		t.Errorf("only the second slot: %q, %v", got.RequestedCapability, err)
	}
}

// A Reference slot of another vocabulary is not a capability: a slot the pack does not say holds capability IDs asks for nothing.
func TestAReferenceSlotThatIsNotACapabilityAsksForNone(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, intentID, 1, nil)) // question_type is a Reference slot with a vocabulary of its own
	got, err := w.intents(w.trustPools(), pack).Get(tctx(t), tenantA, intentID, 1)
	if err != nil || got.RequestedCapability != "" {
		t.Errorf("RequestedCapability = %q, %v; want none (question_type is a Reference, not a capability)", got.RequestedCapability, err)
	}
}

// Verify holds the capability Trust carries to the stored one (INV-17): the same intent with another capability is not the stored intent, and the stored one still verifies.
func TestVerifyRefusesAnAdmittedIntentWhoseCapabilityIsNotTheStoredOne(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, "int_rcverify", 1, rcWithRemedy(rcRef(rcCapability))))
	s := w.intents(w.trustPools(), pack)
	got, err := s.Get(tctx(t), tenantA, "int_rcverify", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(tctx(t), got); err != nil {
		t.Fatalf("the stored intent does not verify: %v", err)
	}
	for name, mutate := range map[string]func(a *intake.AdmittedIntent){
		"another capability": func(a *intake.AdmittedIntent) { a.RequestedCapability = rcProhibited },
		"no capability":      func(a *intake.AdmittedIntent) { a.RequestedCapability = "" },
	} {
		changed := got
		mutate(&changed)
		if err := s.Verify(tctx(t), changed); !errors.Is(err, intake.ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
}

// INV-14: the adapter's source names no slot, capability or industry word in any string it holds; the capability slot is the pack's (`references`).
func TestTheIntentsAdapterSourceNamesNoSlotOrCapability(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "intents.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	literals := 0
	ast.Inspect(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			literals++
			for _, word := range []string{"requested_remedy", "fin.cap.", "remedy"} {
				if strings.Contains(lit.Value, word) {
					t.Errorf("intents.go has a string literal that names %q: %s", word, lit.Value)
				}
			}
		}
		return true
	})
	if literals == 0 {
		t.Fatal("the scan found no string literal in intents.go: it scans nothing")
	}
}
