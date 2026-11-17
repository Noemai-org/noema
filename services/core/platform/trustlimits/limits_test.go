package trustlimits_test

// TRU-06 / REQ-AMB-003 (the ambiguity absolute floor, PERD line 1772), REQ-TRU-006 (the clock phase AT_RISK, PERD line 2134), AGENT-16: the Trust steps read two provisional
// limits as pack data (trust-limits.v1): the ambiguity absolute floor and the clock AT_RISK threshold, both integer basis points. There is no default: a document that is not
// exactly the contract is an error and the returned Limits is the zero value. Ruling A205 Q1. SYNTHETIC data.

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/trustlimits"
)

const (
	packFile    = "../../../../packs/fintech/limits/trust_limits.json"
	fixtureRoot = "../../../../contracts/fixtures/"
)

func doc(floor, atRisk string) string {
	return `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","ambiguity_absolute_floor_bp":` + floor + `,"clock_at_risk_remaining_bp":` + atRisk + `}`
}

func mustRefuse(t *testing.T, name, text string) {
	t.Helper()
	l, err := trustlimits.Load(strings.NewReader(text), "fin")
	if err == nil {
		t.Errorf("%s was accepted", name)
		return
	}
	var le *trustlimits.Error
	if !errors.As(err, &le) {
		t.Errorf("%s: error %T is not a *trustlimits.Error", name, err)
	}
	if l.Loaded() {
		t.Errorf("%s: an error came with a loaded value", name)
	}
	if v, err := l.AmbiguityAbsoluteFloorBP(); err == nil || v != 0 {
		t.Errorf("%s: the floor of a refused document is %d, %v; want 0 and an error", name, v, err)
	}
	if v, err := l.ClockAtRiskRemainingBP(); err == nil || v != 0 {
		t.Errorf("%s: the AT_RISK limit of a refused document is %d, %v; want 0 and an error", name, v, err)
	}
}

// The shipped pack file: the floor 0.70 (PERD line 1772) and the AT_RISK threshold 25 percent (PERD line 2134) as integer basis points.
func TestThePacksFileLoadsToSevenThousandAndTwentyFiveHundred(t *testing.T) {
	f, err := os.Open(packFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := trustlimits.Load(f, "fin")
	if err != nil {
		t.Fatal(err)
	}
	floor, ferr := l.AmbiguityAbsoluteFloorBP()
	atRisk, aerr := l.ClockAtRiskRemainingBP()
	if !l.Loaded() || ferr != nil || aerr != nil || floor != 7000 || atRisk != 2500 {
		t.Fatalf("floor %d (%v), at-risk %d (%v), loaded %v; want 7000, 2500, true", floor, ferr, atRisk, aerr, l.Loaded())
	}
}

// A value is never a constant of the loader: other documents give other values.
func TestTheValuesComeFromTheDocument(t *testing.T) {
	for _, c := range [][2]int{{7000, 1000}, {7500, 3000}, {10000, 5000}} {
		l, err := trustlimits.Load(strings.NewReader(doc(strconv.Itoa(c[0]), strconv.Itoa(c[1]))), "fin")
		floor, _ := l.AmbiguityAbsoluteFloorBP()
		atRisk, _ := l.ClockAtRiskRemainingBP()
		if err != nil || floor != c[0] || atRisk != c[1] {
			t.Errorf("%v: got %d, %d, %v", c, floor, atRisk, err)
		}
	}
}

// Basis points run 0 to 10000 (BasisPoints, A166): nothing outside is a limit, and 0 is not one either (see TestZeroSwitchesNeitherLimitOff).
func TestAValueOutsideZeroToTenThousandIsRefused(t *testing.T) {
	for _, v := range []string{"10001", "-1", "100000", "9223372036854775807", "99999999999999999999"} {
		mustRefuse(t, "floor "+v, doc(v, "2500"))
		mustRefuse(t, "at-risk "+v, doc("7000", v))
	}
}

// Integers only (A166): a fraction, a string, a boolean and null are refused, never rounded.
func TestAValueThatIsNotAWholeNumberIsRefused(t *testing.T) {
	for _, v := range []string{"7000.5", "0.70", "7000.0", "7e3", `"7000"`, "true", "null", "[]", "{}"} {
		mustRefuse(t, "floor "+v, doc(v, "2500"))
		mustRefuse(t, "at-risk "+v, doc("7000", v))
	}
}

// No default: a missing member is refused, not read as 0 (0 is not a legal value either).
func TestAMissingMemberIsRefusedAndNeverDefaulted(t *testing.T) {
	head := `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC"`
	mustRefuse(t, "no floor", head+`,"clock_at_risk_remaining_bp":2500}`)
	mustRefuse(t, "no at-risk", head+`,"ambiguity_absolute_floor_bp":7000}`)
	mustRefuse(t, "neither", head+`}`)
	mustRefuse(t, "no schema_version", `{"pack_id":"fin","label":"SYNTHETIC","ambiguity_absolute_floor_bp":7000,"clock_at_risk_remaining_bp":2500}`)
	mustRefuse(t, "no pack_id", `{"schema_version":"1.0","label":"SYNTHETIC","ambiguity_absolute_floor_bp":7000,"clock_at_risk_remaining_bp":2500}`)
	mustRefuse(t, "no label", `{"schema_version":"1.0","pack_id":"fin","ambiguity_absolute_floor_bp":7000,"clock_at_risk_remaining_bp":2500}`)
}

// The contract is closed: another member (the auto-resolve margin and the candidate cap live in the pack's intent nodes, not here) is refused.
func TestAnUnknownMemberIsRefused(t *testing.T) {
	mustRefuse(t, "margin", strings.TrimSuffix(doc("7000", "2500"), "}")+`,"auto_resolve_margin_bp":3500}`)
	mustRefuse(t, "case", strings.TrimSuffix(doc("7000", "2500"), "}")+`,"Ambiguity_Absolute_Floor_Bp":7000}`)
}

func TestAMemberWrittenTwiceIsRefused(t *testing.T) {
	mustRefuse(t, "floor twice", strings.TrimSuffix(doc("7000", "2500"), "}")+`,"ambiguity_absolute_floor_bp":0}`)
	mustRefuse(t, "at-risk twice", strings.TrimSuffix(doc("7000", "2500"), "}")+`,"clock_at_risk_remaining_bp":10000}`)
}

func TestTheEnvelopeIsChecked(t *testing.T) {
	for name, text := range map[string]string{
		"schema_version 2.0": `{"schema_version":"2.0","pack_id":"fin","label":"SYNTHETIC","ambiguity_absolute_floor_bp":7000,"clock_at_risk_remaining_bp":2500}`,
		"pack_id upper":      `{"schema_version":"1.0","pack_id":"Fin","label":"SYNTHETIC","ambiguity_absolute_floor_bp":7000,"clock_at_risk_remaining_bp":2500}`,
		"pack_id newline":    `{"schema_version":"1.0","pack_id":"fin\n","label":"SYNTHETIC","ambiguity_absolute_floor_bp":7000,"clock_at_risk_remaining_bp":2500}`,
		"pack_id empty":      `{"schema_version":"1.0","pack_id":"","label":"SYNTHETIC","ambiguity_absolute_floor_bp":7000,"clock_at_risk_remaining_bp":2500}`,
		"label":              `{"schema_version":"1.0","pack_id":"fin","label":"PROVISIONAL","ambiguity_absolute_floor_bp":7000,"clock_at_risk_remaining_bp":2500}`,
	} {
		mustRefuse(t, name, text)
	}
}

func TestNotExactlyOneObjectIsRefused(t *testing.T) {
	d := doc("7000", "2500")
	for name, text := range map[string]string{"empty": "", "blank": "  ", "null": "null", "array": "[" + d + "]", "two documents": d + d, "trailing junk": d + " x", "truncated": d[:len(d)-1]} {
		mustRefuse(t, name, text)
	}
	if l, err := trustlimits.Load(strings.NewReader(d+"\n"), "fin"); err != nil || !l.Loaded() {
		t.Errorf("a trailing newline is not trailing content: %v", err)
	}
}

// Security Reviewer on noema#574, blocking 2: the zero value of Limits must not be the most permissive limit. Its accessors refuse, so a Trust step that forgets to
// ask Loaded() cannot read 0 as a floor or a threshold (AGENT-16, REQ-AMB-003, REQ-TRU-006).
func TestTheZeroLimitsIsNotLoadedAndItsAccessorsRefuse(t *testing.T) {
	var l trustlimits.Limits
	if l.Loaded() {
		t.Fatal("the zero Limits reports it is loaded")
	}
	if v, err := l.AmbiguityAbsoluteFloorBP(); !errors.Is(err, trustlimits.ErrNotLoaded) || v != 0 {
		t.Errorf("floor of the zero Limits: %d, %v; want 0 and ErrNotLoaded", v, err)
	}
	if v, err := l.ClockAtRiskRemainingBP(); !errors.Is(err, trustlimits.ErrNotLoaded) || v != 0 {
		t.Errorf("AT_RISK limit of the zero Limits: %d, %v; want 0 and ErrNotLoaded", v, err)
	}
}

// A233 (interim): the ambiguity floor is at least 7000 (a pack may only tighten it) and the AT_RISK share is between 1000 and 5000 basis points. One below, one above
// and the edges are tested for each; both limits stay provisional until GOV-06 adopts them.
func TestTheBoundsOfA233AreEnforced(t *testing.T) {
	for _, v := range []string{"1", "6999"} {
		mustRefuse(t, "floor "+v, doc(v, "2500"))
	}
	for _, v := range []string{"1", "999", "5001", "10000"} {
		mustRefuse(t, "at-risk "+v, doc("7000", v))
	}
	for _, c := range [][2]string{{"7000", "1000"}, {"7000", "5000"}, {"10000", "2500"}} {
		if _, err := trustlimits.Load(strings.NewReader(doc(c[0], c[1])), "fin"); err != nil {
			t.Errorf("%v is inside the bounds and was refused: %v", c, err)
		}
	}
}

// Security Reviewer on noema#574, blocking 1: a pack may make a limit stricter than the ruling's value but not switch it off. A floor of 0 passes every candidate and an
// AT_RISK threshold of 0 means a clock is never AT_RISK, so 0 is refused for each member, by the loader and by the schema (the same two invalid fixtures).
func TestZeroSwitchesNeitherLimitOff(t *testing.T) {
	mustRefuse(t, "floor 0", doc("0", "2500"))
	mustRefuse(t, "at-risk 0", doc("7000", "0"))
	mustRefuse(t, "both 0", doc("0", "0"))
}

// The pack's identity is pinned (Security Reviewer, note 4): a document for another pack, and a call that names no pack, are refused.
func TestThePackIDIsPinned(t *testing.T) {
	if _, err := trustlimits.Load(strings.NewReader(doc("7000", "2500")), "other"); err == nil {
		t.Error("a document of pack fin was accepted for pack other")
	}
	if _, err := trustlimits.Load(strings.NewReader(doc("7000", "2500")), ""); err == nil {
		t.Error("a call naming no pack was accepted")
	}
}

// Provisional is data, not prose (Security Reviewer, note 3): the label the document carries is exposed, and a consumer or a report can say the values are SYNTHETIC.
func TestTheLabelIsExposed(t *testing.T) {
	l, err := trustlimits.Load(strings.NewReader(doc("7000", "2500")), "fin")
	if err != nil || l.Label() != "SYNTHETIC" {
		t.Errorf("label %q, %v; want SYNTHETIC", l.Label(), err)
	}
	v := strings.Replace(doc("7000", "2500"), "SYNTHETIC", "VALIDATED", 1)
	if l, err := trustlimits.Load(strings.NewReader(v), "fin"); err != nil || l.Label() != "VALIDATED" {
		t.Errorf("label %q, %v; want VALIDATED", l.Label(), err)
	}
	var zero trustlimits.Limits
	if zero.Label() != "" {
		t.Errorf("the zero Limits has label %q", zero.Label())
	}
}

// Security Reviewer, note 6: the text of an error carries no part of the document (a decode cause quotes the bad value); the cause stays reachable with errors.Is and As.
func TestTheErrorTextCarriesNoPartOfTheDocument(t *testing.T) {
	_, err := trustlimits.Load(strings.NewReader(strings.Replace(doc("7000", "2500"), "SYNTHETIC", "QUOTEDVALUE", 1)), "fin")
	if err == nil {
		t.Fatal("a label that is not allowed was accepted")
	}
	if strings.Contains(err.Error(), "QUOTEDVALUE") {
		t.Errorf("the error text quotes the document: %q", err.Error())
	}
	var le *trustlimits.Error
	if !errors.As(err, &le) || le.Err == nil {
		t.Errorf("the decode cause is not kept for errors.As: %v", err)
	}
}

// The loader refuses every invalid fixture of the contract and accepts every valid one: the same documents the schema validator is tested with (AGENT-11, REQ-TST-003).
func TestTheLoaderAgreesWithTheContractFixtures(t *testing.T) {
	valid, _ := filepath.Glob(fixtureRoot + "valid/trust-limits/*.json")
	invalid, _ := filepath.Glob(fixtureRoot + "invalid/trust-limits/*.json")
	if len(valid) == 0 || len(invalid) < 10 {
		t.Fatalf("fixtures not found: %d valid, %d invalid", len(valid), len(invalid))
	}
	for _, p := range valid {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := trustlimits.Load(strings.NewReader(string(b)), "fin"); err != nil {
			t.Errorf("%s: %v", filepath.Base(p), err)
		}
	}
	for _, p := range invalid {
		if filepath.Base(p) == "expectations.json" {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		mustRefuse(t, filepath.Base(p), string(b))
	}
}
