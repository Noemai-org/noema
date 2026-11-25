package main

// STM-01 surfaces, the recovery pass bound (REQ-VER-004, REQ-OPS-003; INV-30, AGENT-16; A658, interim, relayed, not yet committed, Codex P2): the bound is tenants x batch x run timeout,
// and a product that does not fit saturates, term by term, instead of wrapping to a small or negative bound. Every number is SYNTHETIC.

import (
	"math"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
)

// A product that overflows int64 at any step saturates to the largest duration; it never wraps to zero or below.
func TestThePassBoundSaturatesWhenAStepOverflows(t *testing.T) {
	for name, c := range map[string]struct {
		tenants int
		cfg     recovery.Config
	}{
		"tenants x batch wraps to exactly zero": {1 << 31, recovery.Config{Batch: 1 << 31, RunTimeoutMS: 4}},
		"batch x timeout wraps negative":        {3, recovery.Config{Batch: 1, RunTimeoutMS: math.MaxInt64 / 2}},
		"tenants x batch wraps to a small one":  {1<<32 + 1, recovery.Config{Batch: 1 << 31, RunTimeoutMS: 1}},
		"every term is large":                   {math.MaxInt32, recovery.Config{Batch: math.MaxInt32, RunTimeoutMS: math.MaxInt64}},
	} {
		if got := passBound(c.tenants, c.cfg); got != time.Duration(math.MaxInt64) {
			t.Errorf("%s: bound %v; want the saturated %v", name, got, time.Duration(math.MaxInt64))
		}
	}
}

// A product that fits is exact, and a zero term gives zero (no saturation by mistake).
func TestThePassBoundIsExactWhenItFits(t *testing.T) {
	if got, want := passBound(2, recovery.Config{Batch: 50, RunTimeoutMS: 30000}), 3000*time.Second; got != want {
		t.Errorf("bound %v; want %v", got, want)
	}
	if got := passBound(0, recovery.Config{Batch: 1 << 31, RunTimeoutMS: math.MaxInt64}); got != 0 {
		t.Errorf("no tenants: bound %v; want 0", got)
	}
}
