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
	got, err := fact("reason_codes", []any{"STATE_KNOWN", "ACCOUNT_0123456789", "1234 5678 9012"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"STATE_KNOWN", "ACCOUNT_******6789", "**** **** 9012"}
	list, ok := got.([]string)
	if !ok || strings.Join(list, "|") != strings.Join(want, "|") {
		t.Fatalf("reason_codes served as %v, want %v", got, want)
	}
	refs, err := fact("disclosed_refs", []any{"fin.transaction:TX88213"}, nil)
	if err != nil || strings.Join(refs.([]string), "|") != "fin.transaction:TX88213" {
		t.Fatalf("disclosed_refs %v, %v: a resource reference is served as written", refs, err)
	}
}

// An integer fact has at most four digits: a number cannot be masked, so a longer one is refused as the contract's range is.
func TestAnIntegerFactHasAtMostFourDigits(t *testing.T) {
	for _, key := range []string{"attempt", "some_count"} {
		if _, err := fact(key, json.Number("9999"), nil); err != nil {
			t.Errorf("%s 9999: %v", key, err)
		}
		if _, err := fact(key, json.Number("12345"), nil); err == nil {
			t.Errorf("%s 12345 was served", key)
		}
	}
}

// An ID is served as written when it has the shape of its key and no run of more than four digits, or when it is a minted ID (the 26 Crockford characters of a ULID) that the Record
// itself cites as an object of this correlation. The shape alone proves nothing, because digits satisfy it (the Security Reviewer on noema#492): 26 digits, or a card number followed
// by letters, is masked unless the Record names it. A request ID is chosen by the caller and is never exempt. A pack ID is data and is masked like text.
func TestAnIdWithALongDigitRunIsServedAsWrittenOnlyWhenTheRecordCitesItAsAMintedId(t *testing.T) {
	const ulid = "01K9ZB4M7Q2XJ5R8T3V6WCD0EF"    // a real ULID has a digit run of at most four
	const longRun = "0123456789ABCDEFGHJKMNPQRS" // minted-shaped, with a run of ten digits
	cited := map[string]bool{"dec_" + longRun: true, "grt_" + ulid: true, "req_" + longRun: true}
	cases := []struct{ key, in, want string }{
		{"request_id", "req_12345678901234", "req_**********1234"},
		{"request_id", "req_" + longRun, "req_******6789ABCDEFGHJKMNPQRS"}, // cited, but the caller chose it: never exempt
		{"decision_id", "dec_" + longRun, "dec_" + longRun},                // minted-shaped and cited: served as written
		{"grant_id", "grt_" + ulid, "grt_" + ulid},
		{"decision_id", "dec_01234567890123456789012345", "dec_**********************2345"}, // 26 digits, not cited
		{"decision_id", "dec_4111111111111111ABCDEFGHIJ", "dec_************1111ABCDEFGHIJ"}, // a card number and letters, not cited
		{"grant_id", "grt_01234567890123456789012345", "grt_**********************2345"},    // 26 digits, not cited
		{"decision_id", "dec_synth0001", "dec_synth0001"},
		{"decision_id", "acct_0123456789", "acct_******6789"},
		{"policy_id", "fin.pol.limit_0123456789", "fin.pol.limit_******6789"},
		{"policy_id", "fin.pol.default.permit_reads", "fin.pol.default.permit_reads"},
		{"intent_id", "int_AB123456CD", "int_AB**3456CD"},
	}
	for _, c := range cases {
		got, err := fact(c.key, c.in, cited)
		if err != nil || got != c.want {
			t.Errorf("%s %q served as %v (%v), want %q", c.key, c.in, got, err, c.want)
		}
	}
}

// A ULID the core minted can still hold a run of digits; the helper must not mistake a masked or invented ID for one the Record cites.
func TestACitedIdIsExemptOnlyWhenItHasTheMintedShape(t *testing.T) {
	known := map[string]bool{"dec_0123456789": true, "dec_0123456789ABCDEFGHJKMNPQRS": true}
	if citedMintedID("decision_id", "dec_0123456789", known) {
		t.Error("a cited ID of ten digits is not minted-shaped and must not be exempt")
	}
	if !citedMintedID("decision_id", "dec_0123456789ABCDEFGHJKMNPQRS", known) {
		t.Error("a cited minted-shaped ID must be exempt")
	}
	if citedMintedID("decision_id", "dec_0123456789ABCDEFGHJKMNPQRS", nil) {
		t.Error("an ID no event cites must not be exempt")
	}
}

// The timeline line is built from the event type, the emitter and the event's kind; it is masked like every other served text.
func TestTheTimelineTextIsMasked(t *testing.T) {
	got := timelineText(stored{eventType: "SECURITY_EVENT", writtenBy: "core-edge", kind: "acct 1234-5678-9012-3456"})
	if strings.Contains(got, "5678") || !strings.HasSuffix(got, "3456") {
		t.Fatalf("timeline text %q, want the digits masked to the last four", got)
	}
}

// A number split by any separator the leak scanner of #222 knows is still the number (the Security Reviewer's note 2 on noema#492): en and em dashes, U+2010 to U+2015, the minus
// sign, the soft hyphen, slashes, underscores, brackets and colons.
func TestDigitsJoinedByDashesSlashesAndBracketsAreMaskedToTheLastFour(t *testing.T) {
	for name, sep := range map[string]string{
		"en dash": "\u2013", "em dash": "\u2014", "hyphen U+2010": "\u2010", "figure dash": "\u2012", "horizontal bar": "\u2015", "minus": "\u2212",
		"soft hyphen": "\u00ad", "slash": "/", "underscore": "_", "colon": ":", "mongolian vowel separator": "\u180e", "combining grapheme joiner": "\u034f",
	} {
		in := "1234" + sep + "5678" + sep + "9012" + sep + "3456"
		want := "****" + sep + "****" + sep + "****" + sep + "3456"
		if got := mask(in); got != want {
			t.Errorf("%s: %q masked as %q, want %q", name, in, got, want)
		}
	}
	if got, want := mask("(1234) 5678"), "(****) 5678"; got != want {
		t.Errorf("a number split by brackets was served as %q, want %q", got, want)
	}
}

// A client's request ID is a lower-case UUID (canonical-input.v2), not a `req_` ID, so the projection's shape for `request_id` never matches it and the UUID is served as text:
// masked wherever it holds a run of more than four digits, separators included, and as written when it does not. This pins what the reconstruction shows today (noema#531, A184);
// a reader matches an input by its correlation and event, never by the client's request ID. SYNTHETIC values.
func TestAClientRequestIdIsServedMaskedWhereItHoldsALongDigitRun(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0b8f3c1e-52a4-4d7e-9a61-123456789012", "0b8f3c1e-52a4-4d7e-9a**-********9012"}, // the tail run of twelve digits is masked, the last four kept
		{"a3c9e1f4-7b2d-4c8a-9e05-6f1d2b8c4a97", "a3c9e1f4-7b2d-4c8a-9e05-6f1d2b8c4a97"}, // no run over four: as written
		{"12345678-1234-4123-8123-123456789012", "********-****-****-****-********9012"}, // all digits: the gateway refuses it at the door, and the projection would mask it
	}
	for _, c := range cases {
		got, err := fact("request_id", c.in, nil)
		if err != nil || got != c.want {
			t.Errorf("request_id %q served as %v (%v), want %q", c.in, got, err, c.want)
		}
	}
}
