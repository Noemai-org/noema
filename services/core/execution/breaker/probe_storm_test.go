package breaker_test

// CAP-01 / ruling A3 (Track 6): one probe at a time, and no probe storm. A cooldown that ends admits exactly one call however many callers arrive at that
// instant, and a probe that fails is followed by a full cooldown before the next. All numbers are chosen by the test (AGENT-16); the clock is the
// simulated one. REQ-CAP-004 and INV-13 (a limit holds under load). All data is SYNTHETIC.

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/execution/breaker"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// However many callers arrive when the cooldown ends, exactly one is admitted as the probe.
func TestAStormOfCallersAtTheEndOfTheCooldownAdmitsExactlyOneProbe(t *testing.T) {
	for _, callers := range []int{2, 8, 64} {
		clk := clock.NewManual(t0)
		b := newBreaker(t, clk, "0.5", 60, policy(2, time.Minute), nil)
		_ = call(t, b, false)
		_ = call(t, b, false)
		clk.Advance(time.Minute)
		var admitted atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, err := b.Allow(); err == nil {
					admitted.Add(1)
				}
			}()
		}
		close(start)
		wg.Wait()
		if n := admitted.Load(); n != 1 {
			t.Fatalf("%d callers at the end of the cooldown: %d admitted, want exactly 1", callers, n)
		}
	}
}

// Over any interleaving of arrivals, clock steps and settlements: at most one probe is outstanding, and a probe is never admitted less than a cooldown
// after the previous probe failed.
func TestPropertyNoProbeStorm(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		clk := clock.NewManual(t0)
		cool := time.Duration(rapid.IntRange(1, 90).Draw(t, "cooldown_s")) * time.Second
		b := newBreaker(t, clk, "0.5", int64(rapid.IntRange(1, 120).Draw(t, "window_s")), policy(rapid.IntRange(1, 6).Draw(t, "min"), cool), nil)
		var probe *breaker.Permit
		var lastFail time.Time
		for i := 0; i < 80; i++ {
			switch rapid.IntRange(0, 3).Draw(t, "op") {
			case 0:
				clk.Advance(time.Duration(rapid.IntRange(0, 100).Draw(t, "step_s")) * time.Second)
			case 1:
				if probe != nil {
					ok := rapid.Bool().Draw(t, "probe_ok")
					if !ok {
						lastFail = clk.Now()
					}
					_ = probe.Done(ok)
					probe = nil
				}
			default:
				wasOpen := b.State() == breaker.Open
				p, err := b.Allow()
				if err != nil {
					continue
				}
				if b.State() == breaker.HalfOpen || wasOpen {
					if probe != nil {
						t.Fatalf("a second probe was admitted while one was in flight")
					}
					if !lastFail.IsZero() && clk.Now().Sub(lastFail) < cool {
						t.Fatalf("a probe was admitted %s after the last failed probe, cooldown %s", clk.Now().Sub(lastFail), cool)
					}
					probe = p
					continue
				}
				_ = p.Done(rapid.Bool().Draw(t, "ok"))
			}
		}
	})
}
