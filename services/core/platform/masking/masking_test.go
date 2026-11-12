package masking_test

// SEC-03 / REQ-SEC-003, REQ-EXP-002, AGENT-16, AGENT-13: the longest unmasked digit run is pack data, read from one file (masking-limits.v1) by the core's redactor,
// the explanation validator and the error-scanner test, and by the model service. A value below the four digits a mask keeps, above the contract's maximum, not a whole
// number, a missing field or anything after the document is refused; there is no default. Ruling A154 as corrected (relayed, not yet committed). SYNTHETIC data.

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/masking"
)

const packFile = "../../../../packs/fintech/limits/masking_limits.json"

func doc(value string) string {
	return `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","max_unmasked_digit_run":` + value + `}`
}

// The pack file says 4: a run of five or more digits is masked.
func TestThePacksLimitIsFourAndAFiveDigitRunIsTheFirstThatIsMasked(t *testing.T) {
	f, err := os.Open(packFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := masking.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if l.MaxUnmaskedRun() != 4 || l.MinMaskedRun() != 5 {
		t.Fatalf("max unmasked %d, min masked %d; want 4 and 5", l.MaxUnmaskedRun(), l.MinMaskedRun())
	}
	if l.KeptDigits() != 4 {
		t.Fatalf("a mask keeps %d digits, want 4", l.KeptDigits())
	}
}

// The value comes from the document and from nowhere else: other valid values come back as they were written.
func TestTheValueIsTheDocumentsAndNothingElse(t *testing.T) {
	for _, v := range []int{4, 5, 6, 32} {
		l, err := masking.Load(strings.NewReader(doc(strconv.Itoa(v))))
		if err != nil || l.MaxUnmaskedRun() != v || l.MinMaskedRun() != v+1 {
			t.Errorf("value %d: got %d/%d, %v", v, l.MaxUnmaskedRun(), l.MinMaskedRun(), err)
		}
	}
}


// Fail closed: every deviation from the contract is a *masking.Error, and no default is returned with it.
func TestAnyDeviationFromTheContractIsRefused(t *testing.T) {
	cases := map[string]string{
		"below the digits a mask keeps": doc("3"),
		"above the contract maximum":    doc("33"),
		"zero":                          doc("0"),
		"negative":                      doc("-4"),
		"fractional":                    doc("4.5"),
		"a string":                      doc(`"4"`),
		"huge":                          doc("9223372036854775807"),
		"missing value":                 `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC"}`,
		"an unknown field":              `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","max_unmasked_digit_run":4,"min_masked_run":5}`,
		"another schema version":        `{"schema_version":"2.0","pack_id":"fin","label":"SYNTHETIC","max_unmasked_digit_run":4}`,
		"a label that is not allowed":   `{"schema_version":"1.0","pack_id":"fin","label":"REAL","max_unmasked_digit_run":4}`,
		"a pack id with a newline":      "{\"schema_version\":\"1.0\",\"pack_id\":\"fin\\n\",\"label\":\"SYNTHETIC\",\"max_unmasked_digit_run\":4}",
		"a pack id in capitals":         `{"schema_version":"1.0","pack_id":"FIN","label":"SYNTHETIC","max_unmasked_digit_run":4}`,
		"a second document":             doc("4") + doc("5"),
		"not JSON":                      "four",
		"empty":                         "",
	}
	for name, in := range cases {
		l, err := masking.Load(strings.NewReader(in))
		var me *masking.Error
		if err == nil || !errors.As(err, &me) {
			t.Errorf("%s: accepted or untyped (%v)", name, err)
			continue
		}
		if l != (masking.Limits{}) {
			t.Errorf("%s: a value came back with the refusal: %+v", name, l)
		}
	}
}

// A Limits that was never loaded is the zero value; it reports that it is not loaded, so a caller can fail closed and never use zero as a limit.
func TestTheZeroValueIsNotLoaded(t *testing.T) {
	var l masking.Limits
	if l.Loaded() {
		t.Fatal("the zero Limits reports it is loaded")
	}
	loaded, _ := masking.Load(strings.NewReader(doc("4")))
	if !loaded.Loaded() {
		t.Fatal("a loaded Limits reports it is not loaded")
	}
}
