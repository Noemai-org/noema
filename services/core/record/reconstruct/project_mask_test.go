package reconstruct

// Security Reviewer's blocking findings on noema#492 (A154: at most 4 digits unmasked on every outward surface): digits split by separators, list facts, bounded integers,
// the ID exemption and the timeline text. REQ-AUD-003, AGENT-13. All values are SYNTHETIC.

import (
	"encoding/json"
	"strings"
	"testing"
)

// A digit run is digits joined by any number of spaces, dots, hyphens or zero-width characters; more than four digits in it keep only the last four, the separators stay.
func TestDigitsJoinedBySeparatorsAreMaskedToTheLastFour(t *testing.T) {
	zw := []string{"\u200b", "\u200c", "\u200d", "\u2060", "\ufeff"}
	cases := map[string]string{
		"1234 5678 9012 3456":                   "**** **** **** 3456",
		"1234-5678-9012-3456":                   "****-****-****-3456",
		"1234.5678.9012.3456":                   "****.****.****.3456",
		"1234  5678":                            "****  5678",
		"card 1234 5678 9012 3456 ok":           "card **** **** **** 3456 ok",
		"\uff11\uff12\uff13\uff14-\uff15\uff16": "**\uff13\uff14-\uff15\uff16",
		"12345":                                 "*2345",
		"ending 0001":                           "ending 0001",
		"1234":                                  "1234",
		"12 34":                                 "12 34",
		"0.41":                                  "0.41",
		"on day 12":                             "on day 12",
	}
	for _, z := range zw {
		cases["1234"+z+"5678"+z+"9012"+z+"3456"] = "****" + z + "****" + z + "****" + z + "3456"
	}
	for _, sp := range []rune{0x00a0, 0x2003, 0x202f, 0x3000} { // Unicode spaces (category Zs) join digits too
		z := string(sp)
		cases["1234"+z+"5678"] = "****" + z + "5678"
	}
	for in, want := range cases {
		if got := mask(in); got != want {
			t.Errorf("mask(%q) = %q, want %q", in, got, want)
		}
	}
	for in := range cases {
		if got := mask(mask(in)); got != mask(in) {
			t.Errorf("mask is not idempotent on %q: %q", in, got)
		}
	}
}

// A list fact is masked item by item; the references of `disclosed_refs` are not a list fact of this kind and stay as written.
func TestAListFactIsMaskedItemByItem(t *testing.T) {
	got, err := fact("reason_codes", []any{"STATE_KNOWN", "ACCOUNT_0123456789", "1234 5678 9012"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"STATE_KNOWN", "ACCOUNT_******6789", "**** **** 9012"}
	list, ok := got.([]string)
	if !ok || strings.Join(list, "|") != strings.Join(want, "|") {
		t.Fatalf("reason_codes served as %v, want %v", got, want)
	}
	refs, err := fact("disclosed_refs", []any{"fin.transaction:TX88213"})
	if err != nil || strings.Join(refs.([]string), "|") != "fin.transaction:TX88213" {
		t.Fatalf("disclosed_refs %v, %v: a resource reference is served as written", refs, err)
	}
}

// An integer fact has at most four digits: a number cannot be masked, so a longer one is refused as the contract's range is.
func TestAnIntegerFactHasAtMostFourDigits(t *testing.T) {
	for _, key := range []string{"attempt", "some_count"} {
		if _, err := fact(key, json.Number("9999")); err != nil {
			t.Errorf("%s 9999: %v", key, err)
		}
		if _, err := fact(key, json.Number("12345")); err == nil {
			t.Errorf("%s 12345 was served", key)
		}
	}
}

// An ID is served as written only in the shape of a real ID: its key's prefix and, when it holds a digit run of more than four, the 26 Crockford characters of an ID the core
// minted. A number dressed in an ID's prefix is masked; a pack ID is data and is masked like text.
func TestAnIdWithALongDigitRunIsServedAsWrittenOnlyWhenItIsAMintedId(t *testing.T) {
	cases := []struct{ key, in, want string }{
		{"request_id", "req_12345678901234", "req_**********1234"},
		{"decision_id", "dec_0123456789ABCDEFGHJKMNPQRS", "dec_0123456789ABCDEFGHJKMNPQRS"},
		{"decision_id", "dec_synth0001", "dec_synth0001"},
		{"decision_id", "acct_0123456789", "acct_******6789"},
		{"policy_id", "fin.pol.limit_0123456789", "fin.pol.limit_******6789"},
		{"policy_id", "fin.pol.default.permit_reads", "fin.pol.default.permit_reads"},
		{"intent_id", "int_AB123456CD", "int_AB**3456CD"},
	}
	for _, c := range cases {
		got, err := fact(c.key, c.in)
		if err != nil || got != c.want {
			t.Errorf("%s %q served as %v (%v), want %q", c.key, c.in, got, err, c.want)
		}
	}
}

// The timeline line is built from the event type, the emitter and the event's kind; it is masked like every other served text.
func TestTheTimelineTextIsMasked(t *testing.T) {
	got := timelineText(stored{eventType: "SECURITY_EVENT", writtenBy: "core-edge", kind: "acct 1234-5678-9012-3456"})
	if strings.Contains(got, "5678") || !strings.HasSuffix(got, "3456") {
		t.Fatalf("timeline text %q, want the digits masked to the last four", got)
	}
}
