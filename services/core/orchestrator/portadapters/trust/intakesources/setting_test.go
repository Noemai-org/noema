package intakesources_test

// Proves AGENT-16 and A345 (relayed, not committed): the at-risk fraction of a clock is configuration, in whole basis points, with no default; the bounds are those of ruling A233
// that platform/ceilings holds (1000 to 5000), and the parse is strict. The error names the setting and never echoes the value. The fraction feeds the Clocks adapter as it is.

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/platform/ceilings"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
)

func settingOf(raw string) func(string) string {
	return func(name string) string {
		if name == intakesources.EnvAtRiskBPS {
			return raw
		}
		return "set-for-no-other-name"
	}
}

func TestAtRiskIsReadFromTheNamedSettingAsBasisPointsOverTenThousand(t *testing.T) {
	if intakesources.EnvAtRiskBPS != "NOEMA_TRUST_AT_RISK_BPS" {
		t.Fatalf("the setting is named %q", intakesources.EnvAtRiskBPS)
	}
	for _, bps := range []int64{ceilings.MinClockAtRiskRemainingBP, 2500, 4999, ceilings.MaxClockAtRiskRemainingBP} {
		got, err := intakesources.AtRiskFromEnv(settingOf(strconv.FormatInt(bps, 10)))
		if err != nil || got != (clocks.Ratio{Num: bps, Den: 10000}) {
			t.Errorf("%d: got %+v, %v", bps, got, err)
		}
	}
}

func TestAtRiskRefusesEverythingBeyondTheBoundsOfA233AndEveryLooseForm(t *testing.T) {
	for _, raw := range []string{
		"", " ", "999", "0", "5001", "10000", "1", "01000", "002500", "+2500", "-2500", " 2500", "2500 ", "2500\n", "\t2500", "25 00", "2500.0", "2.5e3", "0x9c4", "2,500",
		"٢٥٠٠", "２５００", "2500a", "abc", "9223372036854775808", "99999999999999999999999999", "-9223372036854775809", "NaN", "null",
	} {
		got, err := intakesources.AtRiskFromEnv(settingOf(raw))
		if !errors.Is(err, intakesources.ErrSetting) || got != (clocks.Ratio{}) {
			t.Errorf("%q: got %+v, %v; want ErrSetting and no fraction", raw, got, err)
			continue
		}
		if !strings.Contains(err.Error(), intakesources.EnvAtRiskBPS) {
			t.Errorf("%q: the error does not name the setting: %q", raw, err.Error())
		}
	}
}

func TestAtRiskTheErrorNeverEchoesTheValue(t *testing.T) {
	for _, raw := range []string{"7777", "abc7777z", "-7777"} {
		if _, err := intakesources.AtRiskFromEnv(settingOf(raw)); err == nil || strings.Contains(err.Error(), "7777") {
			t.Errorf("%q: the error must exist and must not echo the value: %v", raw, err)
		}
	}
}

func TestAtRiskHasNoDefaultAndNeedsALookup(t *testing.T) {
	if got, err := intakesources.AtRiskFromEnv(func(string) string { return "" }); !errors.Is(err, intakesources.ErrSetting) || got != (clocks.Ratio{}) {
		t.Errorf("an unset setting has no default: %+v, %v", got, err)
	}
	if got, err := intakesources.AtRiskFromEnv(nil); !errors.Is(err, intakesources.ErrSetting) || got != (clocks.Ratio{}) {
		t.Errorf("no lookup: %+v, %v", got, err)
	}
}

func TestAtRiskTheErrorNamesTheBoundsOfTheRuling(t *testing.T) {
	_, err := intakesources.AtRiskFromEnv(settingOf("5001"))
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(ceilings.MinClockAtRiskRemainingBP)) || !strings.Contains(err.Error(), strconv.Itoa(ceilings.MaxClockAtRiskRemainingBP)) {
		t.Errorf("the error should say the bounds: %v", err)
	}
}

type noAnchors struct{}

func (noAnchors) AnchorTime(context.Context, string, string, string) (int64, bool, error) {
	return 0, false, errors.New("no anchors in this test")
}

func TestAtRiskIsAFractionTheClocksAdapterAccepts(t *testing.T) {
	raw, err := readPack()
	if err != nil {
		t.Fatal(err)
	}
	book, err := intakeadapters.LoadClockBook(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, bps := range []string{"1000", "2500", "5000"} {
		r, err := intakesources.AtRiskFromEnv(settingOf(bps))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := intakeadapters.NewClocks(intakeadapters.ClocksConfig{Book: book, Anchors: noAnchors{}, AtRisk: r}); err != nil {
			t.Errorf("%s: the Clocks adapter refuses the fraction: %v", bps, err)
		}
	}
}
