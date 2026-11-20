package payloadguard_test

// A331 (relayed Architect ruling, interim; not a committed decision): the money exemption of A279/A270 applies only to customer-visible text, never to a structured fact value, an
// option ID, a reference or any other field. The guard reads the verdict of the shared vector file (contracts/fixtures/digit_run_vectors.json: `path_vectors`, and `path_class` on
// the money vectors) and holds its own list of customer-visible text paths to the list in that file and to the text members of customer-answer.v1 and overlay-card.v1, so a member
// added to a contract cannot become a money-exempt path unnoticed. REQ-SEC-003, REQ-AUD-003, REQ-UI-001. All data is SYNTHETIC.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/record/payloadguard"
)

type pathVector struct {
	Name    string `json:"name"`
	Input   string `json:"input"`
	Path    string `json:"path"`
	Refused bool   `json:"refused"`
	Proves  string `json:"proves"`
}

type pathRule struct {
	Paths   []string            `json:"customer_visible_text_paths"`
	Sources map[string][]string `json:"sources"`
	NotText []string            `json:"not_text"`
}

func loadPathFile(t *testing.T) (pathRule, []pathVector) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "fixtures", "digit_run_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Rule    pathRule     `json:"path_rule"`
		Vectors []pathVector `json:"path_vectors"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Rule.Paths) == 0 || len(f.Vectors) < 20 {
		t.Fatalf("the vector file has %d text paths and %d path vectors: the path rule is missing", len(f.Rule.Paths), len(f.Vectors))
	}
	return f.Rule, f.Vectors
}

// payloadAt is the JSON document whose value at the pointer path is input: a numeric segment is an array index, any other a key. The path "/" is the root string itself.
func payloadAt(t *testing.T, path, input string) []byte {
	t.Helper()
	var build func(segs []string) any
	build = func(segs []string) any {
		if len(segs) == 0 {
			return input
		}
		if n, err := strconv.Atoi(segs[0]); err == nil {
			arr := make([]any, n+1)
			arr[n] = build(segs[1:])
			return arr
		}
		return map[string]any{segs[0]: build(segs[1:])}
	}
	var segs []string
	if path != "/" {
		segs = strings.Split(strings.TrimPrefix(path, "/"), "/")
	}
	out, err := json.Marshal(build(segs))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Every path vector, by name: a whole money string is accepted at a customer-visible text path and refused at any other.
func TestMoneyIsExemptOnlyAtACustomerVisibleTextPath(t *testing.T) {
	_, vectors := loadPathFile(t)
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			got := payloadguard.Check(payloadAt(t, v.Path, v.Input)) != nil
			if got != v.Refused {
				t.Errorf("%q at %s: refused = %v, the file says %v (%s)", v.Input, v.Path, got, v.Refused, v.Proves)
			}
		})
	}
}

// The money vectors of the main list say where they are accepted, and the guard accepts them there (a text path) and nowhere else (a path of no exemption of its own).
func TestTheMoneyVectorsNameTheirPathClass(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "fixtures", "digit_run_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Vectors []struct {
			Name      string `json:"name"`
			Input     string `json:"input"`
			Exemption string `json:"exemption"`
			PathClass string `json:"path_class"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, v := range f.Vectors {
		if v.Exemption != "money" {
			if v.PathClass != "" {
				t.Errorf("%s has a path class but no exemption", v.Name)
			}
			continue
		}
		n++
		if v.PathClass != "customer_visible_text" {
			t.Errorf("%s: a money vector must name path_class customer_visible_text, has %q", v.Name, v.PathClass)
		}
		if payloadguard.Check(payloadAt(t, "/v", v.Input)) == nil {
			t.Errorf("%s: %q is accepted at /v, a path that is no customer-visible text", v.Name, v.Input)
		}
	}
	if n < 7 {
		t.Fatalf("only %d money vectors name their path class", n)
	}
}

// The guard's list is the file's list, and the file's list is the text members of the contracts plus the Record's explanation claim text.
func TestTheGuardsTextPathsAreTheFilesAndTheContractsOwn(t *testing.T) {
	rule, _ := loadPathFile(t)
	got := payloadguard.CustomerVisibleTextPaths()
	want := slices.Clone(rule.Paths)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("the guard's customer-visible text paths %v differ from the file's %v", got, want)
	}
	var fromSources []string
	for _, ps := range rule.Sources {
		fromSources = append(fromSources, ps...)
	}
	slices.Sort(fromSources)
	if !slices.Equal(fromSources, want) {
		t.Fatalf("the file's sources %v do not add up to its list %v", fromSources, want)
	}
	for file, roots := range map[string]string{"customer-answer.v1.json": "root", "overlay-card.v1.json": "defs"} {
		derived := textMembers(t, file, roots, rule.NotText)
		var declared []string
		for src, ps := range rule.Sources {
			if strings.HasPrefix(file, src) {
				declared = append(declared, ps...)
			}
		}
		slices.Sort(declared)
		if !slices.Equal(derived, declared) {
			t.Errorf("%s: the text members of the contract are %v, the file declares %v", file, derived, declared)
		}
	}
}

// textMembers lists, as pointer paths with * for an array index, the string members of a contract that carry the digit-run pattern and a length bound, except the named non-text ones.
func textMembers(t *testing.T, file, roots string, notText []string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "schemas", file))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var out []string
	var walk func(n map[string]any, path string)
	walk = func(n map[string]any, path string) {
		if n["type"] == "string" && n["pattern"] != nil && n["maxLength"] != nil {
			if !slices.Contains(notText, path[strings.LastIndex(path, "/")+1:]) {
				out = append(out, path)
			}
			return
		}
		if props, ok := n["properties"].(map[string]any); ok {
			for k, v := range props {
				if m, ok := v.(map[string]any); ok {
					walk(m, path+"/"+k)
				}
			}
		}
		if items, ok := n["items"].(map[string]any); ok {
			walk(items, path+"/*")
		}
	}
	if roots == "root" {
		walk(doc, "")
	} else {
		defs, _ := doc["$defs"].(map[string]any)
		for _, d := range defs {
			if m, ok := d.(map[string]any); ok && m["properties"] != nil {
				walk(m, "")
			}
		}
	}
	slices.Sort(out)
	return out
}

// Mask keeps no money exemption; MaskText, the mask of customer-visible text, keeps it. A stored `NGN 12,345.00` in a structured value is masked like any run of five.
func TestMaskHasNoMoneyExemptionAndMaskTextHasIt(t *testing.T) {
	for _, amount := range []string{"NGN 12,345.00", "NGN 123.45", "NGN 123,456,789.00"} {
		if got := payloadguard.Mask(amount); got == amount {
			t.Errorf("Mask(%q) = %q: a money string is no exception outside customer-visible text", amount, got)
		}
		if got := payloadguard.MaskText(amount); got != amount {
			t.Errorf("MaskText(%q) = %q: customer-visible text keeps a whole money string", amount, got)
		}
	}
	for _, s := range []string{"4111 1111 1111 1111", "NGN 12345.00", "ref 0123456789"} {
		if payloadguard.Mask(s) != payloadguard.MaskText(s) || payloadguard.Mask(s) == s {
			t.Errorf("%q: Mask %q, MaskText %q; want the same, and changed", s, payloadguard.Mask(s), payloadguard.MaskText(s))
		}
	}
}
