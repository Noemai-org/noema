package payloadguard_test

// A194 (the one digit and separator definition lives in record/payloadguard), A154 (at most four digits unmasked on every outward surface), A231 (interim: the guard uses the
// display mask's wide separator set; the one exemption is an UPPER_SNAKE reason-code token with at most 6 digits in all and at most 3 in a group), REQ-AUD-003, AGENT-13: the
// display mask (record/reconstruct serves masked facts) and the write-side guard share ONE definition of a run of digits, with no second mode. Every value is SYNTHETIC.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode"

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
	alphabet := []rune{'0', '1', '2', '7', '9', '\uff13', '\u0663', ' ', '.', '-', '/', '_', '(', ')', ':', ',', 'a', 'Z', '\u200b', '\u2060', '\u00a0', '\u2013', '\v', '\u0085', '\u2028', '\u2011', '\u00b7', '\uff0e', '\u30fb', '[', ']', '{', '}', 'X'}
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

// A231: the guard and the mask are one definition. A number split by any of these is a number to a reader, so the guard refuses it as the mask masks it. Each separator is a
// character the first guard (spaces, dots, hyphens, slashes, format characters, combining marks) did not join, or a script's own form of one that it did.
func TestTheGuardRefusesANumberSplitByAnySeparatorOfTheWideSet(t *testing.T) {
	separators := map[string]string{
		"underscore": "_", "colon": ":", "round brackets": ")(", "square brackets": "][", "braces": "}{", "vertical tab": "\v", "next line (U+0085)": "\u0085",
		"line separator (U+2028)": "\u2028", "paragraph separator (U+2029)": "\u2029", "non-breaking hyphen (U+2011)": "\u2011", "small hyphen-minus (U+FE63)": "\ufe63",
		"full-width hyphen-minus (U+FF0D)": "\uff0d", "double oblique hyphen (U+2E17)": "\u2e17", "wave dash (U+301C)": "\u301c", "middle dot (U+00B7)": "\u00b7",
		"katakana middle dot (U+30FB)": "\u30fb", "full-width full stop (U+FF0E)": "\uff0e", "full-width solidus (U+FF0F)": "\uff0f", "full-width colon (U+FF1A)": "\uff1a",
		"full-width low line (U+FF3F)": "\uff3f", "two spaces": "  ", "space hyphen space": " - ", "ideographic space": "\u3000",
	}
	for name, sep := range separators {
		text := "4111" + sep + "1111" + sep + "1111" + sep + "1111"
		raw, err := json.Marshal(map[string]string{"note": text})
		if err != nil {
			t.Fatal(err)
		}
		if err := payloadguard.Check(raw); err == nil {
			t.Errorf("a number split by %s (%q) was accepted", name, text)
		}
		if got := payloadguard.Mask(text); strings.Count(got, "*") != 12 {
			t.Errorf("Mask(%q) = %q: a number split by %s is not masked to its last four digits", text, got, name)
		}
	}
}

// A231, the one exemption: an UPPER_SNAKE reason-code token with at most 6 digits in all and at most 3 in a group. VERSION_CONFLICT_409_412 is the core's own code and must
// pass; the same shape carrying a card number must not.
func TestTheOneExemptionIsAShortUpperSnakeReasonCode(t *testing.T) {
	accepted := []string{"VERSION_CONFLICT_409_412", "CONFLICT_409_412", "NO_ADAPTER_FOR_CAPABILITY", "HTTP_503", "ERR_1_2_3_4_5_6", "ABC_123_456",
		// the receipt's raw_status is the transport result, then a colon and a raw code when there is one (execution/ledger/complete.go): the Record stores these
		"VERSION_CONFLICT_409_412:409", "DUPLICATE_CREATE:412:PRECONDITION_FAILED", "OUTCOME_UNKNOWN", "REJECTED_4XX:404", "CONFLICT_409_412:409:CONFLICT"}
	refused := []string{
		"X_4111_1111_1111_1111",         // a card number behind a prefix
		"X_411_111_111_111",             // groups of three, but twelve digits in all
		"ABC_123_456_7",                 // seven digits in all
		"CONFLICT_4094_412",             // a group of four
		"CODE41111_X",                   // a group that holds five digits (four digits alone are within the limit and need no exemption)
		"conflict_409_412",              // not UPPER_SNAKE
		"Conflict_409_412",              // not UPPER_SNAKE
		"409_412",                       // no letter: not a code
		"_CONFLICT_409_412",             // does not start with a letter
		"CONFLICT_409_412 4111111111",   // more than the token
		"CONFLICT__409_412",             // an empty group
		"CONFLICT_409_412_",             // a trailing underscore
		"CONFLICT-409-412",              // not snake case
		"X_4111:1111:1111:1111",         // a number behind a code, split by colons
		"A:411:111:111:111",             // more raw-code parts than a receipt has
		"VERSION_CONFLICT_409_412:4094", // a raw code of four digits
		"CONFLICT_409_412:409:412",      // ten digits or more in all
		"ABC_123_456:789:123",           // twelve digits
		"CONFLICT_409_412:",             // an empty raw code
		"CONFLICT_409_412:abc",          // a raw code that is not a status or an UPPER_SNAKE word
	}
	for _, s := range accepted {
		raw, _ := json.Marshal(map[string]string{"code": s})
		if err := payloadguard.Check(raw); err != nil {
			t.Errorf("%q was refused: %v", s, err)
		}
		if got := payloadguard.Mask(s); got != s {
			t.Errorf("Mask(%q) = %q: an exempt code is shown whole", s, got)
		}
	}
	for _, s := range refused {
		raw, _ := json.Marshal(map[string]string{"code": s})
		if err := payloadguard.Check(raw); err == nil {
			t.Errorf("%q was accepted", s)
		}
	}
}

// Security Reviewer on noema#600 (note 2): every code point of the Unicode decimal-digit class, in a run of five, is refused, so no script's digits slip past the class.
func TestEveryUnicodeDecimalDigitCountsAsADigit(t *testing.T) {
	n := 0
	for _, rng := range unicode.Nd.R16 {
		for r := rng.Lo; r <= rng.Hi; r += rng.Stride {
			checkDigit(t, rune(r))
			n++
		}
	}
	for _, rng := range unicode.Nd.R32 {
		for r := rng.Lo; r <= rng.Hi; r += rng.Stride {
			checkDigit(t, rune(r))
			n++
		}
	}
	if n < 600 {
		t.Fatalf("only %d code points of unicode.Nd were walked", n)
	}
}

func checkDigit(t *testing.T, r rune) {
	t.Helper()
	text := strings.Repeat(string(r), 5)
	raw, err := json.Marshal(map[string]string{"note": text})
	if err != nil {
		t.Fatal(err)
	}
	if err := payloadguard.Check(raw); err == nil {
		t.Errorf("five of U+%04X were accepted", r)
	}
}
