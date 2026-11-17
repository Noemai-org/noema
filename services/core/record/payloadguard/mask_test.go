package payloadguard_test

// A194 (the one digit and separator definition lives in record/payloadguard), A154 (at most four digits unmasked on every outward surface), A231 (interim: the guard uses the
// display mask's wide separator set; the one exemption is an UPPER_SNAKE reason-code token with at most 6 digits in all and at most 3 in a group), REQ-AUD-003, AGENT-13: the
// display mask (record/reconstruct serves masked facts) and the write-side guard share ONE definition of a run of digits, with no second mode. Every value is SYNTHETIC.

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
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

// A231 as narrowed by Codex's P1 and the Security Reviewer on noema#600: the one exemption is the transport class a RECEIPT carries, at its two exact payload paths and for its finite
// vocabulary only. VERSION_CONFLICT_409_412 (six digits) is the core's own class and its receipt must be written; the same string, or any string shaped like a code, at another path
// is refused, so no nine-digit identifier can travel as a "code".
func TestTheOneExemptionIsTheReceiptsTransportClassAtItsTwoPathsOnly(t *testing.T) {
	check := func(path, value string) error {
		raw, err := json.Marshal(map[string]string{strings.TrimPrefix(path, "/"): value})
		if err != nil {
			t.Fatal(err)
		}
		return payloadguard.Check(raw)
	}
	accepted := map[string][]string{
		"/transport_result": {"VERSION_CONFLICT_409_412", "DUPLICATE_CREATE", "TIMEOUT", "NO_RESPONSE", "REJECTED_4XX", "ACK_2XX"},
		"/raw_status":       {"VERSION_CONFLICT_409_412:409", "DUPLICATE_CREATE:412:PRECONDITION_FAILED", "TIMEOUT", "REJECTED_4XX:404", "VERSION_CONFLICT_409_412:409:CONFLICT"},
	}
	for path, values := range accepted {
		for _, v := range values {
			if err := check(path, v); err != nil {
				t.Errorf("%q at %s was refused: %v", v, path, err)
			}
		}
	}
	// the same strings anywhere else are refused (Codex's P1: a code-shaped string must not bypass the guard on an unrelated field)
	for _, path := range []string{"/note", "/reason", "/raw_code", "/status", "/transport_result_x", "/event_transport_result"} {
		for _, v := range []string{"VERSION_CONFLICT_409_412", "VERSION_CONFLICT_409_412:409", "ABC_123_456", "X_123_456_789"} {
			if err := check(path, v); err == nil {
				t.Errorf("%q at %s was accepted", v, path)
			}
		}
	}
	// nested: not the receipt's own top-level member
	raw, _ := json.Marshal(map[string]any{"detail": map[string]string{"raw_status": "VERSION_CONFLICT_409_412:409"}})
	if err := payloadguard.Check(raw); err == nil {
		t.Error("a nested raw_status was accepted")
	}
	// at the two paths, only the vocabulary and the shapes of the raw code: a number cannot ride in them
	refusedAtRawStatus := []string{
		"X_411_111_111_111", "ABC_123_456_789", "X:123:456", "A:411:111:111:111", "VERSION_CONFLICT_409_412:4094", "VERSION_CONFLICT_409_412:409:412",
		"VERSION_CONFLICT_409_412:", "VERSION_CONFLICT_409_412:abc", "VERSION_CONFLICT_409_412:409:411:111", "version_conflict_409_412:409", "VERSION_CONFLICT_409_412 4111111111",
		"VERSION_CONFLICT_409_412:409:412:123", "CONFLICT_409_412:409", "4111111111111111", "TIMEOUT:123456789",
	}
	for _, v := range refusedAtRawStatus {
		if err := check("/raw_status", v); err == nil {
			t.Errorf("%q at /raw_status was accepted", v)
		}
	}
	for _, v := range []string{"X_4111_1111_1111_1111", "CONFLICT_409_412", "ABC_123_456", "version_conflict_409_412", "VERSION_CONFLICT_409_412:409"} {
		if err := check("/transport_result", v); err == nil {
			t.Errorf("%q at /transport_result was accepted", v)
		}
	}
}

// The vocabulary the guard keeps is the controller's TransportClass constants, read from the source (record/ imports no execution package): a class added there with digits fails here.
func TestTheTransportClassesAreTheControllersConstants(t *testing.T) {
	src, err := os.ReadFile("../../execution/controller/types.go")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	re := regexp.MustCompile(`(?m)^\s*Transport[A-Za-z0-9]+\s+TransportClass\s*=\s*"([A-Z0-9_]+)"`)
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		want = append(want, m[1])
	}
	if len(want) < 9 {
		t.Fatalf("only %d TransportClass constants found in the controller", len(want))
	}
	got := payloadguard.TransportClasses()
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the guard's transport classes %v are not the controller's %v", got, want)
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
