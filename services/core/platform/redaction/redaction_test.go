package redaction_test

// SEC-03 (REQ-SEC-003, REQ-OBS-002, AGENT-13, POC-NFR-06): the mandatory redactor of the core. A full account number planted in a log line, a log attribute (flat, nested, an error,
// a number), a trace attribute or an API output never reaches the output: it is masked to its last four digits, and a value under a secret-looking key is removed. The threshold is
// a required parameter, never a default (AGENT-16). All data is SYNTHETIC.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/redaction"
)

const minDigits = 8 // the test's own value; the decision records carry the real one (AGENT-16)

var longRun = regexp.MustCompile(`[0-9]{8,}`)

func newRedactor(t *testing.T) *redaction.Redactor {
	t.Helper()
	r, err := redaction.New(minDigits)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

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
	fn(slog.New(newRedactor(t).Handler(slog.NewJSONHandler(&buf, dropTime))))
	return buf.String()
}

func noRun(t *testing.T, label, out string) {
	t.Helper()
	if longRun.MatchString(out) {
		t.Errorf("%s: a digit run of %d or more reached the output: %s", label, minDigits, out)
	}
}

func TestAPlantedAccountNumberInALogMessageOrAttributeIsMasked(t *testing.T) {
	const acct = "0123456789"
	out := logged(t, func(l *slog.Logger) {
		l.Info("transfer for account "+acct, "account", acct, slog.Group("detail", slog.String("beneficiary", "acct "+acct), slog.Group("deep", slog.String("n", acct))))
		l.With("account", acct).WithGroup("g").Warn("again", "n", acct)
	})
	noRun(t, "log", out)
	for _, want := range []string{"******6789"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks the masked form %q: %s", want, out)
		}
	}
	if strings.Count(out, acct) != 0 {
		t.Errorf("the full number is in the output: %s", out)
	}
}

func TestAnErrorAStringerAndANumberAreMaskedToo(t *testing.T) {
	out := logged(t, func(l *slog.Logger) {
		l.Error("failed", "err", errors.New("lookup of 0123 4567 89 failed"), "n", int64(1234567890123), "u", uint64(9876543210), "s", stringer("acct 0123456789"),
			"w", fmt.Errorf("wrapped: %w", errors.New("card 4111-1111-1111-1111")))
	})
	noRun(t, "log", out)
	for _, bad := range []string{"0123456789", "1234567890123", "9876543210", "4111111111111111", "4111-1111-1111-1111", "0123 4567 89"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q reached the output: %s", bad, out)
		}
	}
}

type stringer string

func (s stringer) String() string { return string(s) }

func TestUnicodeDigitsAreMaskedLikeAsciiDigits(t *testing.T) {
	// full-width, Arabic-Indic and Devanagari digits: a bypass if only ASCII digits count (the model service's redactor counts them all)
	for name, num := range map[string]string{"full-width": "０１２３４５６７８９", "arabic-indic": "٠١٢٣٤٥٦٧٨٩", "devanagari": "०१२३४५६७८९"} {
		got := newRedactor(t).MaskAccountNumbers("acct " + num)
		if strings.Contains(got, num) || !strings.HasPrefix(got, "acct ******") {
			t.Errorf("%s: got %q, want the number masked to its last four digits", name, got)
		}
		if !strings.HasSuffix(got, string([]rune(num)[len([]rune(num))-4:])) {
			t.Errorf("%s: the last four digits are not kept: %q", name, got)
		}
	}
}

func TestRunsWithSingleSpacesOrHyphensCountAndAmountsAndShortRunsDoNot(t *testing.T) {
	r := newRedactor(t)
	cases := map[string]string{
		"0123-4567-89": "******6789",
		"0123 4567 89": "******6789",
		"20,000,000":   "20,000,000", // a comma ends a run: an amount is not an account number
		"12345":        "12345",
		"1234567":      "1234567", // one short of the threshold
		"12345678":     "****5678",
		"x12345678y":   "x****5678y",
	}
	for in, want := range cases {
		if got := r.MaskAccountNumbers(in); got != want {
			t.Errorf("MaskAccountNumbers(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAValueUnderASecretKeyIsRemovedWhatEverItsShape(t *testing.T) {
	out := logged(t, func(l *slog.Logger) {
		l.Info("auth", "token", "abc", "Authorization", "Bearer xyz", "api_key", "k", "db_password", "p", slog.Group("creds", slog.String("client_secret", "s"), slog.String("fine", "ok")),
			"Authorization-Header", 12345)
	})
	for _, leaked := range []string{`"abc"`, "Bearer xyz", `"k"`, `"p"`, `"s"`} {
		if strings.Contains(out, leaked) {
			t.Errorf("a secret value %s reached the output: %s", leaked, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") || !strings.Contains(out, `"fine":"ok"`) {
		t.Errorf("the output should carry [REDACTED] and keep non-secret fields: %s", out)
	}
}

func TestTheHandlerKeepsLevelContextAndGroupsOfTheInnerHandler(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := newRedactor(t).Handler(inner)
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("the wrapper enabled a level the inner handler does not")
	}
	l := slog.New(h).WithGroup("g").With("k", "v")
	l.Warn("m", "a", "b")
	if got := buf.String(); !strings.Contains(got, `"g":{"k":"v","a":"b"}`) {
		t.Errorf("groups and attributes were not kept: %s", got)
	}
}

func TestATraceAttributeIsFilteredLikeALogAttribute(t *testing.T) {
	got := newRedactor(t).FilterAttributes(map[string]string{"account": "0123456789", "note": "for 0123-4567-89", "token": "t0p", "ok": "fine"})
	if got["account"] != "******6789" || got["note"] != "for ******6789" || got["token"] != redaction.Redacted || got["ok"] != "fine" {
		t.Errorf("filtered attributes: %v", got)
	}
}

func TestTheThresholdIsRequiredAndMustExceedTheDigitsKept(t *testing.T) {
	for _, n := range []int{-1, 0, 1, 4} {
		if _, err := redaction.New(n); err == nil {
			t.Errorf("New(%d) accepted a threshold that would mask nothing or everything", n)
		}
	}
	if _, err := redaction.New(5); err != nil {
		t.Errorf("New(5): %v", err)
	}
}

func TestAHugeRunAndAHugeInputAreHandled(t *testing.T) {
	r := newRedactor(t)
	big := strings.Repeat("9", 100000)
	got := r.MaskAccountNumbers(big)
	if len(got) != len(big) || !strings.HasSuffix(got, "9999") || strings.Contains(got, "99999") {
		t.Errorf("a 100000-digit run was not masked to its last four digits")
	}
	if got := r.MaskAccountNumbers(strings.Repeat("a1 ", 50000)); got == "" {
		t.Error("empty result for a long input")
	}
}
