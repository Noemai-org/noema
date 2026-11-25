package contracts_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/record/payloadguard"
)

// The one parser of the intent reference `int_<id>@<version>` (common.v1.json#IntentRef), and the one test that every consumer relies on: the Trust intake, the Trust port, the
// evidence adapter, the Interpreter port that builds the form, and the Record's typed-ID check. The need was recorded in the independent read of noema#661 (note 1: "give the form
// one parser and one test shared with C1") and in status/UND-04.md (EL note 1). REQ-TRU-009 (the intake refuses a malformed reference at T1), REQ-AUD-001, INV-12, AGENT-02.

func TestParseIntentRefAcceptsTheFormAndReturnsTheIDAndTheVersion(t *testing.T) {
	for ref, want := range map[string]struct {
		id      string
		version int
	}{
		"int_ab@1":                              {"int_ab", 1},
		"int_synth0001@2":                       {"int_synth0001", 2},
		"int_01J9X3@1":                          {"int_01J9X3", 1},
		"int_" + strings.Repeat("Z", 32) + "@1": {"int_" + strings.Repeat("Z", 32), 1},
		"int_Ab9@999999999":                     {"int_Ab9", 999999999},
		"int_ab@10":                             {"int_ab", 10},
	} {
		id, version, err := contracts.ParseIntentRef(ref)
		if err != nil || id != want.id || version != want.version {
			t.Errorf("ParseIntentRef(%q) = %q, %d, %v; want %q, %d, nil", ref, id, version, err, want.id, want.version)
		}
	}
}

// refused holds every string the parser must refuse, with a short name. Where the contract's pattern would accept it, the name says so in narrower.
var refused = map[string]string{
	"empty":                       "",
	"no version":                  "int_ab",
	"empty version":               "int_ab@",
	"no id":                       "@1",
	"id of one character":         "int_a@1",
	"id of 33 characters":         "int_" + strings.Repeat("a", 33) + "@1",
	"id with an underscore":       "int_a_b@1",
	"id with a hyphen":            "int_a-b@1",
	"id with a slash":             "int/ab@1",
	"id with a non-ASCII letter":  "int_éa@1",
	"another prefix":              "usr_ab@1",
	"capital prefix":              "INT_ab@1",
	"no prefix":                   "ab@1",
	"version zero":                "int_ab@0",
	"version with a leading zero": "int_ab@01",
	"version of ten digits":       "int_ab@1000000000",
	"huge version":                "int_ab@99999999999999999999",
	"version with a sign":         "int_ab@-1",
	"version with a plus":         "int_ab@+1",
	"version with a fraction":     "int_ab@1.5",
	"version with a letter":       "int_ab@x",
	"fullwidth digit":             "int_ab@１",
	"two separators":              "int_ab@1@2",
	"leading space":               " int_ab@1",
	"trailing space":              "int_ab@1 ",
	"trailing newline":            "int_ab@1\n",
	"leading newline":             "\nint_ab@1",
	"space before the at":         "int_ab @1",
	"a NUL":                       "int_ab@1\x00",
}

// narrower names the refusals that the contract's looser pattern (`@[0-9]+`) accepts: the parser is stricter on the version only (a positive integer of at most nine digits, no leading
// zero), which is the form the Trust intake, the Trust port and the evidence adapter already required and the form the Interpreter port builds from a PositiveInt.
var narrower = map[string]bool{
	"version zero": true, "version with a leading zero": true, "version of ten digits": true, "huge version": true,
}

func TestParseIntentRefRefusesEverythingElseWithErrIntentRef(t *testing.T) {
	for name, ref := range refused {
		id, version, err := contracts.ParseIntentRef(ref)
		if !errors.Is(err, contracts.ErrIntentRef) || id != "" || version != 0 {
			t.Errorf("%s: ParseIntentRef(%q) = %q, %d, %v; want zero values and ErrIntentRef", name, ref, id, version, err)
		}
	}
}

// The contract is the authority for the form: everything the parser accepts matches common.v1.json#IntentRef, and everything the contract accepts and the parser refuses is named in
// narrower. Read from the contract, so a change to the pattern fails here.
func TestTheParserIsAStrictSubsetOfTheContractsIntentRef(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "schemas", "common.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Pattern string `json:"pattern"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	pattern := doc.Defs["IntentRef"].Pattern
	if pattern == "" {
		t.Fatal("common.v1.json has no IntentRef pattern: the test is looking in the wrong place")
	}
	contract := regexp.MustCompile(pattern)
	for _, ref := range []string{"int_ab@1", "int_synth0001@2", "int_01J9X3@1", "int_" + strings.Repeat("Z", 32) + "@999999999"} {
		if _, _, err := contracts.ParseIntentRef(ref); err != nil || !contract.MatchString(ref) {
			t.Errorf("%q: parser error %v, contract match %v; want both to accept", ref, err, contract.MatchString(ref))
		}
	}
	for name, ref := range refused {
		if contract.MatchString(ref) && !narrower[name] {
			t.Errorf("%s: the contract accepts %q and the parser refuses it, and it is not listed as a deliberate narrowing", name, ref)
		}
	}
}

// The Record's typed-ID check and the parser agree on the shape (the shared test of noema#661, note 1). A reference the parser accepts is a typed value the Record's digit guard
// exempts as a whole, however many digits its ID holds; a string the parser refuses, with the same long digit run, is not exempt and the guard refuses the payload.
func TestTheRecordsTypedIDCheckExemptsEveryReferenceTheParserAcceptsAndNothingItRefuses(t *testing.T) {
	accepted := []string{"int_12345678a9@3", "int_9876543210zz@999999999", "int_00000000a0@1", "int_" + strings.Repeat("7", 31) + "x@12"}
	for _, ref := range accepted {
		if _, _, err := contracts.ParseIntentRef(ref); err != nil {
			t.Fatalf("%q must be a reference: %v", ref, err)
		}
		if err := payloadguard.Check([]byte(`{"object_ref":"` + ref + `"}`)); err != nil {
			t.Errorf("the guard refuses the reference %q that the parser accepts: %v", ref, err)
		}
	}
	for _, ref := range []string{"int_12345678a9@x", "int_1234567890a@", "int_12345678_a9@3", "int_" + strings.Repeat("7", 33) + "x@1", "int_12345678a9@3 4"} {
		if _, _, err := contracts.ParseIntentRef(ref); err == nil {
			t.Fatalf("%q must not be a reference", ref)
		}
		if err := payloadguard.Check([]byte(`{"object_ref":"` + ref + `"}`)); err == nil {
			t.Errorf("the guard exempts %q, which the parser refuses", ref)
		}
	}
}

// CON-08 (A337, REQ-API-001): no second parser of the intent reference exists. No production Go file under services/core, other than intentref.go, holds a regular expression that
// reads the whole form (the `int_` ID class, an at sign and a version): a consumer calls contracts.ParseIntentRef. A pattern for the ID alone is not a parser of the form and is not
// refused here. Generated files and tests are not production code.
func TestNoSecondParserOfTheIntentReferenceExists(t *testing.T) {
	root := filepath.Join("..", "..")
	form := regexp.MustCompile("int_\\[[^\n]*@")
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == "testdata" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gen.go") || path == filepath.Join(root, "internal", "contracts", "intentref.go") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if form.Match(raw) {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Errorf("a second parser of the intent reference in %v; call contracts.ParseIntentRef", offenders)
	}
}
