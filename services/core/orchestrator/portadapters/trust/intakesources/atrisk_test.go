package intakesources_test

// Proves REQ-TRU-006, AGENT-16, A447 and A416 (DEC-2026-11-20-architect-rulings-a358-a453; they correct A345 S4): the at-risk value of a clock has ONE source,
// clock_at_risk_remaining_bp of the pack's trust-limits document, read through platform/trustlimits. The fraction the Clocks adapter takes is that number of basis points over
// ten thousand. This package holds no setting, no default and no copy of the bounds: a document outside the bounds of A233 is refused by trustlimits.Load and gives no Limits,
// and a Limits that was not loaded gives no fraction.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/platform/trustlimits"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
)

const limitsPath = "../../../../../../packs/fintech/limits/trust_limits.json"

// limitsDoc is a SYNTHETIC trust-limits.v1 document for pack fin with the given at-risk value.
func limitsDoc(atRisk int) string {
	return fmt.Sprintf(`{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","ambiguity_absolute_floor_bp":7000,"clock_at_risk_remaining_bp":%d}`, atRisk)
}

func loadLimits(t *testing.T, doc string) (trustlimits.Limits, error) {
	t.Helper()
	return trustlimits.Load(strings.NewReader(doc), "fin")
}

type refusingAnchors struct{}

func (refusingAnchors) AnchorTime(context.Context, string, string, string) (int64, bool, error) {
	return 0, false, errors.New("no anchors in this test")
}

func TestAtRiskFromLimitsIsTheClockAtRiskRemainingBPOfTheShippedPack(t *testing.T) {
	f, err := os.Open(limitsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := trustlimits.Load(f, "fin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := intakesources.AtRiskFromLimits(l)
	if err != nil || got != (clocks.Ratio{Num: 2500, Den: 10000}) {
		t.Errorf("the shipped pack says 2500 basis points: got %+v, %v", got, err)
	}
}

func TestAtRiskFromLimitsIsTheLoadedValueOverTenThousandAtEveryPointOfTheBounds(t *testing.T) {
	for _, bps := range []int{1000, 1234, 2500, 3333, 4999, 5000} {
		l, err := loadLimits(t, limitsDoc(bps))
		if err != nil {
			t.Fatalf("%d: %v", bps, err)
		}
		got, err := intakesources.AtRiskFromLimits(l)
		if err != nil || got != (clocks.Ratio{Num: int64(bps), Den: 10000}) {
			t.Errorf("%d: got %+v, %v", bps, got, err)
		}
	}
}

func TestAtRiskFromLimitsHasNoDefaultAndRefusesALimitsThatWasNotLoaded(t *testing.T) {
	got, err := intakesources.AtRiskFromLimits(trustlimits.Limits{})
	if !errors.Is(err, intakesources.ErrConfig) || got != (clocks.Ratio{}) {
		t.Errorf("an unloaded Limits has no at-risk value and no default: got %+v, %v", got, err)
	}
}

func TestAtRiskFromLimitsARefusedDocumentGivesNoFraction(t *testing.T) {
	for _, bps := range []int{-1, 0, 999, 5001, 10000, 10001} {
		l, err := loadLimits(t, limitsDoc(bps))
		if err == nil {
			t.Fatalf("%d: trustlimits.Load accepted a value outside the bounds of A233", bps)
		}
		got, err := intakesources.AtRiskFromLimits(l)
		if !errors.Is(err, intakesources.ErrConfig) || got != (clocks.Ratio{}) {
			t.Errorf("%d: the refused document must give no fraction: got %+v, %v", bps, got, err)
		}
	}
}

func TestAtRiskFromLimitsTheErrorIsFixedAndKeepsNoValue(t *testing.T) {
	_, err := intakesources.AtRiskFromLimits(trustlimits.Limits{})
	if err == nil || !strings.Contains(err.Error(), "not configured") || !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("want the fixed ErrConfig text: %v", err)
	}
}

func TestAtRiskFromLimitsIsAFractionTheClocksAdapterAccepts(t *testing.T) {
	raw, err := readPack()
	if err != nil {
		t.Fatal(err)
	}
	book, err := intakeadapters.LoadClockBook(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, bps := range []int{1000, 2500, 5000} {
		l, err := loadLimits(t, limitsDoc(bps))
		if err != nil {
			t.Fatal(err)
		}
		r, err := intakesources.AtRiskFromLimits(l)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := intakeadapters.NewClocks(intakeadapters.ClocksConfig{Book: book, Anchors: refusingAnchors{}, AtRisk: r}); err != nil {
			t.Errorf("%d: the Clocks adapter refuses the fraction: %v", bps, err)
		}
	}
}
