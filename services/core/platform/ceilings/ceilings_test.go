package ceilings_test

// A154 as corrected, REQ-AMB-004, REQ-SEC-003: the digit ceiling is the one number of masking-limits.v1, so a clarification label can never show more than a masked
// surface may. If the pack's masking limit and this ceiling ever differ, this test fails. The values are SYNTHETIC.

import (
	"os"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/ceilings"
	"github.com/Noemai-org/noema/services/core/platform/masking"
)

func TestTheDigitCeilingIsTheMaskingLimitOfThePack(t *testing.T) {
	f, err := os.Open("../../../../packs/fintech/limits/masking_limits.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := masking.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if l.MaxUnmaskedRun() != ceilings.MaxLabelDigitRun {
		t.Fatalf("masking-limits allows %d unmasked digits, the clarification ceiling is %d", l.MaxUnmaskedRun(), ceilings.MaxLabelDigitRun)
	}
}
