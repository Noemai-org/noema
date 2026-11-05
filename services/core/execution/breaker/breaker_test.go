package breaker_test

// CAP-01 slice 1 (REQ-CAP-003): per-capability limits from the descriptor. The circuit breaker opens at the descriptor's error rate over
// its window, an open circuit allows no dispatch, and the per-principal rate is enforced. Time comes only from a simulated clock. SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/execution/breaker"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

var t0 = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

func limits(rate string, windowS, timeoutMs, perHour int64) contracts.CapabilityDescriptorV1Limits {
	return contracts.CapabilityDescriptorV1Limits{RatePerPrincipalPerHour: perHour, TimeoutMs: timeoutMs,
		CircuitBreaker: contracts.CapabilityDescriptorV1LimitsCircuitBreaker{ErrorRate: json.Number(rate), WindowS: windowS}}
}

// The policy numbers are inputs, not constants (AGENT-16); the tests choose their own.
func policy(minSamples int, cooldown time.Duration) breaker.Policy {
	return breaker.Policy{MinSamples: minSamples, Cooldown: cooldown}
}

// tb is what the helpers need of a test; both *testing.T and *rapid.T have it.
type tb interface {
	Helper()
	Fatal(args ...any)
}

func newBreaker(t tb, clk clock.Clock, rate string, windowS int64, p breaker.Policy, onChange breaker.Notify) *breaker.Breaker {
	t.Helper()
	params, err := breaker.ParamsOf(limits(rate, windowS, 3000, 5))
	if err != nil {
		t.Fatal(err)
	}
	b, err := breaker.New(clk, params, p, onChange)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func call(t tb, b *breaker.Breaker, ok bool) error {
	t.Helper()
	p, err := b.Allow()
	if err != nil {
		return err
	}
	p.Done(ok)
	return nil
}

func TestParamsComeFromTheDescriptorAndBadValuesAreRefused(t *testing.T) {
	p, err := breaker.ParamsOf(limits("0.5", 60, 3000, 5))
	if err != nil || p.Window != time.Minute || p.Timeout != 3*time.Second || p.RatePerPrincipalPerHour != 5 {
		t.Fatalf("params = %+v, %v", p, err)
	}
	for name, l := range map[string]contracts.CapabilityDescriptorV1Limits{
		"rate zero":        limits("0", 60, 3000, 5),
		"rate above one":   limits("1.01", 60, 3000, 5),
		"rate negative":    limits("-0.5", 60, 3000, 5),
		"rate not decimal": limits("half", 60, 3000, 5),
		"rate exponent":    limits("5e-1", 60, 3000, 5),
		"rate empty":       limits("", 60, 3000, 5),
		"window zero":      limits("0.5", 0, 3000, 5),
		"window negative":  limits("0.5", -1, 3000, 5),
		"timeout zero":     limits("0.5", 60, 0, 5),
		"per hour zero":    limits("0.5", 60, 3000, 0),
		// Codex review of noema#182: a value that would wrap time.Duration is refused, not turned into a tiny deadline.
		"window overflows a Duration":  limits("0.5", math.MaxInt64/int64(time.Second)+1, 3000, 5),
		"timeout overflows a Duration": limits("0.5", 60, math.MaxInt64/int64(time.Millisecond)+1, 5),
		"window is MaxInt64":           limits("0.5", math.MaxInt64, 3000, 5),
		"timeout is MaxInt64":          limits("0.5", 60, math.MaxInt64, 5),
	} {
		if _, err := breaker.ParamsOf(l); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	for _, r := range []string{"1", "1.0", "0.25", "0.999"} {
		if _, err := breaker.ParamsOf(limits(r, 60, 3000, 5)); err != nil {
			t.Errorf("rate %s refused: %v", r, err)
		}
	}
}

func TestThePolicyMustBeGivenAndHasNoDefaults(t *testing.T) {
	params, _ := breaker.ParamsOf(limits("0.5", 60, 3000, 5))
	clk := clock.NewManual(t0)
	for name, p := range map[string]breaker.Policy{"no samples": policy(0, time.Minute), "no cooldown": policy(4, 0), "negative cooldown": policy(4, -time.Second), "empty": {}} {
		if _, err := breaker.New(clk, params, p, nil); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := breaker.New(nil, params, policy(4, time.Minute), nil); err == nil {
		t.Error("a nil clock was accepted")
	}
}

// It opens when, with enough samples, the failure ratio reaches the descriptor's rate; before that it stays closed.
func TestItOpensAtTheDescriptorsErrorRateOnlyWithEnoughSamples(t *testing.T) {
	clk := clock.NewManual(t0)
	b := newBreaker(t, clk, "0.5", 60, policy(4, time.Minute), nil)
	_ = call(t, b, false)
	_ = call(t, b, false)
	_ = call(t, b, false) // 3 failures of 3: below the minimum samples, stays closed
	if b.State() != breaker.Closed {
		t.Fatalf("state = %s with fewer samples than the minimum", b.State())
	}
	_ = call(t, b, true) // 3 of 4 = 0.75 >= 0.5
	if b.State() != breaker.Open {
		t.Fatalf("state = %s, want OPEN at 3 of 4", b.State())
	}
	// exactly at the rate opens too: 2 of 4
	b2 := newBreaker(t, clock.NewManual(t0), "0.5", 60, policy(4, time.Minute), nil)
	_ = call(t, b2, true)
	_ = call(t, b2, true)
	_ = call(t, b2, false)
	_ = call(t, b2, false)
	if b2.State() != breaker.Open {
		t.Fatalf("state = %s, want OPEN at exactly the rate", b2.State())
	}
	// below the rate stays closed: 1 of 4 = 0.25
	b3 := newBreaker(t, clock.NewManual(t0), "0.5", 60, policy(4, time.Minute), nil)
	for _, ok := range []bool{true, true, true, false} {
		_ = call(t, b3, ok)
	}
	if b3.State() != breaker.Closed {
		t.Fatalf("state = %s, want CLOSED at 1 of 4", b3.State())
	}
}

// REQ-CAP-003 acceptance: with the circuit open, dispatches are 0.
func TestAnOpenCircuitAllowsNothingUntilTheCooldown(t *testing.T) {
	clk := clock.NewManual(t0)
	b := newBreaker(t, clk, "0.5", 60, policy(2, time.Minute), nil)
	_ = call(t, b, false)
	_ = call(t, b, false)
	for i := 0; i < 20; i++ {
		var open *breaker.OpenError
		if _, err := b.Allow(); !errors.As(err, &open) {
			t.Fatalf("an open circuit allowed a call: %v", err)
		}
		clk.Advance(time.Second)
	}
}

// After the cooldown exactly one probe goes through; a success closes the circuit, a failure reopens it for another cooldown.
func TestHalfOpenAllowsOneProbe(t *testing.T) {
	clk := clock.NewManual(t0)
	var moves []string
	b := newBreaker(t, clk, "0.5", 60, policy(2, time.Minute), func(from, to breaker.State) error { moves = append(moves, string(from)+">"+string(to)); return nil })
	_ = call(t, b, false)
	_ = call(t, b, false)
	clk.Advance(time.Minute)
	p, err := b.Allow()
	if err != nil || b.State() != breaker.HalfOpen {
		t.Fatalf("no probe after the cooldown: %v, state %s", err, b.State())
	}
	if _, err := b.Allow(); err == nil {
		t.Fatal("a second call went through while the probe was in flight")
	}
	p.Done(false)
	if b.State() != breaker.Open {
		t.Fatalf("state = %s after a failed probe, want OPEN", b.State())
	}
	if _, err := b.Allow(); err == nil {
		t.Fatal("the circuit allowed a call right after a failed probe")
	}
	clk.Advance(time.Minute)
	p, err = b.Allow()
	if err != nil {
		t.Fatal(err)
	}
	p.Done(true)
	if b.State() != breaker.Closed {
		t.Fatalf("state = %s after a good probe, want CLOSED", b.State())
	}
	want := []string{"CLOSED>OPEN", "OPEN>HALF_OPEN", "HALF_OPEN>OPEN", "OPEN>HALF_OPEN", "HALF_OPEN>CLOSED"}
	if len(moves) != len(want) {
		t.Fatalf("transitions = %v, want %v", moves, want)
	}
	for i := range want {
		if moves[i] != want[i] {
			t.Fatalf("transitions = %v, want %v", moves, want)
		}
	}
	// the closed circuit starts with an empty window: one failure does not reopen it
	_ = call(t, b, false)
	if b.State() != breaker.Closed {
		t.Fatalf("state = %s, the window was not cleared when the circuit closed", b.State())
	}
}

// Outcomes older than the window no longer count.
func TestOutcomesOutsideTheWindowDoNotCount(t *testing.T) {
	clk := clock.NewManual(t0)
	b := newBreaker(t, clk, "0.5", 60, policy(2, time.Minute), nil)
	_ = call(t, b, false)
	clk.Advance(61 * time.Second)
	_ = call(t, b, false) // the first failure is outside the window: 1 sample, below the minimum
	if b.State() != breaker.Closed {
		t.Fatalf("state = %s, an expired outcome was counted", b.State())
	}
}

// A permit settles once; a second Done changes nothing.
func TestAPermitIsSettledOnce(t *testing.T) {
	clk := clock.NewManual(t0)
	b := newBreaker(t, clk, "0.5", 60, policy(2, time.Minute), nil)
	p, _ := b.Allow()
	p.Done(false)
	p.Done(false)
	p.Done(false)
	if b.State() != breaker.Closed {
		t.Fatalf("state = %s, one failed call counted more than once", b.State())
	}
}

// Property (REQ-CAP-003): whatever the sequence of calls, results and clock steps, no call is ever allowed while the circuit is open
// and its cooldown has not passed.
func TestPropertyNoCallIsAllowedWhileOpen(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		clk := clock.NewManual(t0)
		cool := time.Duration(rapid.IntRange(1, 90).Draw(t, "cooldown_s")) * time.Second
		b := newBreaker(t, clk, "0.5", int64(rapid.IntRange(1, 120).Draw(t, "window_s")), policy(rapid.IntRange(1, 6).Draw(t, "min"), cool), nil)
		var openedAt time.Time
		for i := 0; i < 60; i++ {
			switch rapid.IntRange(0, 2).Draw(t, "op") {
			case 0:
				clk.Advance(time.Duration(rapid.IntRange(0, 100).Draw(t, "step_s")) * time.Second)
			default:
				before := b.State()
				p, err := b.Allow()
				if before == breaker.Open && clk.Now().Sub(openedAt) < cool && err == nil {
					t.Fatalf("a call was allowed %s into a %s cooldown", clk.Now().Sub(openedAt), cool)
				}
				if err == nil {
					_ = p.Done(rapid.Bool().Draw(t, "ok"))
				}
			}
			if b.State() == breaker.Open {
				openedAt = b.OpenSince()
			}
		}
	})
}

func TestItIsSafeUnderConcurrentUse(t *testing.T) {
	clk := clock.NewManual(t0)
	b := newBreaker(t, clk, "0.5", 60, policy(4, time.Minute), nil)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if p, err := b.Allow(); err == nil {
					p.Done((i+g)%3 != 0)
				}
				_ = b.State()
			}
		}(g)
	}
	wg.Wait()
}

// The per-principal rate of the descriptor: at most N calls per principal in any hour; another principal has its own count.
func TestThePerPrincipalRateIsEnforcedPerHour(t *testing.T) {
	clk := clock.NewManual(t0)
	params, _ := breaker.ParamsOf(limits("0.5", 60, 3000, 3))
	l, err := breaker.NewRateLimiter(clk, params)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if !l.Allow("usr_a") {
			t.Fatalf("call %d of 3 was refused", i+1)
		}
		clk.Advance(time.Minute)
	}
	if l.Allow("usr_a") {
		t.Fatal("a fourth call in the hour was allowed")
	}
	if !l.Allow("usr_b") {
		t.Fatal("another principal was limited by usr_a")
	}
	clk.Advance(time.Hour - 3*time.Minute) // the first call is exactly one hour old
	if !l.Allow("usr_a") {
		t.Fatal("the hour passed and the call was still refused")
	}
	if l.Allow("") {
		t.Fatal("an empty principal was allowed")
	}
}

// The largest values that still fit a Duration are accepted.
func TestTheLargestValuesThatFitADurationAreAccepted(t *testing.T) {
	p, err := breaker.ParamsOf(limits("0.5", math.MaxInt64/int64(time.Second), math.MaxInt64/int64(time.Millisecond), 5))
	if err != nil || p.Window <= 0 || p.Timeout <= 0 {
		t.Fatalf("params = %+v, %v", p, err)
	}
}

// Codex review of noema#182: the limiter's memory is bounded. A principal that calls once and never again leaves no entry after the
// hour, whatever other principals do.
func TestTheLimiterForgetsPrincipalsThatStoppedCalling(t *testing.T) {
	clk := clock.NewManual(t0)
	params, _ := breaker.ParamsOf(limits("0.5", 60, 3000, 3))
	l, err := breaker.NewRateLimiter(clk, params)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if !l.Allow(fmt.Sprintf("usr_%d", i)) {
			t.Fatalf("principal %d refused", i)
		}
	}
	if n := l.Principals(); n != 1000 {
		t.Fatalf("principals = %d, want 1000", n)
	}
	clk.Advance(2 * time.Hour)
	l.Allow("usr_x") // any call after the hour sweeps the principals that went quiet
	if n := l.Principals(); n != 1 {
		t.Fatalf("principals = %d after two quiet hours, want 1", n)
	}
}

// Codex review of noema#182: state-change callbacks are delivered one at a time and in transition order, so the CIRCUIT_STATE_CHANGED
// record never shows HALF_OPEN before the OPEN that led to it, and two callbacks never run at once.
func TestStateChangesAreDeliveredInOrderAndOneAtATime(t *testing.T) {
	clk := clock.NewManual(t0)
	var mu sync.Mutex
	var moves [][2]breaker.State
	running := 0
	overlap := false
	cb := func(from, to breaker.State) error {
		mu.Lock()
		running++
		if running > 1 {
			overlap = true
		}
		mu.Unlock()
		time.Sleep(time.Millisecond) // a slow observer widens the window in which another transition could overtake
		mu.Lock()
		moves = append(moves, [2]breaker.State{from, to})
		running--
		mu.Unlock()
		return nil
	}
	b := newBreaker(t, clk, "0.5", 60, policy(1, time.Second), cb)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 150; i++ {
				if p, err := b.Allow(); err == nil {
					p.Done(false) // every settled call fails, so the circuit keeps opening and probing
				}
				if g == 0 {
					clk.Advance(time.Second)
				}
			}
		}(g)
	}
	wg.Wait()
	b.Flush() // everything queued is delivered before we look
	mu.Lock()
	defer mu.Unlock()
	if overlap {
		t.Fatal("two state-change callbacks ran at the same time")
	}
	if len(moves) == 0 {
		t.Fatal("no transition happened; the test did not exercise the breaker")
	}
	prev := breaker.Closed
	for i, m := range moves {
		if m[0] != prev {
			t.Fatalf("transition %d is %s>%s but the previous one ended in %s: delivered out of order (moves %v)", i, m[0], m[1], prev, moves)
		}
		prev = m[1]
	}
}

// Codex review of noema#182 (second): a caller does not return, and so cannot act on the new state, before the notification of its own
// transition has been delivered. Here the OPEN notification is slow; the HALF_OPEN transition queued behind it must not return before
// its own notification has run.
func TestACallerWaitsForItsOwnTransitionsNotification(t *testing.T) {
	clk := clock.NewManual(t0)
	gate := make(chan struct{})
	var mu sync.Mutex
	delivered := map[breaker.State]bool{}
	cb := func(from, to breaker.State) error {
		if to == breaker.Open {
			<-gate // the slow observer
		}
		mu.Lock()
		delivered[to] = true
		mu.Unlock()
		return nil
	}
	b := newBreaker(t, clk, "0.5", 60, policy(1, time.Second), cb)
	openDone := make(chan struct{})
	go func() { // opens the circuit and blocks in the OPEN notification
		p, err := b.Allow()
		if err != nil {
			t.Error(err)
			return
		}
		_ = p.Done(false)
		close(openDone)
	}()
	for b.State() != breaker.Open { // wait until the circuit is open and its notification is stuck
		time.Sleep(time.Millisecond)
	}
	clk.Advance(2 * time.Second)
	halfOpen := make(chan error, 1)
	go func() {
		p, err := b.Allow() // moves OPEN to HALF_OPEN; its notification queues behind the stuck one
		if err == nil {
			mu.Lock()
			ok := delivered[breaker.HalfOpen]
			mu.Unlock()
			if !ok {
				err = errors.New("Allow returned a probe before the HALF_OPEN notification was delivered")
			}
			_ = p
		}
		halfOpen <- err
	}()
	select {
	case err := <-halfOpen:
		t.Fatalf("Allow returned while an earlier notification was still being delivered: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	if err := <-halfOpen; err != nil {
		t.Fatal(err)
	}
	<-openDone
}

// A notification that fails is surfaced, and a transition that would let calls through is undone: the circuit goes back to OPEN
// (fail closed), because the state change was not recorded.
func TestAFailedNotificationOfAPermissiveTransitionReopensTheCircuit(t *testing.T) {
	boom := errors.New("record append failed")
	for _, to := range []breaker.State{breaker.HalfOpen, breaker.Closed} {
		clk := clock.NewManual(t0)
		fail := true
		b := newBreaker(t, clk, "0.5", 60, policy(1, time.Second), func(from, next breaker.State) error {
			if fail && next == to {
				return boom
			}
			return nil
		})
		p, _ := b.Allow()
		if err := p.Done(false); err != nil { // CLOSED to OPEN is recorded fine
			t.Fatal(err)
		}
		clk.Advance(2 * time.Second)
		if to == breaker.HalfOpen {
			if _, err := b.Allow(); !errors.Is(err, boom) {
				t.Fatalf("Allow = %v, want the notification failure", err)
			}
			if b.State() != breaker.Open {
				t.Fatalf("state = %s after a failed HALF_OPEN record, want OPEN", b.State())
			}
			continue
		}
		fail = false
		probe, err := b.Allow()
		if err != nil {
			t.Fatal(err)
		}
		fail = true
		if err := probe.Done(true); !errors.Is(err, boom) {
			t.Fatalf("Done = %v, want the notification failure", err)
		}
		if b.State() != breaker.Open {
			t.Fatalf("state = %s after a failed CLOSED record, want OPEN", b.State())
		}
		if _, err := b.Allow(); err == nil {
			t.Fatal("a call was allowed right after a closing that was not recorded")
		}
	}
}

// A call is not let through on a CLOSED circuit whose CLOSED record is not yet written; if that record fails, the circuit reopens and the
// waiting call is refused (Codex review of noema#182, P1; AGENTS.md section 5: the Record comes first).
func TestNoCallPassesOnAClosedCircuitWhoseRecordIsStillPending(t *testing.T) {
	boom := errors.New("record append failed")
	clk := clock.NewManual(t0)
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	slow := false
	b := newBreaker(t, clk, "0.5", 60, policy(1, time.Second), func(from, to breaker.State) error {
		if slow && to == breaker.Closed {
			entered <- struct{}{}
			<-gate
			return boom
		}
		return nil
	})
	p, _ := b.Allow()
	if err := p.Done(false); err != nil {
		t.Fatal(err)
	}
	clk.Advance(2 * time.Second)
	probe, err := b.Allow()
	if err != nil {
		t.Fatal(err)
	}
	slow = true
	closing := make(chan error, 1)
	go func() { closing <- probe.Done(true) }()
	<-entered // the CLOSED notification is now running
	other := make(chan error, 1)
	go func() {
		pm, err := b.Allow()
		if err == nil {
			_ = pm.Done(true)
		}
		other <- err
	}()
	select {
	case err := <-other:
		t.Fatalf("Allow returned while the CLOSED record was pending: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	if err := <-closing; !errors.Is(err, boom) {
		t.Fatalf("Done = %v, want the notification failure", err)
	}
	if err := <-other; err == nil {
		t.Fatal("a call was allowed although the CLOSED record failed")
	}
}

// A failed OPEN notification is surfaced too, and the circuit stays open (opening is the safe direction).
func TestAFailedNotificationOfOpeningIsSurfacedAndTheCircuitStaysOpen(t *testing.T) {
	boom := errors.New("record append failed")
	b := newBreaker(t, clock.NewManual(t0), "0.5", 60, policy(1, time.Minute), func(from, to breaker.State) error { return boom })
	p, _ := b.Allow()
	if err := p.Done(false); !errors.Is(err, boom) {
		t.Fatalf("Done = %v, want the notification failure", err)
	}
	if b.State() != breaker.Open {
		t.Fatalf("state = %s, want OPEN", b.State())
	}
}
