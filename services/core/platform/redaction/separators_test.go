package redaction_test

// SEC-03 follow-up (REQ-SEC-003, REQ-OBS-002, AGENT-13; A154, A259, A289): the redactor joins the digits of a run with the same separators as the Record's append guard
// (record/payloadguard), no fewer. A number written "4111  1111  1111  1111", "4111_1111_1111_1111" or "4111.1111.1111.1111" is one run of sixteen and is masked to its last four
// digits. The secret-looking key rules are tested one word at a time, and a secret inside a VALUE under a key that is not secret-looking is removed too. All data is SYNTHETIC.

import (
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/Noemai-org/noema/services/core/platform/redaction"
	"github.com/Noemai-org/noema/services/core/record/payloadguard"
	"pgregory.net/rapid"
)

// guardJoins is the reference: true when payloadguard (the one definition of a run, A194, A231) treats c between two groups of four digits as part of one run of eight.
func guardJoins(c rune) bool {
	s := "1111" + string(c) + "1111"
	return payloadguard.Mask(s) != s
}

var (
	sepOnce sync.Once
	sepList []rune
)

// separatorRunes lists every code point the guard joins digits with, found by asking the guard, never by a list kept here. A decimal digit is not counted (digits join digits).
func separatorRunes() []rune {
	sepOnce.Do(func() {
		for c := rune(0); c <= unicode.MaxRune; c++ {
			if (c >= 0xD800 && c <= 0xDFFF) || unicode.Is(unicode.Nd, c) {
				continue
			}
			if guardJoins(c) {
				sepList = append(sepList, c)
			}
		}
	})
	return sepList
}

// core keeps the digits and the asterisks of s: which digits a masking leaves visible, whatever the text and the separators around them.
func core(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c == '*' || unicode.Is(unicode.Nd, c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

var digitAlphabets = [][]rune{
	[]rune("0123456789"),
	[]rune("٠١٢٣٤٥٦٧٨٩"),
	[]rune("०१२३४५६७८९"),
	[]rune("０１２３４５６７８９"),
}

// wideRun is a run of five to sixteen digits, each from any of the four alphabets, joined by zero to three separators of the shared set.
func wideRun(rt *rapid.T) string {
	var b strings.Builder
	for i, n := 0, rapid.IntRange(5, 16).Draw(rt, "digits"); i < n; i++ {
		if i > 0 {
			for j, k := 0, rapid.IntRange(0, 3).Draw(rt, "separators"); j < k; j++ {
				b.WriteRune(rapid.SampledFrom(separatorRunes()).Draw(rt, "separator"))
			}
		}
		a := rapid.SampledFrom(digitAlphabets).Draw(rt, "alphabet")
		b.WriteRune(a[rapid.IntRange(0, 9).Draw(rt, "digit")])
	}
	return b.String()
}

// The probes of the finding: a run of sixteen digits, masked to its last four, whatever joins the groups.
func TestADigitRunJoinedByTheWideSeparatorSetIsMaskedToItsLastFour(t *testing.T) {
	r := redaction.New()
	stars := strings.Repeat("*", 12)
	for name, c := range map[string]struct{ in, want string }{
		"two spaces":                {"4111  1111  1111  1111", stars + "1111"},
		"underscore":                {"4111_1111_1111_1111", stars + "1111"},
		"dot":                       {"4111.1111.1111.1111", stars + "1111"},
		"one space":                 {"4111 1111 1111 1111", stars + "1111"},
		"hyphen":                    {"4111-1111-1111-1111", stars + "1111"},
		"slash":                     {"4111/1111/1111/1111", stars + "1111"},
		"colon":                     {"4111:1111:1111:1111", stars + "1111"},
		"space, hyphen, space":      {"4111 - 1111 - 1111 - 1111", stars + "1111"},
		"tab and newline":           {"4111\t1111\n1111\r\n1111", stars + "1111"},
		"no-break space":            {"4111 1111 1111 1111", stars + "1111"},
		"zero-width space":          {"4111​1111​1111​1111", stars + "1111"},
		"soft hyphen":               {"4111­1111­1111­1111", stars + "1111"},
		"minus sign":                {"4111−1111−1111−1111", stars + "1111"},
		"middle dot":                {"4111·1111·1111·1111", stars + "1111"},
		"full-width dot":            {"4111．1111．1111．1111", stars + "1111"},
		"combining mark":            {"4111́1111́1111́1111", stars + "1111"},
		"brackets":                  {"(4111)(1111)(1111)(1111)", "(" + stars + "1111)"},
		"next line":                 {"4111\u00851111\u00851111\u00851111", stars + "1111"},
		"line separator":            {"4111 1111 1111 1111", stars + "1111"},
		"full-width digits":         {"４１１１　１１１１　１１１１　１１１１", stars + "１１１１"},
		"Arabic-Indic, two dots":    {"٤١١١..١١١١..١١١١..١١١١", stars + "١١١١"},
		"Devanagari, underscores":   {"४१११_११११_११११_११११", stars + "११११"},
		"ASCII and Arabic-Indic":    {"4111  ١١١١  4111  ١١١١", stars + "١١١١"},
		"six digits, two spaces":    {"12345  6", "**3456"},
		"inside a sentence":         {"card 4111  1111  1111  1111 expired", "card " + stars + "1111 expired"},
		"two numbers in one string": {"4111_1111_1111_1111 and 5500.0000.0000.0004", stars + "1111 and " + stars + "0004"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := r.MaskAccountNumbers(c.in); got != c.want {
				t.Errorf("MaskAccountNumbers(%q) = %q, want %q", c.in, got, c.want)
			}
			if got := r.FilterAttributes(map[string]string{"k": c.in})["k"]; got != c.want {
				t.Errorf("FilterAttributes(%q) = %q, want %q", c.in, got, c.want)
			}
			out := logged(t, func(l *slog.Logger) { l.Info("n "+c.in, "account", c.in, "e", errors.New(c.in)) })
			if hasRun(out) {
				t.Errorf("a run of more than four digits reached the log: %s", out)
			}
		})
	}
}

// Every code point is asked: the redactor joins digits with c exactly when the Record guard does (A259: one separator definition). This is the divergence check: if either side changes, it fails.
func TestEveryCodePointJoinsDigitsExactlyWhenTheRecordGuardSaysSo(t *testing.T) {
	r := redaction.New()
	n := 0
	for c := rune(0); c <= unicode.MaxRune; c++ {
		if (c >= 0xD800 && c <= 0xDFFF) || unicode.Is(unicode.Nd, c) { // a digit between two groups is not a separator, it is a digit
			continue
		}
		in := "1111" + string(c) + "1111"
		got := r.MaskAccountNumbers(in)
		if guardJoins(c) {
			n++
			if got != "****1111" {
				t.Fatalf("U+%04X between two groups of four digits is a separator for the Record guard but not for the redactor: %q -> %q", c, in, got)
			}
		} else if got != in {
			t.Fatalf("U+%04X is no separator for the Record guard but the redactor changed %q -> %q", c, in, got)
		}
	}
	if n < 100 {
		t.Errorf("only %d separator code points found: the reference is empty or the probe is wrong", n)
	}
}

// The characters the rulings name are in the set whatever the reference says (the reference could shrink; these may not).
func TestTheSeparatorsTheRulingsNameAllJoinDigits(t *testing.T) {
	r := redaction.New()
	named := []rune{' ', '\t', '\n', '\v', '\f', '\r', 0x85, 0xa0, 0x2028, 0x2029, 0x200b, 0x200d, 0xad, 0xfeff, 0x0301, 0x20d0, '-', 0x2010, 0x2013, 0x2212, '(', ')', '[', ']', '{', '}', '.', '/', '_', ':', 0xb7, 0x30fb, 0xff0e, 0xff0f, 0xff1a, 0xff3f}
	for _, c := range named {
		in := "1111" + string(c) + "1111"
		if got := r.MaskAccountNumbers(in); got != "****1111" {
			t.Errorf("U+%04X does not join digits: %q -> %q", c, in, got)
		}
	}
	for _, c := range []rune{',', ';', 'a', '*', '+', '#', '@'} {
		in := "1111" + string(c) + "1111"
		if got := r.MaskAccountNumbers(in); got != in {
			t.Errorf("%q must end a run: %q -> %q", c, in, got)
		}
	}
}

// Any NUMBER of separators between two digits, and any mix of them, is one run.
func TestAnyNumberOfSeparatorsInARowJoinsDigits(t *testing.T) {
	r := redaction.New()
	seps := separatorRunes()
	for i, c := range seps {
		d := seps[(i*7+3)%len(seps)]
		for _, mid := range []string{string(c) + string(c) + string(c), string(c) + string(d), strings.Repeat(string(d), 5)} {
			in := "12345" + mid + "6789"
			if got := r.MaskAccountNumbers(in); got != "*****6789" {
				t.Fatalf("%q -> %q, want *****6789", in, got)
			}
		}
	}
}

// Digits of another script and a mix of scripts, joined by every separator, with five or more digits.
func TestEveryScriptAndEverySeparatorIsOneRun(t *testing.T) {
	r := redaction.New()
	for _, a := range digitAlphabets {
		for _, c := range separatorRunes() {
			in := string(a[1]) + string(a[2]) + string(a[3]) + string(a[4]) + string(a[5]) + string(c) + string(a[6]) + string(a[7]) + string(a[8]) + string(a[9])
			want := "*****" + string(a[6]) + string(a[7]) + string(a[8]) + string(a[9])
			if got := r.MaskAccountNumbers(in); got != want {
				t.Fatalf("U+%04X in a %c run: %q -> %q, want %q", c, a[1], in, got, want)
			}
		}
	}
}

// The redactor and the Record guard keep the same digits on a table of inputs, text around them included (the separators drop out of the redactor's output, the digits and asterisks
// do not move).
func TestTheRedactorLeavesVisibleTheSameDigitsAsTheRecordGuard(t *testing.T) {
	r := redaction.New()
	for _, in := range []string{
		"pay 20,000,000 now", "pay 20.000.000 now", "ref 4111,1111,1111,1111", "4111", "41 11", "4-1-1-1", "(41)(11)", "41111", "0123456789", "a1b2c3d4e5f6",
		"٠١٢٣٤ ٥٦٧٨٩", "4111\n1111\n1111\n1111", "4111​1111​1111​1111", "４１１１　１１１１　１１１１　１１１１", "4111 1111 1111 1111 and 5500 0000 0000 0004",
		"ip 192.168.100.200", "12/30", "1 2 3 4 5", "1,2,3,4,5,6,7,8", "x_1111_1111_y", "no digits at all",
	} {
		got := r.MaskAccountNumbers(in)
		if core(got) != core(payloadguard.Mask(in)) {
			t.Errorf("%q: the redactor leaves %q visible, the Record guard %q", in, core(got), core(payloadguard.Mask(in)))
		}
		if hasRun(got) {
			t.Errorf("%q -> %q still holds a run of more than four digits", in, got)
		}
	}
}

// A comma ends a run, as in the guard: groups of three joined by commas are an amount, not a number to hide.
func TestACommaStillEndsARunAndFourDigitsJoinedAreNotMasked(t *testing.T) {
	r := redaction.New()
	for _, in := range []string{"20,000,000", "pay 1,234,567,890 now", "4111,1111,1111,1111", "4111", "41 11", "4-1-1-1", "4​1​1​1", "(41)(11)", "4.1.1.1", "4_1_1_1", "x4111y", "v1.2"} {
		if got := r.MaskAccountNumbers(in); got != in {
			t.Errorf("%q must stay readable: got %q", in, got)
		}
	}
	rapid.Check(t, func(rt *rapid.T) {
		groups := rapid.SliceOfN(rapid.StringMatching(`[0-9]{1,4}`), 1, 6).Draw(rt, "groups")
		in := strings.Join(groups, ",")
		if got := r.MaskAccountNumbers(in); got != in {
			rt.Fatalf("%q -> %q: a comma-separated list of groups of at most four digits must stay readable", in, got)
		}
	})
	if got := r.MaskAccountNumbers("41111"); got != "*1111" {
		t.Errorf("five digits must be masked: %q", got)
	}
}

// A property over generated runs (any of four scripts, any of the separators, 5 to 16 digits): the output has no run by the guard's definition, and it keeps the digits the guard keeps.
func TestAGeneratedRunOfAnyScriptAndSeparatorIsMasked(t *testing.T) {
	r := redaction.New()
	rapid.Check(t, func(rt *rapid.T) {
		run := wideRun(rt)
		in := rapid.SampledFrom([]string{"", "acct ", "n=", "id:x"}).Draw(rt, "before") + run + rapid.SampledFrom([]string{"", " ok", ",5"}).Draw(rt, "after")
		got := r.MaskAccountNumbers(in)
		if hasRun(got) {
			rt.Fatalf("%q -> %q: a run of more than four digits remains", in, got)
		}
		if core(got) != core(payloadguard.Mask(in)) {
			rt.Fatalf("%q -> %q: the redactor shows %q, the Record guard %q", in, got, core(got), core(payloadguard.Mask(in)))
		}
		if out := logged(t, func(l *slog.Logger) { l.Info(in, "v", in) }); hasRun(out) {
			rt.Fatalf("%q: a run of more than four digits reached the log: %s", in, out)
		}
	})
}

// The secret-looking key words, one subtest each: a key that holds the word has its value removed, so removing a word from the pattern turns exactly its subtest red. Each key below
// matches through that word alone (the word "authorization" is also matched by "auth"; its subtest cannot tell the two apart).
func TestEverySecretKeyWordRemovesItsValue(t *testing.T) {
	const value = "synthetic-value-7f3a"
	for word, keys := range map[string][]string{
		"api_key":       {"api_key", "API-KEY", "x_apikey"},
		"authorization": {"Authorization"},
		"auth":          {"auth", "x_auth_y"},
		"token":         {"token", "csrf_token", "TokenValue"},
		"secret":        {"secret", "client_secret"},
		"password":      {"password", "db_password"},
		"passwd":        {"passwd", "db_passwd"},
		"pwd":           {"pwd", "db_pwd", "PWD", "x_pwd_y"},
		"credential":    {"credential", "credentials"},
		"bearer":        {"bearer", "x_bearer"},
		"private_key":   {"private_key", "private-key", "privatekey"},
		"signing_key":   {"signing_key", "signing-key", "signingkey"},
		"raw_audio":     {"raw_audio", "raw-audio", "rawaudio"},
		"cookie":        {"cookie", "Set-Cookie", "session_cookie"},
		"pin":           {"pin", "PIN", "user_pin", "userPin", "userPIN", "new-pin", "pin2", "pin_code", "PINCode"},
	} {
		t.Run(word, func(t *testing.T) {
			for _, key := range keys {
				out := logged(t, func(l *slog.Logger) { l.Info("m", key, value, slog.Group("g", key, value)) })
				if strings.Contains(out, value) || !strings.Contains(out, redaction.Redacted) {
					t.Errorf("the value under %q reached the log: %s", key, out)
				}
				if got := redaction.New().FilterAttributes(map[string]string{key: value})[key]; got != redaction.Redacted {
					t.Errorf("the trace attribute %q is %q, want %q", key, got, redaction.Redacted)
				}
			}
		})
	}
}

// "pin" is a word, not a substring: a key that merely contains the letters keeps its value (the secret-key rule fails closed, but not on "shipping").
func TestAKeyThatOnlyContainsThePinLettersIsNotASecretKey(t *testing.T) {
	for _, key := range []string{"shipping_address", "mapping_id", "spinner", "pinned", "typing", "SHIPPING", "MAPPING_ID", "pinball"} {
		out := logged(t, func(l *slog.Logger) { l.Info("m", key, "kept-value") })
		if !strings.Contains(out, `"kept-value"`) {
			t.Errorf("the value under %q was removed: %s", key, out)
		}
	}
}

// A secret inside a VALUE, under a key that is not secret-looking (A154's sibling: the key list alone is a name guess). The forms below are the ones an HTTP client, a URL, a JSON body or a
// header dump puts into a message, an error or a field.
func TestASecretInsideAValueUnderAnUnlistedKeyIsRemoved(t *testing.T) {
	r := redaction.New()
	for name, c := range map[string]struct{ in, secret string }{
		"a header line":          {"Authorization: Bearer sk-live-abc", "sk-live-abc"},
		"a lower-case header":    {"authorization=Basic dXNlcjpwYXNz", "dXNlcjpwYXNz"},
		"a bare bearer token":    {"sent Bearer sk-live-abcdef12", "sk-live-abcdef12"},
		"a query parameter":      {"GET /v1/x?api_key=SYNTH123&a=1", "SYNTH123"},
		"a JSON body":            {`{"password":"hunter2-synthetic","a":1}`, "hunter2-synthetic"},
		"a quoted value":         {`token: "abc def synth"`, "abc def synth"},
		"a password with spaces": {"password = 'two words'", "two words"},
		"a cookie":               {"cookie: session=SYNTHSESSION1", "SYNTHSESSION1"},
		"a pin":                  {"pin=4821", "4821"},
		"a private key":          {"private_key=SYNTHKEY", "SYNTHKEY"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := r.FilterAttributes(map[string]string{"note": c.in})["note"]; strings.Contains(got, c.secret) {
				t.Errorf("FilterAttributes kept the secret: %q -> %q", c.in, got)
			}
			out := logged(t, func(l *slog.Logger) {
				l.Info("failed: "+c.in, "note", c.in, "err", errors.New(c.in), slog.Group("detail", slog.String("text", c.in)))
			})
			if strings.Contains(out, c.secret) {
				t.Errorf("the secret reached the log: %s", out)
			}
			if !strings.Contains(out, redaction.Redacted) {
				t.Errorf("no [REDACTED] marker in the log: %s", out)
			}
		})
	}
}

// What is not a secret in a value stays readable: the scan looks for a key word followed by a colon or an equals sign, or the word "bearer" followed by a token.
func TestOrdinaryTextIsNotMistakenForASecretInAValue(t *testing.T) {
	r := redaction.New()
	for _, in := range []string{"password reset requested", "max_tokens=100", "token_count=5", "the bearer of news", "authorization failed", "pinned the build", "shipping=express", "cookie banner shown", "mapping: done", "spin=fast", "hairpin: bent", "pallbearer attended"} {
		if got := r.FilterAttributes(map[string]string{"note": in})["note"]; got != in {
			t.Errorf("%q was changed to %q", in, got)
		}
	}
}
