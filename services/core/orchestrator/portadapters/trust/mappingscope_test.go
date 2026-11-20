package trustport_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

// caseScope is the option a composition gives the mapping so that its list entities (the case system's, fin.map.v1@2, A438) are typed by the pack's own evidence descriptors. These
// tests classify the reconciliation rows only; the list entities are not read here.
func caseScope(t *testing.T, packFile string) mapping.Option {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(packFile))
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
	return mapping.WithScope(sc)
}
