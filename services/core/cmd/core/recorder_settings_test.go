package main

// STM-01, ruling A475 (interim, relayed, recorded in docs#216), A330, AGENT-16: the grant lifetime, the record lifetime and the Record transaction timeout are three required
// settings, in whole milliseconds, with NO default in code. A missing or invalid one is refused and the error names that setting only, never its value. The values used here are
// SYNTHETIC test numbers; they are not the product's lifetimes. REQ-TRU-008, REQ-TRU-009; INV-03.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var recorderSettingNames = []string{EnvTrustGrantLifetimeMS, EnvTrustRecordLifetimeMS, EnvTrustTxTimeoutMS}

func recorderSettingsEnv(over map[string]string) Env {
	m := envMap{EnvTrustGrantLifetimeMS: "7000", EnvTrustRecordLifetimeMS: "8000", EnvTrustTxTimeoutMS: "900"} // SYNTHETIC test numbers
	for k, v := range over {
		if v == "<unset>" {
			delete(m, k)
			continue
		}
		m[k] = v
	}
	return Env{Getenv: m.get}
}

func TestRecorderSettingsNamesFollowA330(t *testing.T) { // A330
	re := regexp.MustCompile(`\ANOEMA_TRUST_[A-Z_]+_MS\z`)
	for _, n := range recorderSettingNames {
		if !re.MatchString(n) {
			t.Errorf("%q is not NOEMA_<AREA>_<WHAT>_MS", n)
		}
	}
}

func TestRecorderSettingsReadsThreeSettings(t *testing.T) {
	got, err := loadRecorderSettings(recorderSettingsEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := RecorderSettings{GrantLifetime: 7 * time.Second, RecordLifetime: 8 * time.Second, TxTimeout: 900 * time.Millisecond}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// AGENT-16: with no setting at all there is nothing to fall back on, and nothing is returned.
func TestRecorderSettingsHaveNoDefault(t *testing.T) {
	got, err := loadRecorderSettings(Env{Getenv: envMap{}.get})
	if err == nil {
		t.Fatalf("no settings were accepted: %+v", got)
	}
	if got != (RecorderSettings{}) {
		t.Fatalf("an error came with values: %+v", got)
	}
}

// Each setting alone missing is refused, and the error names that one only.
func TestRecorderSettingsMissingNamesOnlyThatSetting(t *testing.T) {
	for _, name := range recorderSettingNames {
		_, err := loadRecorderSettings(recorderSettingsEnv(map[string]string{name: "<unset>"}))
		if err == nil {
			t.Errorf("%s missing: accepted", name)
			continue
		}
		assertNamesOnly(t, err.Error(), name, "")
	}
}

func TestRecorderSettingsRefuseInvalidValues(t *testing.T) {
	bad := []string{"", "0", "-5", "+5", "1.5", " 5", "5 ", "05", "abc", "5s", "1e3", "0x10", "٣", "9223372036854775807", "99999999999999999999999999"}
	for _, name := range recorderSettingNames {
		for _, v := range bad {
			got, err := loadRecorderSettings(recorderSettingsEnv(map[string]string{name: v}))
			if err == nil {
				t.Errorf("%s=%q accepted: %+v", name, v, got)
				continue
			}
			assertNamesOnly(t, err.Error(), name, v)
			if got != (RecorderSettings{}) {
				t.Errorf("%s=%q: an error came with values: %+v", name, v, got)
			}
		}
	}
}

// The error names the setting and no other, and never echoes the value (AGENT-13).
func assertNamesOnly(t *testing.T, msg, name, value string) {
	t.Helper()
	if !strings.Contains(msg, name) {
		t.Errorf("the error does not name %s: %q", name, msg)
	}
	for _, other := range recorderSettingNames {
		if other != name && strings.Contains(msg, other) {
			t.Errorf("the error for %s also names %s: %q", name, other, msg)
		}
	}
	if len(value) > 2 && strings.Contains(msg, value) {
		t.Errorf("the error echoes the value %q: %q", value, msg)
	}
}

// The first setting in the order grant, record, transaction is the one named when several are bad.
func TestRecorderSettingsNameTheFirstBadOne(t *testing.T) {
	_, err := loadRecorderSettings(recorderSettingsEnv(map[string]string{EnvTrustRecordLifetimeMS: "x", EnvTrustTxTimeoutMS: "y"}))
	if err == nil {
		t.Fatal("accepted")
	}
	assertNamesOnly(t, err.Error(), EnvTrustRecordLifetimeMS, "")
}

// AGENT-16, ruling A475: the code holds no lifetime. The bench-only numbers of the ruling and the PERD's grant number are written in the PR and the status file, never in this
// loader. This reads the loader's own source, comments removed, and finds no literal that could be a default.
func TestRecorderSettingsSourceHoldsNoDefault(t *testing.T) {
	src, err := os.ReadFile("recorder_settings.go")
	if err != nil {
		t.Fatal(err)
	}
	code := regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(string(src), "")
	for _, lit := range []string{"60000", "10000", "600000", "60 *", "10 *", "600 *", "time.Minute", "time.Hour"} {
		if strings.Contains(code, lit) {
			t.Errorf("recorder_settings.go carries the literal %q", lit)
		}
	}
	if strings.Count(code, "time.Second") > 0 {
		t.Error("recorder_settings.go builds a duration in seconds: the settings are in milliseconds and no default")
	}
}

// The evidence guard. No evidence page of the repository states a grant lifetime as the product's: a line that gives a grant lifetime, TTL or expiry as a number of seconds,
// minutes or milliseconds must carry its source (the adopted PERD value: PERD, OD-P-31 or DEC-2026-10-17) or say it is bench-only. It covers the text files (.md, .py, .json, .yaml,
// .yml, .txt) of every top-level tree of the repository except services/ (code, covered by the loader test above), ui/ and .git; that takes in the benchmark tree and docs/ without
// naming the first, because the core's lint (REQ-TST-006) bans that word from any core string. It does not cover a number given without the words grant and lifetime, TTL or
// expiry on its line.
var (
	grantLifetimeClaim = regexp.MustCompile(`(?i)grants?\W{1,3}(?:\w+\W{1,3}){0,2}?(?:lifetime|ttl|expir\w*)\W.{0,40}?\b\d+\s*(?:ms|milliseconds?|s|secs?|seconds?|mins?|minutes?)\b`)
	grantClaimSource   = regexp.MustCompile(`(?i)PERD|OD-P-31|DEC-2026-10-17|bench-only`)
)

// unsourcedGrantLifetimeClaims returns the lines that state a grant lifetime with no source.
func unsourcedGrantLifetimeClaims(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if grantLifetimeClaim.MatchString(line) && !grantClaimSource.MatchString(line) {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

func TestGrantLifetimeClaimMatcher(t *testing.T) { // the matcher itself, on SYNTHETIC lines
	flagged := []string{
		"The grant lifetime is 60 s.",
		"grant TTL: 10 minutes",
		"Decision grants expire after 5 min",
		"grant expiry 600000 ms",
	}
	for _, l := range flagged {
		if len(unsourcedGrantLifetimeClaims(l)) != 1 {
			t.Errorf("not flagged: %q", l)
		}
	}
	passed := []string{
		"The grant lifetime the POC adopted: 10 minutes (PERD, OD-P-31).",
		"grant TTL 60 s, bench-only, Founder to confirm",
		"A grant is single use.",
		"The record lifetime is 60 s.",
		"GRANT_TTL_SECONDS = 600",
	}
	for _, l := range passed {
		if len(unsourcedGrantLifetimeClaims(l)) != 0 {
			t.Errorf("flagged: %q", l)
		}
	}
}

func TestNoEvidencePageStatesAGrantLifetimeAsTheProducts(t *testing.T) { // ruling A475, AGENT-16, AGENT-18
	root := filepath.Join("..", "..", "..", "..")
	var files int
	tops, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var trees []string
	for _, e := range tops {
		if e.IsDir() && e.Name() != "services" && e.Name() != "ui" && !strings.HasPrefix(e.Name(), ".") {
			trees = append(trees, e.Name())
		}
	}
	if len(trees) < 2 {
		t.Fatalf("the evidence trees were not found under %s: %v", root, trees)
	}
	for _, tree := range trees {
		base := filepath.Join(root, tree)
		err = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "__pycache__" {
					return fs.SkipDir
				}
				return nil
			}
			switch filepath.Ext(p) {
			case ".md", ".py", ".json", ".yaml", ".yml", ".txt":
			default:
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			files++
			for _, l := range unsourcedGrantLifetimeClaims(string(b)) {
				t.Errorf("%s states a grant lifetime with no source: %q", p, l)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if files < 10 {
		t.Fatalf("the guard read only %d files", files)
	}
}
