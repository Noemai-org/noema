package trustport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unicode/utf8"
)

// A350 (noema#741): the bounds this adapter holds are the contract's own. The generated Go types carry no bound constants, so the adapter holds them as constants and this test
// reads the schemas and asserts each constant equals the schema's value; a contract change that moves a bound fails here, not silently in production (REQ-CTX-006, INV-16).

func candSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	dir := filepath.Dir(here)
	for i := 0; i < 8; i++ {
		if st, err := os.Stat(filepath.Join(dir, "contracts", "schemas")); err == nil && st.IsDir() {
			raw, err := os.ReadFile(filepath.Join(dir, "contracts", "schemas", name))
			if err != nil {
				t.Fatal(err)
			}
			var out map[string]any
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("contracts/schemas not found above the test file")
	return nil
}

func candAt(t *testing.T, m map[string]any, path ...string) any {
	t.Helper()
	var cur any = m
	for _, k := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("schema path %v: %q is not an object", path, k)
		}
		cur, ok = obj[k]
		if !ok {
			t.Fatalf("schema path %v: no %q", path, k)
		}
	}
	return cur
}

func TestTheAdaptersBoundsEqualTheContractsOwn(t *testing.T) {
	resp := candSchema(t, "candidates-response.v1.json")
	req := candSchema(t, "candidates-request.v1.json")
	money := candSchema(t, "money.v1.json")
	for name, c := range map[string]struct {
		got  int64
		want any
	}{
		"candidates.maxItems of the response":            {candMaxRaw, candAt(t, resp, "properties", "candidates", "maxItems")},
		"counterparty_display.maxLength of the response": {candMaxSurfaceRunes, candAt(t, resp, "$defs", "Candidate", "properties", "counterparty_display", "maxLength")},
		"counterparty_surface.maxLength of the request":  {candMaxSurfaceRunes, candAt(t, req, "properties", "filters", "properties", "counterparty_surface", "maxLength")},
		"MinorAmount maximum of the money contract":      {candMaxMinor, candAt(t, money, "$defs", "MinorAmount", "maximum")},
	} {
		w, ok := c.want.(float64)
		if !ok {
			t.Fatalf("%s: the schema's value %v is not a number", name, c.want)
		}
		if float64(c.got) != w && c.got != int64(w) {
			t.Errorf("%s: the adapter holds %d, the schema says %v", name, c.got, c.want)
		}
	}
	if utf8.RuneCountInString(candSchemaVersion) == 0 {
		t.Error("the adapter's schema version is empty")
	}
}

func TestTheAdaptersEnumsAreTheContractsOwn(t *testing.T) {
	resp := candSchema(t, "candidates-response.v1.json")
	money := candSchema(t, "money.v1.json")
	want := func(v any) []string {
		raw, ok := v.([]any)
		if !ok {
			t.Fatalf("not an enum list: %v", v)
		}
		out := make([]string, len(raw))
		for i, e := range raw {
			out[i], _ = e.(string)
		}
		return out
	}
	dirs := want(candAt(t, resp, "$defs", "Candidate", "properties", "direction", "enum"))
	curs := want(candAt(t, money, "$defs", "CurrencyCode", "enum"))
	if len(dirs) != 2 || dirs[0] != "OUTGOING" || dirs[1] != "INCOMING" {
		t.Errorf("the response's direction enum is %v; candEnumsOK checks OUTGOING and INCOMING", dirs)
	}
	if len(curs) != 1 || curs[0] != "NGN" {
		t.Errorf("the money contract's currency enum is %v; candEnumsOK checks exactly the generated constants (one)", curs)
	}
}
