package breaker

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sync"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

const errPrefix = "breaker: "

// decimalRe is the only form of error_rate accepted: a plain decimal, no sign, no exponent, no fraction bar.
var decimalRe = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// Params are the limits of one capability, as its descriptor declares them. A value that is missing, zero or malformed is refused.
type Params struct {
	Window                  time.Duration // circuit_breaker.window_s
	Timeout                 time.Duration // timeout_ms
	RatePerPrincipalPerHour int64
	errorRate               *big.Rat // circuit_breaker.error_rate, exact (no float)
}

// ParamsOf reads the limits of a descriptor.
func ParamsOf(l contracts.CapabilityDescriptorV1Limits) (Params, error) {
	s := l.CircuitBreaker.ErrorRate.String()
	if !decimalRe.MatchString(s) {
		return Params{}, fmt.Errorf("%serror_rate %q is not a plain decimal", errPrefix, s)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Sign() <= 0 || r.Cmp(big.NewRat(1, 1)) > 0 {
		return Params{}, fmt.Errorf("%serror_rate %q is not in (0, 1]", errPrefix, s)
	}
	switch {
	case l.CircuitBreaker.WindowS <= 0 || l.CircuitBreaker.WindowS > math.MaxInt64/int64(time.Second):
		return Params{}, errors.New(errPrefix + "window_s must be positive and fit a duration")
	case l.TimeoutMs <= 0 || l.TimeoutMs > math.MaxInt64/int64(time.Millisecond):
		return Params{}, errors.New(errPrefix + "timeout_ms must be positive and fit a duration")
	case l.RatePerPrincipalPerHour <= 0:
		return Params{}, errors.New(errPrefix + "rate_per_principal_per_hour must be positive")
	}
	return Params{Window: time.Duration(l.CircuitBreaker.WindowS) * time.Second, Timeout: time.Duration(l.TimeoutMs) * time.Millisecond,
		RatePerPrincipalPerHour: l.RatePerPrincipalPerHour, errorRate: r}, nil
}

// Policy holds the two numbers the descriptor does not carry. They have no defaults here: choosing them is a policy decision
// (AGENT-16), so the caller must supply them from configuration.
type Policy struct {
	MinSamples int           // outcomes needed in the window before the circuit may open
	Cooldown   time.Duration // how long it stays open before one probe is allowed
}

// State is the state of a circuit.
type State string

const (
	Closed   State = "CLOSED"
	Open     State = "OPEN"
	HalfOpen State = "HALF_OPEN"
)

// OpenError is returned by Allow when the circuit allows no call.
type OpenError struct {
	State State
	Until time.Time // the earliest instant a probe may be allowed; zero while a probe is in flight
}

func (e *OpenError) Error() string { return errPrefix + "the circuit is " + string(e.State) }

type outcome struct {
	at time.Time
	ok bool
}

// Breaker is the circuit breaker of one capability (and, by construction, one tenant). It is safe for concurrent use.
type Breaker struct {
	clk      clock.Clock
	p        Params
	pol      Policy
	onChange func(from, to State)

	mu       sync.Mutex
	state    State
	gen      uint64 // changes with every state change, so a stale permit is ignored
	openedAt time.Time
	probing  bool
	window   []outcome

	// State-change notifications are queued under mu and delivered one at a time, in the order the states changed, by whichever goroutine
	// finds the queue non-empty and no delivery running (Codex review of noema#182). idle is signalled when the queue is empty and idle.
	pending    []func()
	delivering bool
	idle       *sync.Cond
}

// New builds a closed circuit. onChange, if not nil, is called after every state change, outside the lock; the caller records
// CIRCUIT_STATE_CHANGED there.
func New(clk clock.Clock, p Params, pol Policy, onChange func(from, to State)) (*Breaker, error) {
	switch {
	case clk == nil:
		return nil, errors.New(errPrefix + "a clock is required")
	case p.errorRate == nil || p.Window <= 0:
		return nil, errors.New(errPrefix + "params must come from ParamsOf")
	case pol.MinSamples <= 0 || pol.Cooldown <= 0:
		return nil, errors.New(errPrefix + "the policy needs positive MinSamples and Cooldown; there are no defaults")
	}
	b := &Breaker{clk: clk, p: p, pol: pol, onChange: onChange, state: Closed}
	b.idle = sync.NewCond(&b.mu)
	return b, nil
}

// Timeout is the descriptor's call timeout; the caller applies it as a context deadline.
func (b *Breaker) Timeout() time.Duration { return b.p.Timeout }

// State is the current state. An open circuit stays OPEN until a call arrives after the cooldown.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// OpenSince is the instant the circuit last opened; zero if it never has.
func (b *Breaker) OpenSince() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.openedAt
}

// Permit settles one allowed call.
type Permit struct {
	b     *Breaker
	gen   uint64
	probe bool
	once  sync.Once
}

// Allow asks to make one call. It returns a Permit to settle with Done, or an *OpenError. While the circuit is open, or a probe is in
// flight, no call is allowed (REQ-CAP-003: dispatches are 0 with the circuit open).
func (b *Breaker) Allow() (*Permit, error) {
	b.mu.Lock()
	now := b.clk.Now()
	switch b.state {
	case Open:
		if now.Sub(b.openedAt) < b.pol.Cooldown {
			err := &OpenError{State: Open, Until: b.openedAt.Add(b.pol.Cooldown)}
			b.mu.Unlock()
			return nil, err
		}
		b.move(HalfOpen)
		b.probing = true
	case HalfOpen:
		if b.probing {
			b.mu.Unlock()
			return nil, &OpenError{State: HalfOpen}
		}
		b.probing = true
	}
	pm := &Permit{b: b, gen: b.gen, probe: b.state == HalfOpen}
	b.mu.Unlock()
	b.deliver()
	return pm, nil
}

// Done settles the call: ok is whether it succeeded. Only the first call counts. A permit from before a state change is ignored.
func (pm *Permit) Done(ok bool) {
	pm.once.Do(func() { pm.b.settle(pm, ok) })
}

func (b *Breaker) settle(pm *Permit, ok bool) {
	b.mu.Lock()
	if pm.gen != b.gen {
		b.mu.Unlock()
		return
	}
	now := b.clk.Now()
	switch {
	case pm.probe && ok:
		b.move(Closed)
	case pm.probe:
		b.move(Open)
		b.openedAt = now
	default:
		b.window = append(b.window, outcome{at: now, ok: ok})
		b.prune(now)
		if b.shouldOpen() {
			b.move(Open)
			b.openedAt = now
		}
	}
	b.mu.Unlock()
	b.deliver()
}

// move changes the state under the lock and queues the notification; deliver runs it after the lock is released.
func (b *Breaker) move(to State) {
	from := b.state
	b.state = to
	b.gen++
	b.probing = false
	b.window = nil
	if b.onChange != nil {
		cb := b.onChange
		b.pending = append(b.pending, func() { cb(from, to) })
	}
}

// deliver runs the queued notifications, one at a time and in order, outside the state lock. If another goroutine is already
// delivering it returns at once: that goroutine will deliver what was queued.
func (b *Breaker) deliver() {
	b.mu.Lock()
	if b.delivering {
		b.mu.Unlock()
		return
	}
	b.delivering = true
	for len(b.pending) > 0 {
		next := b.pending[0]
		b.pending = b.pending[1:]
		b.mu.Unlock()
		next()
		b.mu.Lock()
	}
	b.delivering = false
	b.idle.Broadcast()
	b.mu.Unlock()
}

// Flush blocks until every queued notification has been delivered. Tests and orderly shutdown use it.
func (b *Breaker) Flush() {
	b.mu.Lock()
	for b.delivering || len(b.pending) > 0 {
		if !b.delivering {
			b.mu.Unlock()
			b.deliver()
			b.mu.Lock()
			continue
		}
		b.idle.Wait()
	}
	b.mu.Unlock()
}

func (b *Breaker) prune(now time.Time) {
	keep := b.window[:0]
	for _, o := range b.window {
		if now.Sub(o.at) <= b.p.Window {
			keep = append(keep, o)
		}
	}
	b.window = keep
}

// shouldOpen: at least MinSamples outcomes in the window and a failure ratio at or above the descriptor's error rate.
func (b *Breaker) shouldOpen() bool {
	total := len(b.window)
	if total < b.pol.MinSamples {
		return false
	}
	var failed int64
	for _, o := range b.window {
		if !o.ok {
			failed++
		}
	}
	return new(big.Rat).SetFrac64(failed, int64(total)).Cmp(b.p.errorRate) >= 0
}

// RateLimiter enforces rate_per_principal_per_hour: at most that many calls by one principal in any sliding hour.
type RateLimiter struct {
	clk clock.Clock
	max int64

	mu        sync.Mutex
	calls     map[string][]time.Time
	lastSweep time.Time
}

// NewRateLimiter builds a limiter from a descriptor's params.
func NewRateLimiter(clk clock.Clock, p Params) (*RateLimiter, error) {
	if clk == nil || p.RatePerPrincipalPerHour <= 0 {
		return nil, errors.New(errPrefix + "a clock and params from ParamsOf are required")
	}
	return &RateLimiter{clk: clk, max: p.RatePerPrincipalPerHour, calls: map[string][]time.Time{}, lastSweep: clk.Now()}, nil
}

// Allow records and allows a call by the principal, or refuses it. An empty principal is always refused.
func (l *RateLimiter) Allow(principal string) bool {
	if principal == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clk.Now()
	l.sweep(now)
	recent := l.calls[principal][:0]
	for _, at := range l.calls[principal] {
		if now.Sub(at) < time.Hour {
			recent = append(recent, at)
		}
	}
	if int64(len(recent)) >= l.max {
		l.calls[principal] = recent
		return false
	}
	l.calls[principal] = append(recent, now)
	return true
}

// sweep drops every principal whose calls have all expired, at most once an hour, so the limiter holds state only for principals that
// called in the last two hours (and at most max timestamps each): inactive principals do not accumulate (Codex review of noema#182).
func (l *RateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Hour {
		return
	}
	l.lastSweep = now
	for principal, calls := range l.calls {
		if len(calls) == 0 || now.Sub(calls[len(calls)-1]) >= time.Hour {
			delete(l.calls, principal)
		}
	}
}

// Principals is the number of principals the limiter currently holds state for.
func (l *RateLimiter) Principals() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}
