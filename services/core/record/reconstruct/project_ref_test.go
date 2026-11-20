package reconstruct

// A331 (relayed Architect ruling, interim; not a committed decision), REQ-AUD-003, REQ-SEC-003, REQ-UI-001: a resource reference is masked in the projector like any other served
// string, so that no run over four digits reaches a reader through `resolved_target` or `disclosed_refs` (the schema's reference branch accepts a letter anywhere in a body of up
// to 64 characters, so `fin.account:` and forty digits and `a` passed it), and a money string keeps its exemption only in customer-visible text (the `text` fact of a claim), not in
// any other fact. All values are SYNTHETIC.

import (
	"regexp"
	"strings"
	"testing"
)

var fiveDigits = regexp.MustCompile(`[0-9]{5}`)

var maskedRefs = map[string]string{
	"fin.account:0123456789012345":                         "fin.account:************2345",
	"fin.account:" + strings.Repeat("0123456789", 4) + "a": "fin.account:" + strings.Repeat("*", 36) + "6789a",
	"fin.account:0123-4567-8901-2345":                      "fin.account:****-****-****-2345",
	"fin.transaction:TX8821":                               "fin.transaction:TX8821",
	"test.transaction:TX0001":                              "test.transaction:TX0001",
	"fin.transaction:TX88213":                              "fin.transaction:TX*8213",
	"fin.case:ab1234cd5678":                                "fin.case:ab1234cd5678",
	"fin.account:" + strings.Repeat("1", 64):               "fin.account:" + strings.Repeat("*", 60) + "1111",
}

func TestAResourceReferenceIsServedMaskedToTheLastFourDigits(t *testing.T) {
	for in, want := range maskedRefs {
		t.Run(in, func(t *testing.T) {
			got, err := fact("resolved_target", in, nil)
			if err != nil || got != want {
				t.Errorf("resolved_target %q served as %v (%v), want %q", in, got, err, want)
			}
			refs, err := fact("disclosed_refs", []any{in}, nil)
			if err != nil || len(refs.([]string)) != 1 || refs.([]string)[0] != want {
				t.Errorf("disclosed_refs [%q] served as %v (%v), want [%q]", in, refs, err, want)
			}
			if s, _ := got.(string); fiveDigits.MatchString(s) {
				t.Errorf("the served reference %q holds five digits in a row", s)
			}
		})
	}
}

// Masking does not make a bad reference good: a value that is not a resource reference is still refused, before it is masked.
func TestAReferenceThatIsNotOneIsStillRefused(t *testing.T) {
	for _, bad := range []string{"not a ref", "fin.account:", "fin.account:" + strings.Repeat("1", 65), "FIN.account:1", "fin.account:12 345"} {
		if _, err := fact("resolved_target", bad, nil); err == nil {
			t.Errorf("resolved_target %q was served", bad)
		}
		if _, err := fact("disclosed_refs", []any{bad}, nil); err == nil {
			t.Errorf("disclosed_refs [%q] was served", bad)
		}
	}
}

// Money keeps its exemption in customer-visible text only: the claim's `text` fact is served as it is, every other fact, a plain scalar and a list item included, is masked.
func TestMoneyIsServedAsItIsInTheClaimTextOnly(t *testing.T) {
	const amount = "NGN 12,345.00"
	if got, err := fact("text", amount, nil); err != nil || got != amount {
		t.Errorf("the text fact served %v (%v) for %q, want it as it is", got, err, amount)
	}
	if got, err := fact("amount_text", amount, nil); err != nil || got != "NGN 12,*45.00" {
		t.Errorf("a plain scalar fact served %v (%v) for %q, want it masked", got, err, amount)
	}
	if got, err := fact("reason_codes", []any{amount}, nil); err != nil || got.([]string)[0] != "NGN 12,*45.00" {
		t.Errorf("a list fact served %v (%v) for %q, want it masked", got, err, amount)
	}
}
