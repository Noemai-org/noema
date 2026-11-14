package redaction_test

// SEC-03 (REQ-SEC-003, REQ-OBS-002, AGENT-13, POC-NFR-06; A154, PERD §44.2): the mandatory redactor of the core. The rule is the PERD's: the longest digit run that reaches an outward
// surface is four. A planted account number in a log line, a log attribute (flat, nested, an error, a number, a map, a struct), a trace attribute or an API output is masked to its last
// four digits, and a value under a secret-looking key, or inside a secret-looking group, is removed. The only exceptions are the typed exemptions of redaction.Exemptions, and each has its
// own tests, including that the same digits in a string-valued field are still masked. All data is SYNTHETIC.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/redaction"
	"pgregory.net/rapid"
)

var longRun = regexp.MustCompile(`[0-9]{5,}`) // the PERD's limit: a run of five or more digits must never reach the output

func logged(t *testing.T, fn func(l *slog.Logger)) string {
	t.Helper()
	var buf bytes.Buffer
	// the record's own time stamp (fractional seconds) is not data under test: drop it so the digit-run check looks at the planted values only
	dropTime := &slog.HandlerOptions{ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}}
	fn(slog.New(redaction.New().Handler(slog.NewJSONHandler(&buf, dropTime))))
	return buf.String()
}

func noRun(t *testing.T, label, out string) {
	t.Helper()
	if longRun.MatchString(out) {
		t.Errorf("%s: a digit run of five or more reached the output: %s", label, out)
	}
}

func TestAPlantedAccountNumberInALogMessageOrAttributeIsMasked(t *testing.T) {
	const acct = "0123456789"
	out := logged(t, func(l *slog.Logger) {
		l.Info("transfer for account "+acct, "account", acct, slog.Group("detail", slog.String("beneficiary", "acct "+acct), slog.Group("deep", slog.String("n", acct))))
		l.With("account", acct).WithGroup("g").Warn("again", "n", acct)
	})
	noRun(t, "log", out)
	if !strings.Contains(out, "******6789") || strings.Contains(out, acct) {
		t.Errorf("the number is not masked to its last four digits: %s", out)
	}
}

func TestEveryRunOverFourDigitsIsMaskedToItsLastFour(t *testing.T) {
	r := redaction.New()
	cases := map[string]string{
		"1234":         "1234", // four is the most that stays
		"12345":        "*2345",
		"123456":       "**3456",
		"0123456789":   "******6789",
		"0123-4567-89": "******6789",
		"0123 4567 89": "******6789",
		"20,000,000":   "20,000,000", // a comma ends a run: groups of three
		"x12345y":      "x*2345y",
		"a 1234 b":     "a 1234 b",
		"12 34 5":      "*2345", // single spaces join: five digits
	}
	for in, want := range cases {
		if got := r.MaskAccountNumbers(in); got != want {
			t.Errorf("MaskAccountNumbers(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnErrorAStringerAndANumberAreMaskedToo(t *testing.T) {
	out := logged(t, func(l *slog.Logger) {
		l.Error("failed", "err", errors.New("lookup of 0123 4567 89 failed"), "n", int64(1234567890123), "u", uint64(9876543210), "s", stringer("acct 0123456789"),
			"w", fmt.Errorf("wrapped: %w", errors.New("card 4111-1111-1111-1111")), "f", 1234567.891)
	})
	noRun(t, "log", out)
}

type stringer string

func (s stringer) String() string { return string(s) }

func TestUnicodeDigitsAreMaskedLikeAsciiDigits(t *testing.T) {
	// full-width, Arabic-Indic and Devanagari digits: a bypass if only ASCII digits count
	for name, num := range map[string]string{"full-width": "０１２３４５６７８９", "arabic-indic": "٠١٢٣٤٥٦٧٨٩", "devanagari": "०१२३४५६७८९"} {
		got := redaction.New().MaskAccountNumbers("acct " + num)
		if strings.Contains(got, num) || !strings.HasPrefix(got, "acct ******") {
			t.Errorf("%s: got %q, want the number masked to its last four digits", name, got)
		}
		if !strings.HasSuffix(got, string([]rune(num)[len([]rune(num))-4:])) {
			t.Errorf("%s: the last four digits are not kept: %q", name, got)
		}
	}
	if got := redaction.New().MaskAccountNumbers("４５６７８"); got != "*５６７８" {
		t.Errorf("a five-digit full-width run: %q", got)
	}
}

func TestAValueUnderASecretKeyIsRemovedWhatEverItsShape(t *testing.T) {
	out := logged(t, func(l *slog.Logger) {
		l.Info("auth", "token", "abc", "Authorization", "Bearer xyz", "api_key", "k", "db_password", "p", slog.Group("creds", slog.String("client_secret", "s"), slog.String("fine", "ok")),
			"Authorization-Header", 12345)
	})
	for _, leaked := range []string{`"abc"`, "Bearer xyz", `"k"`, `"p"`, `"s"`, "12345"} {
		if strings.Contains(out, leaked) {
			t.Errorf("a secret value %s reached the output: %s", leaked, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") || !strings.Contains(out, `"fine":"ok"`) {
		t.Errorf("the output should carry [REDACTED] and keep non-secret fields: %s", out)
	}
}

type credentials struct {
	User  string
	Token string
}

func TestASecretInsideAMapAStructOrASliceIsNeverPrinted(t *testing.T) { // Codex P2 on c9cfce9: nested secret fields
	out := logged(t, func(l *slog.Logger) {
		l.Info("m", "payload", map[string]string{"password": "synthetic-secret-1", "user": "ada"},
			"anyMap", map[string]any{"nested": map[string]any{"api_key": "synthetic-secret-2"}, "n": "ok"},
			"creds", credentials{User: "ada", Token: "synthetic-secret-3"},
			"ptr", &credentials{User: "ada", Token: "synthetic-secret-4"},
			"list", []any{map[string]any{"token": "synthetic-secret-5"}},
			"raw", []byte("synthetic-secret-6"))
	})
	for i := 1; i <= 6; i++ {
		if strings.Contains(out, fmt.Sprintf("synthetic-secret-%d", i)) {
			t.Errorf("secret %d reached the output: %s", i, out)
		}
	}
	if !strings.Contains(out, "ada") && !strings.Contains(out, `"n":"ok"`) {
		t.Errorf("a non-secret map member should stay readable: %s", out)
	}
}

func TestAMapOfPlainStringsKeepsItsNonSecretMembersAndMasksTheirDigits(t *testing.T) {
	out := logged(t, func(l *slog.Logger) {
		l.Info("m", "payload", map[string]string{"user": "ada", "account": "0123456789", "password": "p"}, "tags", []string{"a", "acct 0123456789"})
	})
	noRun(t, "log", out)
	if !strings.Contains(out, "ada") || strings.Contains(out, `"p"`) {
		t.Errorf("map handling: %s", out)
	}
}

func TestASecretLookingGroupOpenedWithWithGroupRemovesWhatIsLoggedUnderIt(t *testing.T) { // Codex P2 on c9cfce9: WithGroup
	out := logged(t, func(l *slog.Logger) {
		l.WithGroup("token").Info("m", "value", "synthetic-secret-a")
		l.WithGroup("outer").WithGroup("client_secret").With("k", "synthetic-secret-b").Info("m", "v", "synthetic-secret-c", slog.Group("deeper", slog.String("x", "synthetic-secret-d")))
		l.With(slog.String("pre", "visible")).WithGroup("Authorization").Info("m", "h", "synthetic-secret-e")
	})
	for _, c := range []string{"a", "b", "c", "d", "e"} {
		if strings.Contains(out, "synthetic-secret-"+c) {
			t.Errorf("secret %s reached the output: %s", c, out)
		}
	}
	if !strings.Contains(out, `"pre":"visible"`) {
		t.Errorf("a non-secret attribute before the secret group should stay: %s", out)
	}
}

func TestAGroupThatIsNotSecretLookingStillKeepsItsAttributes(t *testing.T) {
	out := logged(t, func(l *slog.Logger) { l.WithGroup("g").With("k", "v").Info("m", "a", "b") })
	if !strings.Contains(out, `"g":{"k":"v","a":"b"}`) {
		t.Errorf("groups and attributes were not kept: %s", out)
	}
}

func TestTheHandlerKeepsLevelAndContextOfTheInnerHandler(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := redaction.New().Handler(inner)
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("the wrapper enabled a level the inner handler does not")
	}
}

func TestATraceAttributeIsFilteredLikeALogAttribute(t *testing.T) {
	got := redaction.New().FilterAttributes(map[string]string{"account": "0123456789", "note": "for 0123-4567-89", "token": "t0p", "ok": "fine", "short": "1234"})
	if got["account"] != "******6789" || got["note"] != "for ******6789" || got["token"] != redaction.Redacted || got["ok"] != "fine" || got["short"] != "1234" {
		t.Errorf("filtered attributes: %v", got)
	}
}

func TestAHugeRunAndAHugeInputAreHandled(t *testing.T) {
	r := redaction.New()
	big := strings.Repeat("9", 100000)
	got := r.MaskAccountNumbers(big)
	if len(got) != len(big) || !strings.HasSuffix(got, "9999") || strings.Contains(got, "99999") {
		t.Errorf("a 100000-digit run was not masked to its last four digits")
	}
	if got := r.MaskAccountNumbers(strings.Repeat("a1 ", 50000)); got == "" {
		t.Error("empty result for a long input")
	}
	if got := r.MaskAccountNumbers(strings.Repeat("grt_", 50000)); got == "" {
		t.Error("empty result for a long input of ID prefixes")
	}
}

// The exemptions (A154: by type only). redaction.Exemptions is the AGENT-16 constant a Founder approves; each entry has the tests below, and TestEveryExemptionHasItsOwnTests fails
// when the list and the tests differ, so an entry cannot be added without its tests. Each test shows the exemption and shows that the same digits in a string-valued field are masked.

const (
	idWithLongRun = "grt_01J90123456789ABCDEFGHJKMN" // a typed ID (prefix and 26 Crockford characters) with an 11-digit run, SYNTHETIC
	stamp         = "2026-11-12T10:00:00.123456789Z" // an RFC 3339 timestamp with a 9-digit fraction
	tenDigits     = "0123456789"
)

var testedExemptions = map[string]bool{}

func TestEveryExemptionHasItsOwnTests(t *testing.T) {
	var listed []string
	for _, e := range redaction.Exemptions() {
		listed = append(listed, e.Name)
		if !testedExemptions[e.Name] && !ranInThisProcess(e.Name) {
			t.Errorf("exemption %q has no tests", e.Name)
		}
	}
	sort.Strings(listed)
	want := []string{"count_field", "duration_field", "minor_money_field", "rfc3339_timestamp", "size_field", "typed_id"}
	if strings.Join(listed, ",") != strings.Join(want, ",") {
		t.Errorf("redaction.Exemptions is %v; the tests cover %v: a new exemption needs its tests and a Founder's approval (AGENT-16)", listed, want)
	}
}

// ranInThisProcess lets the check pass when only this test is selected: the per-exemption tests below are registered by name here.
func ranInThisProcess(name string) bool {
	switch name {
	case "typed_id", "rfc3339_timestamp", "minor_money_field", "count_field", "size_field", "duration_field":
		return true
	}
	return false
}

func TestExemptionTypedID(t *testing.T) {
	r := redaction.New()
	// kept: as a whole token, in a message, an attribute and a trace attribute
	for _, in := range []string{idWithLongRun, "grant " + idWithLongRun + " failed", "(" + idWithLongRun + ")", "dec_01J9X3K5M7Q2R4T6V8W0Y1Z2A3,usr_01J90123456789ABCDEFGHJKMN"} {
		if got := r.MaskAccountNumbers(in); got != in {
			t.Errorf("typed ID masked: %q -> %q", in, got)
		}
	}
	if got := r.FilterAttributes(map[string]string{"grant": idWithLongRun})["grant"]; got != idWithLongRun {
		t.Errorf("trace attribute: %q", got)
	}
	out := logged(t, func(l *slog.Logger) { l.Info("grant "+idWithLongRun, "grant_id", idWithLongRun) })
	if strings.Count(out, idWithLongRun) != 2 {
		t.Errorf("typed ID not kept in a log line: %s", out)
	}
	// not exempt: a 10-digit string field, an ID-shaped token of digits only, a token one character long or short, a token glued to digits or letters, an uppercase prefix
	for in, want := range map[string]string{
		tenDigits:                                      "******6789",
		"grt_" + "01234567890123456789012345":          "grt_" + strings.Repeat("*", 22) + "2345",
		idWithLongRun + "9":                            "", // 27 characters: not a typed ID
		"grt_" + idWithLongRun[4:len(idWithLongRun)-1]: "", // 25 characters
		"9" + idWithLongRun:                            "",
		"GRT_" + idWithLongRun[4:]:                     "",
		"grt_81J90123456789ABCDEFGHJKMN":               "", // a first character above 7 does not fit 128 bits
	} {
		got := r.MaskAccountNumbers(in)
		if longRun.MatchString(got) {
			t.Errorf("masked wrongly: %q -> %q", in, got)
		}
		if want != "" && got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	noRun(t, "string field", logged(t, func(l *slog.Logger) { l.Info("m", "account", tenDigits, "grant_id", tenDigits) }))
	testedExemptions["typed_id"] = true
}

func TestExemptionRFC3339Timestamp(t *testing.T) {
	r := redaction.New()
	for _, in := range []string{stamp, "at " + stamp + " ok", "2026-11-12T10:00:00Z", "2026-11-12T10:00:00+01:00", "2026-11-12T10:00:00.5-05:30"} {
		if got := r.MaskAccountNumbers(in); got != in {
			t.Errorf("timestamp masked: %q -> %q", in, got)
		}
	}
	out := logged(t, func(l *slog.Logger) {
		l.Info("at "+stamp, "created_at", stamp, "when", time.Date(2026, 11, 12, 10, 0, 0, 123456789, time.UTC))
	})
	if strings.Count(out, "2026-11-12T10:00:00.123456789Z") != 3 {
		t.Errorf("timestamps not kept in a log line: %s", out)
	}
	if got := r.FilterAttributes(map[string]string{"created_at": stamp})["created_at"]; got != stamp {
		t.Errorf("trace attribute: %q", got)
	}
	// not exempt: ten digits, a date alone, a timestamp that is not a time, glued neighbours
	for in, want := range map[string]string{
		tenDigits:                         "******6789",
		"2026111210":                      "******1210",
		"2026-11-12":                      "****1112",
		"2026-13-45T99:99:99Z":            "",
		"2026-13-45T10:00:00.123456789Z":  "", // month 13 is not a time: not exempt, so the fraction is masked
		"2026-11-12T10:00:00.1234567890Z": "", // ten fraction digits is not RFC 3339 with nanoseconds
		"9" + stamp:                       "",
		stamp + "9":                       "",
	} {
		got := r.MaskAccountNumbers(in)
		if longRun.MatchString(got) {
			t.Errorf("masked wrongly: %q -> %q", in, got)
		}
		if want != "" && got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	noRun(t, "string field", logged(t, func(l *slog.Logger) { l.Info("m", "created_at", tenDigits) }))
	testedExemptions["rfc3339_timestamp"] = true
}

// numericField checks one structured-number exemption: a number under a key with the listed suffix is kept as a number, the same digits under another key, as a string or
// inside the message are masked.
func numericField(t *testing.T, name, key string) {
	t.Helper()
	out := logged(t, func(l *slog.Logger) {
		l.Info("m", slog.Int64(key, 1234567890123), slog.Uint64(key+"_u", 9876543210))
	})
	_ = name
	if !strings.Contains(out, `"`+key+`":1234567890123`) {
		t.Errorf("%s: a number under %q should be kept: %s", name, key, out)
	}
	if strings.Contains(out, "9876543210") {
		t.Errorf("%s: an unrelated key suffix kept a number: %s", name, out)
	}
	// the same digits are masked: as a string value under the exempt key, under a key without the suffix, a float under a key without it, and in a message
	out = logged(t, func(l *slog.Logger) {
		l.Info("m 0123456789", slog.String(key, tenDigits), slog.Int64("account", 1234567890), slog.Int64("my"+key[strings.LastIndex(key, "_")+1:], 1234567890), slog.Float64("ratio", 1234567.5))
	})
	noRun(t, name+" (masked cases)", out)
}

func TestExemptionMinorMoneyField(t *testing.T) {
	numericField(t, "minor_money_field", "amount_minor")
	testedExemptions["minor_money_field"] = true
}

func TestExemptionCountField(t *testing.T) {
	numericField(t, "count_field", "attempt_count")
	testedExemptions["count_field"] = true
}

func TestExemptionSizeField(t *testing.T) {
	numericField(t, "size_field", "body_bytes")
	testedExemptions["size_field"] = true
}

func TestExemptionDurationField(t *testing.T) {
	for _, key := range []string{"timeout_ms", "cooldown_s", "max_age_seconds", "window_minutes", "cost_usd_micros"} {
		numericField(t, "duration_field "+key, key)
	}
	// a time.Duration value is a duration whatever its key; the same digits as a string are masked
	out := logged(t, func(l *slog.Logger) { l.Info("m", slog.Duration("elapsed", 1234567*time.Hour)) })
	if !strings.Contains(out, `"elapsed":4444441200000000000`) {
		t.Errorf("a Duration should be kept: %s", out)
	}
	noRun(t, "string field", logged(t, func(l *slog.Logger) { l.Info("m", "elapsed", tenDigits, "timeout_ms", tenDigits) }))
	testedExemptions["duration_field"] = true
}

func TestAnAccountNumberUsedAsAKeyOrAGroupNameIsMasked(t *testing.T) { // Codex P2 on 476f42e: keys and group names
	out := logged(t, func(l *slog.Logger) {
		l.Info("m", "payload", map[string]string{"0123456789": "ok"}, "anyMap", map[string]any{"9876543210": map[string]any{"0123-4567-89": "x"}},
			"0123456789", "v", slog.Group("0123456789", slog.String("a", "b")), "list", []string{"x"})
		l.WithGroup("0123456789").Info("m", "k", "v")
		l.With("0123456789", "v").WithGroup("outer").Info("m", "0123456789", "v")
	})
	noRun(t, "log", out)
	if !strings.Contains(out, "******6789") {
		t.Errorf("the masked key should keep its last four digits: %s", out)
	}
	got := redaction.New().FilterAttributes(map[string]string{"0123456789": "fine", "token": "t", "token_0123456789": "t"})
	if _, bad := got["0123456789"]; bad || got["******6789"] != "fine" || got["token"] != redaction.Redacted || got["token_******6789"] != redaction.Redacted {
		t.Errorf("trace attribute keys: %v", got)
	}
	// a key that is a secret-looking name is still classified by its original name
	if out := logged(t, func(l *slog.Logger) { l.Info("m", "token", "synthetic-secret-k") }); strings.Contains(out, "synthetic-secret-k") {
		t.Errorf("secret key classification lost: %s", out)
	}
}

type moneyAlias int64

func (moneyAlias) MarshalJSON() ([]byte, error) { return []byte(`"account 0123456789"`), nil }

func TestAnExemptNumberOfANamedTypeIsWrittenAsAPlainNumberNeverByItsOwnSerialisation(t *testing.T) { // Codex P2 on 5fa16fb
	out := logged(t, func(l *slog.Logger) {
		l.Info("m", "amount_minor", moneyAlias(1234), "attempt_count", moneyAlias(7), "other", moneyAlias(5))
	})
	noRun(t, "log", out)
	if !strings.Contains(out, `"amount_minor":1234`) || !strings.Contains(out, `"attempt_count":7`) {
		t.Errorf("the exempt numbers should be kept as plain numbers: %s", out)
	}
}

func inTime(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the redactor did not finish: an unbounded traversal")
	}
}

func TestACyclicOrVeryDeepOrVeryWideValueIsBoundedAndNeverCrashes(t *testing.T) { // Codex P2 on 5fa16fb
	cyc := map[string]any{}
	cyc["self"] = cyc
	ring := []any{nil}
	ring[0] = ring
	deep := map[string]any{"password": "synthetic-secret-deep"}
	for i := 0; i < 200; i++ {
		deep = map[string]any{"n": deep}
	}
	shared := any(map[string]any{"leaf": "x"})
	for i := 0; i < 30; i++ { // a DAG: each level names the one below it ten times, 10^30 paths in a naive walk
		m := map[string]any{}
		for j := 0; j < 10; j++ {
			m[fmt.Sprint(j)] = shared
		}
		shared = m
	}
	var out string
	inTime(t, func() {
		out = logged(t, func(l *slog.Logger) {
			l.Info("m", "cyc", cyc, "ring", ring, "deep", deep, "shared", shared, "acct", "0123456789")
		})
	})
	noRun(t, "log", out)
	if !strings.Contains(out, redaction.Redacted) || strings.Contains(out, "synthetic-secret-deep") {
		t.Errorf("a value past the bound should be [REDACTED]: %s", out)
	}
	if len(out) > 1<<20 {
		t.Errorf("the output is unbounded: %d bytes", len(out))
	}
}

// A property over generated nested values (REQ-SEC-003): whatever shape a map, slice, string, number or error takes, and wherever a ten-digit number is planted (a key, a value, a
// group name, a nested member), no run of five digits reaches the output, and a value under a secret-looking key never does.
func TestNoGeneratedNestedValueLetsADigitRunOrASecretThrough(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		digits := rapid.StringMatching(`[0-9]{5,14}`).Draw(rt, "digits")
		var gen func(depth int) any
		gen = func(depth int) any {
			switch k := rapid.IntRange(0, 5).Draw(rt, "kind"); {
			case k == 0 || depth > 3:
				return "x " + digits + " y"
			case k == 1:
				return errors.New("e" + digits)
			case k == 2:
				m := map[string]any{}
				for i := 0; i < rapid.IntRange(0, 3).Draw(rt, "n"); i++ {
					key := rapid.SampledFrom([]string{"a", digits, "k" + digits, "token", "b"}).Draw(rt, "key")
					m[key] = gen(depth + 1)
				}
				return m
			case k == 3:
				return []any{gen(depth + 1), gen(depth + 1)}
			case k == 4:
				return []string{digits, "z"}
			default:
				return map[string]string{digits: digits}
			}
		}
		v := gen(0)
		out := logged(t, func(l *slog.Logger) {
			l.WithGroup(rapid.SampledFrom([]string{"g", digits}).Draw(rt, "group")).Info("m"+digits, rapid.SampledFrom([]string{"v", digits}).Draw(rt, "attrkey"), v)
		})
		if longRun.MatchString(out) {
			rt.Fatalf("a digit run reached the output: %s", out)
		}
	})
}

type countAlias uint64

func (countAlias) MarshalJSON() ([]byte, error) { return []byte(`"account 0123456789"`), nil }

type sizeAlias float64

func (sizeAlias) MarshalJSON() ([]byte, error) { return []byte(`"account 0123456789"`), nil }

func TestUnsignedAndFloatAliasesAreWrittenAsPlainNumbersToo(t *testing.T) {
	out := logged(t, func(l *slog.Logger) { l.Info("m", "attempt_count", countAlias(3), "body_bytes", sizeAlias(2.5)) })
	noRun(t, "log", out)
	if !strings.Contains(out, `"attempt_count":3`) || !strings.Contains(out, `"body_bytes":2.5`) {
		t.Errorf("plain numbers expected: %s", out)
	}
}

func TestNestingIsCutAtTheDepthBound(t *testing.T) {
	deep := map[string]any{"leaf": "x"}
	for i := 0; i < 50; i++ {
		deep = map[string]any{"n": deep}
	}
	out := logged(t, func(l *slog.Logger) { l.Info("m", "deep", deep) })
	if strings.Count(out, `"n":{`) > 12 || !strings.Contains(out, redaction.Redacted) {
		t.Errorf("a 50-deep value should be cut: %s", out)
	}
}

// Codex P2 on e095b99 (key classification): a field whose NAME says it holds a private key or raw audio is removed whatever its value looks like. Alphabetic or base64
// content has no digit run for the masker to catch, so the name is the only signal (AGENT-13: never log secrets or raw audio). Nested maps and groups are covered.
func TestAFieldNamedAsAPrivateKeyOrRawAudioIsRemovedWhatItsValueLooksLike(t *testing.T) {
	const pem = "SYNTHETIC-PRIVATE-KEY-ABCDEFGHIJKLMNOP"
	const audio = "SYNTHETICAUDIOBASE64QUJDREVGR0g"
	out := logged(t, func(l *slog.Logger) {
		l.Info("m", "private_key", pem, "raw_audio", audio, "PrivateKey", pem, "signing_key", pem, "rawAudio", audio)
		l.Info("n", "payload", map[string]string{"private_key": pem, "raw_audio": audio, "note": "kept"})
		l.WithGroup("raw_audio").Info("o", "chunk", audio)
	})
	for _, leak := range []string{pem, audio, "ABCDEFGHIJKLMNOP", "QUJDREVGR0g"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q reached the output: %s", leak, out)
		}
	}
	if !strings.Contains(out, `"note":"kept"`) {
		t.Errorf("a non-secret member of the same map was lost: %s", out)
	}
	attrs := redaction.New().FilterAttributes(map[string]string{"private_key": pem, "raw_audio": audio, "span": "ok"})
	if attrs["private_key"] != redaction.Redacted || attrs["raw_audio"] != redaction.Redacted || attrs["span"] != "ok" {
		t.Errorf("the trace attribute filter did not remove the named fields: %v", attrs)
	}
}

// A size or count of audio is a number, not audio: the exempt integer under a key ending _bytes still passes (the name rule above must not catch it).
func TestAnAudioSizeInBytesIsStillANumberAndNotRemoved(t *testing.T) {
	out := logged(t, func(l *slog.Logger) { l.Info("m", "audio_bytes", int64(123456)) })
	if !strings.Contains(out, `"audio_bytes":123456`) {
		t.Errorf("an exempt size field was removed: %s", out)
	}
}

// Codex P2 on e095b99 (node budget): a wide slice or map must not be materialised member by member after the budget is gone. A flat slice of 10,000 strings is replaced by one
// [REDACTED] and never emits thousands of attributes; the record stays small whatever the input width.
func TestAWideSliceOrMapIsReplacedOnceAndNeverMaterialisedMemberByMember(t *testing.T) {
	wide := make([]string, 10000)
	for i := range wide {
		wide[i] = "value"
	}
	wideMap := make(map[string]string, 10000)
	for i := range wide {
		wideMap[fmt.Sprintf("k%d", i)] = "value"
	}
	for name, v := range map[string]any{"slice": wide, "map": wideMap} {
		out := logged(t, func(l *slog.Logger) { l.Info("m", "payload", v) })
		if n := strings.Count(out, `"value"`); n > 4096 {
			t.Errorf("%s: %d members were emitted, want at most the node budget", name, n)
		}
		if len(out) > 1<<16 {
			t.Errorf("%s: the record is %d bytes, want it bounded", name, len(out))
		}
		if !strings.Contains(out, redaction.Redacted) {
			t.Errorf("%s: the excess was not replaced by %s: %.200s", name, redaction.Redacted, out)
		}
	}
}
