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
	l, err := trustlimits.Load(strings.NewReader(text))
	if err == nil {
		t.Errorf("%s was accepted", name)
		return
	}
	var le *trustlimits.Error
	if !errors.As(err, &le) {
		t.Errorf("%s: error %T is not a *trustlimits.Error", name, err)
	}
	if l.Loaded() || l.AmbiguityAbsoluteFloorBP() != 0 || l.ClockAtRiskRemainingBP() != 0 {
		t.Errorf("%s: an error came with a value", name)
	}
}

// The shipped pack file: the floor 0.70 (PERD line 1772) and the AT_RISK threshold 25 percent (PERD line 2134) as integer basis points.
func TestThePacksFileLoadsToSevenThousandAndTwentyFiveHundred(t *testing.T) {
	f, err := os.Open(packFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := trustlimits.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Loaded() || l.AmbiguityAbsoluteFloorBP() != 7000 || l.ClockAtRiskRemainingBP() != 2500 {
		t.Fatalf("floor %d, at-risk %d, loaded %v; want 7000, 2500, true", l.AmbiguityAbsoluteFloorBP(), l.ClockAtRiskRemainingBP(), l.Loaded())
	}
}

// A value is never a constant of the loader: other documents give other values.
func TestTheValuesComeFromTheDocument(t *testing.T) {
	for _, c := range [][2]int{{6500, 3000}, {0, 0}, {10000, 10000}, {1, 9999}} {
		l, err := trustlimits.Load(strings.NewReader(doc(strconv.Itoa(c[0]), strconv.Itoa(c[1]))))
		if err != nil || l.AmbiguityAbsoluteFloorBP() != c[0] || l.ClockAtRiskRemainingBP() != c[1] {
			t.Errorf("%v: got %d, %d, %v", c, l.AmbiguityAbsoluteFloorBP(), l.ClockAtRiskRemainingBP(), err)
		}
	}
}

// Basis points run 0 to 10000 (BasisPoints, A166): nothing outside is a limit.
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

// No default: a missing member is refused, not read as 0 (0 is a legal value).
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
	if l, err := trustlimits.Load(strings.NewReader(d + "\n")); err != nil || !l.Loaded() {
		t.Errorf("a trailing newline is not trailing content: %v", err)
	}
}

func TestTheZeroLimitsIsNotLoaded(t *testing.T) {
	var l trustlimits.Limits
	if l.Loaded() {
		t.Fatal("the zero Limits reports it is loaded")
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
		if _, err := trustlimits.Load(strings.NewReader(string(b))); err != nil {
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
