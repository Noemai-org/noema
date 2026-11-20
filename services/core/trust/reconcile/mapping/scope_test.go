package mapping_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

// Tests of mapping.ScopeFromDescriptors (moved from cmd/core by the ruling on noema#773: it sits beside WithScope): the typed fields of the pack's evidence entities, which the evidence mapping needs for its list entities (fin.map.v1@2, A438). UND-04; A320, A333, A438.

func scopePack(t *testing.T, descriptors string) []byte {
	t.Helper()
	return []byte(`{"pack_id":"fin","pack_version":"1.0.0","evidence_descriptors":` + descriptors + `}`)
}

func TestTheScopeOfTheRealPackTypesTheCaseAndComplaintEntities(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "packs", "fintech_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	sc, err := mapping.ScopeFromDescriptors(raw)
	if err != nil {
		t.Fatalf("the real pack's descriptors must give a scope: %v", err)
	}
	if got := sc.Entities["complaint_ref"]["effective_at"]; got.Kind != predicate.KindTimestamp {
		t.Errorf("complaint_ref.effective_at = %v, want Timestamp (A438: the clock anchor)", got)
	}
	if got := sc.Entities["case"]["epoch"]; got.Kind != predicate.KindInteger {
		t.Errorf("case.epoch = %v, want Integer", got)
	}
	if got := sc.Entities["case"]["id"]; got.Kind != predicate.KindReference {
		t.Errorf("case.id = %v, want Reference", got)
	}
	if st := sc.Entities["case"]["status"]; st.Kind != predicate.KindEnum {
		t.Errorf("case.status = %v, want an Enum", st)
	}
}

func TestTheScopeRefusesDescriptorsThatAreNotSound(t *testing.T) {
	for name, tc := range map[string]string{
		"no descriptors":                `[]`,
		"an entity with no name":        `[{"entity":"","source":"s","fields":{"id":{"type":"Reference"}}}]`,
		"an entity twice":               `[{"entity":"a","source":"s","fields":{"id":{"type":"Reference"}}},{"entity":"a","source":"s","fields":{"id":{"type":"Reference"}}}]`,
		"an entity with no field":       `[{"entity":"a","source":"s","fields":{}}]`,
		"a field type that is unknown":  `[{"entity":"a","source":"s","fields":{"id":{"type":"Decimal"}}}]`,
		"an enum with no value":         `[{"entity":"a","source":"s","fields":{"st":{"type":"Enum"}}}]`,
		"an enum with an empty list":    `[{"entity":"a","source":"s","fields":{"st":{"type":"Enum","values":[]}}}]`,
		"a value on a type that is not": `[{"entity":"a","source":"s","fields":{"id":{"type":"Reference","values":["X"]}}}]`,
	} {
		t.Run(name, func(t *testing.T) {
			sc, err := mapping.ScopeFromDescriptors(scopePack(t, tc))
			if err == nil {
				t.Fatalf("accepted: %+v", sc)
			}
			if strings.Contains(err.Error(), "Decimal") {
				t.Errorf("the error names pack content: %v", err)
			}
		})
	}
	if _, err := mapping.ScopeFromDescriptors([]byte(`{"pack_id":"fin"}`)); err == nil {
		t.Error("a pack with no evidence_descriptors member was accepted")
	}
}

func TestTheScopeTypesEveryFieldKindTheContractAllows(t *testing.T) {
	sc, err := mapping.ScopeFromDescriptors(scopePack(t, `[{"entity":"a","source":"s","fields":{"s":{"type":"String"},"i":{"type":"Integer"},"b":{"type":"Boolean"},"t":{"type":"Timestamp"},"d":{"type":"Duration"},"r":{"type":"Reference"},"e":{"type":"Enum","values":["ONE","TWO"]}}}]`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]predicate.Type{"s": predicate.String, "i": predicate.Integer, "b": predicate.Boolean, "t": predicate.Timestamp, "d": predicate.Duration, "r": predicate.Reference, "e": predicate.EnumOf("ONE", "TWO")}
	for name, w := range want {
		if got := sc.Entities["a"][name]; got.Kind != w.Kind {
			t.Errorf("field %s: kind %v, want %v", name, got.Kind, w.Kind)
		}
	}
}
