package payloadguard_test

// A194 (the one digit and separator definition lives in record/payloadguard), A154 (at most four digits unmasked on every outward surface), REQ-AUD-003, AGENT-13: the
// display mask (record/reconstruct serves masked facts) and the write-side guard share one definition of a run of digits. The mask is the stricter of the two on purpose: it also
// joins digits across an underscore, a bracket and a colon, because a number split that way is still the number to a reader; the guard does not, because a code such as
// VERSION_CONFLICT_409_412 is not a number and a false refusal stops a write. Every value is SYNTHETIC.

import (
	"encoding/json"
	"os"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/platform/masking"
	"github.com/Noemai-org/noema/services/core/record/payloadguard"
)

func TestMaskKeepsTheLastFourDigitsOfEveryRunAndTheSeparatorsStay(t *testing.T) {
	cases := map[string]string{
		"1234 5678 9012 3456":                   "**** **** **** 3456",
		"1234-5678-9012-3456":                   "****-****-****-3456",
		"1234.5678.9012.3456":                   "****.****.****.3456",
		"card 1234 5678 9012 3456 ok":           "card **** **** **** 3456 ok",
		"\uff11\uff12\uff13\uff14-\uff15\uff16": "**\uff13\uff14-\uff15\uff16",
		"1234\u200b5678\u200b9012":              "****\u200b****\u200b9012",
		"12345":                                 "*2345",
		"1234":                                  "1234",
		"12 34":                                 "12 34",
		"ending 0001":                           "ending 0001",
		"0.41":                                  "0.41",
		// The display mask also joins across an underscore, a bracket and a colon (the leak scanner's longer set): a number split that way is still the number.
		"1234_5678_9012_3456": "****_****_****_3456",
		"(1234)(5678)":        "(****)(5678)",
		"12:34:56":            "**:34:56",
	}
	for in, want := range cases {
		if got := payloadguard.Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
		if got := payloadguard.Mask(payloadguard.Mask(in)); got != payloadguard.Mask(in) {
			t.Errorf("Mask is not idempotent on %q: %q", in, got)
		}
	}
}

// Whatever string is masked, the write-side guard finds no run of more than four digits in the result: the two definitions cannot drift apart (one is a stricter
// superset of the other). A string is drawn from digits of three scripts, every separator of either definition, zero-width characters and letters.
func TestAMaskedStringNeverHoldsARunTheGuardRefuses(t *testing.T) {
	alphabet := []rune{'0', '1', '2', '7', '9', '\uff13', '\u0663', ' ', '.', '-', '/', '_', '(', ')', ':', ',', 'a', 'Z', '\u200b', '\u2060', '\u00a0', '\u2013'}
	rapid.Check(t, func(rt *rapid.T) {
		s := string(rapid.SliceOfN(rapid.SampledFrom(alphabet), 0, 40).Draw(rt, "s"))
		raw, err := json.Marshal(map[string]string{"note": payloadguard.Mask(s)})
		if err != nil {
			rt.Fatal(err)
		}
		if err := payloadguard.Check(raw); err != nil {
			rt.Fatalf("Mask(%q) = %q is refused by the guard: %v", s, payloadguard.Mask(s), err)
		}
	})
}

// The limit is the one number of masking-limits.v1: if the pack's limit and the guard's ever differ, this fails (A194, A154 as corrected).
func TestTheGuardsLimitIsTheMaskingLimitOfThePack(t *testing.T) {
	f, err := os.Open("../../../../packs/fintech/limits/masking_limits.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := masking.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if l.MaxUnmaskedRun() != payloadguard.Limit {
		t.Fatalf("masking-limits allows %d unmasked digits, the guard's limit is %d", l.MaxUnmaskedRun(), payloadguard.Limit)
	}
}
