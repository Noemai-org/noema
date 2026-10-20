package corpus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

const contractsDir = "../../../../contracts"

func readJSON(t *testing.T, rel string, into *scopeFile) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(contractsDir, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode %s: %v", rel, err)
	}
}

type fieldSpec struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type descriptor struct {
	Entity string               `json:"entity"`
	Fields map[string]fieldSpec `json:"fields"`
}

type scopeJSON struct {
	Descriptors []descriptor         `json:"evidence_descriptors"`
	Variables   map[string]fieldSpec `json:"predicate_variables"`
}

type corpusCase struct {
	ID     string `json:"id"`
	Expr   string `json:"expr"`
	Result string `json:"result"`
	Error  string `json:"error"`
}

type scopeFile struct {
	Scope scopeJSON    `json:"scope"`
	Cases []corpusCase `json:"cases"`
}

func typeOf(spec fieldSpec, t *testing.T) predicate.Type {
	t.Helper()
	switch spec.Type {
	case "Boolean":
		return predicate.Boolean
	case "String":
		return predicate.String
	case "Integer":
		return predicate.Integer
	case "Timestamp":
		return predicate.Timestamp
	case "Duration":
		return predicate.Duration
	case "Reference":
		return predicate.Reference
	case "Enum":
		return predicate.EnumOf(spec.Values...)
	}
	t.Fatalf("unknown field type %q", spec.Type)
	return predicate.Type{}
}

func toScope(t *testing.T, s scopeJSON) predicate.Scope {
	t.Helper()
	sc := predicate.Scope{Entities: map[string]map[string]predicate.Type{}, Variables: map[string]predicate.Type{}}
	for _, d := range s.Descriptors {
		fields := map[string]predicate.Type{}
		for name, spec := range d.Fields {
			fields[name] = typeOf(spec, t)
		}
		sc.Entities[d.Entity] = fields
	}
	for name, spec := range s.Variables {
		sc.Variables[name] = typeOf(spec, t)
	}
	return sc
}

func loadCorpus(t *testing.T) (predicate.Scope, []corpusCase) {
	t.Helper()
	var f scopeFile
	readJSON(t, "predicates/tests/corpus.json", &f)
	return toScope(t, f.Scope), f.Cases
}
