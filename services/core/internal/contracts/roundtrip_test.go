package contracts_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// CON-03, REQ-API-001, REQ-TST-003: the generated Go types against the fixtures of /contracts, the same files the JSON Schema
// validator and the generated Pydantic models are tested against (services/model/tests/test_contracts_generated.py).

var fixtures = filepath.Join("..", "..", "..", "..", "contracts", "fixtures")

var versioned = regexp.MustCompile(`\.v[0-9]+$`)

// target maps a fixture folder and file name to the generated type's name in contracts.Types: `<folder>` is `<schema>.v1` for
// the first major, and `<Def>__<case>.json` targets a definition.
func target(folder, file string) string {
	key := folder
	if !versioned.MatchString(key) {
		key += ".v1"
	}
	if def, _, ok := strings.Cut(strings.TrimSuffix(file, ".json"), "__"); ok {
		return key + "#" + def
	}
	return key
}

type fixture struct{ folder, file, key, path string }

// excluded is the list in tools/codegen/excluded.json: contracts the services hold no types for.
func excluded(t *testing.T, key string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "tools", "codegen", "excluded.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Prefixes []string `json:"prefixes"`
		Names    []string `json:"names"`
		GoOnly   struct {
			Names []string `json:"names"`
		} `json:"go_only"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	base := key
	if i := strings.LastIndex(key, ".v"); i > 0 {
		base = key[:i]
	}
	for _, n := range cfg.Names {
		if base == n {
			return true
		}
	}
	for _, p := range cfg.Prefixes {
		if strings.HasPrefix(base, p) {
			return true
		}
	}
	for _, n := range cfg.GoOnly.Names {
		if key == n {
			return true
		}
	}
	return false
}

func listFixtures(t *testing.T, kind string) []fixture {
	t.Helper()
	dirs, err := os.ReadDir(filepath.Join(fixtures, kind))
	if err != nil {
		t.Fatal(err)
	}
	var out []fixture
	for _, d := range dirs {
		if !d.IsDir() || excluded(t, target(d.Name(), "x.json")) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(fixtures, kind, d.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			n := f.Name()
			if !strings.HasSuffix(n, ".json") || n == "expectations.json" || strings.HasSuffix(n, ".clarification.json") {
				continue
			}
			out = append(out, fixture{d.Name(), n, target(d.Name(), n), filepath.Join(fixtures, kind, d.Name(), n)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func decode(t *testing.T, key string, raw []byte) (any, error) {
	t.Helper()
	mk, ok := contracts.Types[key]
	if !ok {
		t.Fatalf("contracts.Types has no %q: every schema and definition must have a generated type", key)
	}
	v := mk()
	return v, json.Unmarshal(raw, v)
}

func generic(t *testing.T, raw []byte) any {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// A generated type that decodes a valid fixture and encodes it again must give the same document. This is what catches a lost
// member, a null turned into an absence, an empty array turned into null, or a number rewritten.
func TestEveryValidFixtureRoundTripsThroughItsGeneratedType(t *testing.T) {
	all := listFixtures(t, "valid")
	if len(all) < 150 {
		t.Fatalf("only %d valid fixtures found; the fixture path is wrong", len(all))
	}
	var bad []string
	for _, f := range all {
		raw, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		v, err := decode(t, f.key, raw)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s: a valid fixture is refused: %v", f.path, err))
			continue
		}
		out, err := json.Marshal(v)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s: cannot be encoded: %v", f.path, err))
			continue
		}
		if !reflect.DeepEqual(generic(t, raw), generic(t, out)) {
			bad = append(bad, fmt.Sprintf("%s: the round trip changed the document:\n  in:  %s\n  out: %s", f.path, compact(raw), out))
		}
	}
	if len(bad) > 0 {
		t.Fatalf("%d of %d valid fixtures failed:\n%s", len(bad), len(all), strings.Join(bad, "\n"))
	}
	t.Logf("%d valid fixtures round-trip through their generated Go types", len(all))
}

func compact(raw []byte) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

func TestEverySchemaAndDefinitionHasAGeneratedType(t *testing.T) {
	schemas, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "contracts", "schemas", "*.json"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schemas found: %v", err)
	}
	for _, p := range schemas {
		key := strings.TrimSuffix(filepath.Base(p), ".json")
		if excluded(t, key) {
			if _, ok := contracts.Types[key]; ok {
				t.Errorf("%s is excluded by tools/codegen/excluded.json and has a generated type", key)
			}
			continue
		}
		if _, ok := contracts.Types[key]; !ok {
			t.Errorf("no generated type for %s", key)
		}
		raw, _ := os.ReadFile(p)
		var s struct {
			Defs map[string]json.RawMessage `json:"$defs"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		for d := range s.Defs {
			if _, ok := contracts.Types[key+"#"+d]; !ok {
				t.Errorf("no generated type for %s#%s", key, d)
			}
		}
	}
}

// The generated types model shape; the JSON Schema validator models the rest. tools/codegen/shape_refusals.json lists, from the
// validator's own error paths, the invalid fixtures that break a shape keyword (required, additionalProperties, enum, const,
// type) with no conditional above it. The generated types must refuse every one of them. Patterns, bounds and conditional rules
// are not in the list because they are the validator's.
func shapeRefusals(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "tools", "codegen", "shape_refusals.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTheGeneratedTypesRefuseWhatTheirShapeForbids(t *testing.T) {
	want := shapeRefusals(t)
	if len(want) < 150 {
		t.Fatalf("only %d shape refusals listed; regenerate tools/codegen/shape_refusals.json", len(want))
	}
	found := map[string]bool{}
	for id := range want {
		if folder, _, _ := strings.Cut(id, "/"); excluded(t, target(folder, "x.json")) {
			delete(want, id) // a contract held back from Go (tools/codegen/excluded.json)
		}
	}
	for _, f := range listFixtures(t, "invalid") {
		id := f.folder + "/" + f.file
		kw, listed := want[id]
		if !listed {
			continue
		}
		found[id] = true
		raw, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, derr := decode(t, f.key, raw); derr == nil {
			t.Errorf("%s: breaks %q and the generated Go type accepts it", id, kw)
		}
	}
	for id := range want {
		if !found[id] {
			t.Errorf("shape_refusals.json names %s, which is not an invalid fixture", id)
		}
	}
	t.Logf("%d invalid fixtures with a shape violation are refused by the generated Go types", len(want))
}
