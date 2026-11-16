package contracts_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// A166 (2) (the Architect, recorded in docs#159), task A173, REQ-INP-002, AGENT-15: a confidence is an integer number of basis points, so an Intent Object can be hashed.
// crypto.CanonicalJSON refuses a fractional number before it canonicalises (so no hash or signature is ever computed over a float), and the canonical rule is not loosened (A166 (2)).
// The Intent Object v2 carries `0.97` in its slots and `0.91` in its source, so no v2 object that holds a confidence can be hashed. v3 carries `9700` and `9100`, and can.

func readFixture(t *testing.T, kind, folder, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtures, kind, folder, name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func strictDecode(t *testing.T, raw []byte, into any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		t.Fatal(err)
	}
}

func fixtureNames(t *testing.T, kind, folder string) []string {
	t.Helper()
	files, err := os.ReadDir(filepath.Join(fixtures, kind, folder))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range files {
		if filepath.Ext(f.Name()) == ".json" && f.Name() != "expectations.json" {
			out = append(out, f.Name())
		}
	}
	return out
}

// Every valid v3 fixture decodes into the generated type and its canonical form can be computed; the canonical form of the typed value equals that of the generic JSON
// (the typed form loses nothing and invents nothing).
func TestAnIntentObjectV3CanBeCanonicalisedAndHashed(t *testing.T) {
	names := fixtureNames(t, "valid", "intent-object.v3")
	if len(names) < 7 {
		t.Fatalf("only %d valid v3 fixtures", len(names))
	}
	for _, name := range names {
		raw := readFixture(t, "valid", "intent-object.v3", name)
		var typed contracts.IntentObjectV3
		strictDecode(t, raw, &typed)
		got, err := crypto.CanonicalJSON(typed)
		if err != nil {
			t.Errorf("%s: the canonical form of a valid v3 object was refused: %v", name, err)
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var generic any
		if err := dec.Decode(&generic); err != nil {
			t.Fatal(err)
		}
		want, err := crypto.CanonicalJSON(generic)
		if err != nil {
			t.Errorf("%s: the generic form was refused: %v", name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: the typed and the generic canonical forms differ:\n typed   %s\n generic %s", name, got, want)
		}
	}
}

// The reason the bump exists, on the v2 types as they are: a v2 object with a confidence cannot be canonicalised.
func TestAnIntentObjectV2WithAConfidenceCannotBeCanonicalised(t *testing.T) {
	raw := readFixture(t, "valid", "intent-object.v2", "sot_example.json")
	var typed contracts.IntentObjectV2
	strictDecode(t, raw, &typed)
	if _, err := crypto.CanonicalJSON(typed); err == nil {
		t.Fatal("a v2 object with a ratio was canonicalised: the premise of A166 (2) no longer holds, and v3 has no reason to exist")
	}
}

// The generated slot and source types carry integers: a fractional JSON number is refused at decode, not rounded.
func TestAFractionalConfidenceIsRefusedAtDecode(t *testing.T) {
	for _, name := range []string{"confidence_fractional.json", "asr_confidence_fractional.json", "confidence_string.json", "confidence_boolean.json"} {
		raw := readFixture(t, "invalid", "intent-object.v3", name)
		var typed contracts.IntentObjectV3
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&typed); err == nil {
			t.Errorf("%s: decoded into the v3 type without an error", name)
		}
	}
}
