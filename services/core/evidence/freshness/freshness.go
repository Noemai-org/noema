// Package freshness is the effective freshness of an evidence item and the one refresh a stale source gets (REQ-CTX-003, INV-23).
//
// The effective limit is the strictest of the source's limit, the trust profile's limit and a per-source override (PERD 21.2): a profile or an
// override can only tighten. The age is measured from the item's read_at on the injected clock (INV-21), and an item is stale when it is OLDER than
// its limit (PERD 21.1), so an item exactly at the limit is fresh. Nothing here sets a limit: the limits are inputs from the pack (AGENT-16).
//
// A stale source is re-read at most once per evaluation (a Session is one evaluation). A refresh that fails, that answers with another stale read or
// that cannot be recorded makes the source UNAVAILABLE and returns no item: the stale item is never reused (INV-16). The EVIDENCE_REFRESHED event
// goes to a Sink before the refreshed item is returned (Record first, PERD 32.4).
package freshness

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// Typed causes of an UNAVAILABLE result (no string matching).
var (
	ErrNoLimit          = errors.New("freshness: no usable limit")
	ErrUnusableReadAt   = errors.New("freshness: the item's read_at is unusable")
	ErrRefreshFailed    = errors.New("freshness: the refresh could not be made")
	ErrRefreshStale     = errors.New("freshness: the refresh answered with a stale read")
	ErrAlreadyRefreshed = errors.New("freshness: the source was already refreshed in this evaluation")
	ErrRecord           = errors.New("freshness: the refresh could not be recorded")
)

// Limits are the three limits of PERD 21.2. Source must be positive. A zero Profile or Override sets none; a negative value is refused.
type Limits struct {
	Source   time.Duration // freshness_sla_seconds of the evidence source
	Profile  time.Duration // the trust profile's max_evidence_age_seconds
	Override time.Duration // a per-source override; nothing supplies one yet
}

// Effective is the strictest of the three limits and the one that set it. On a tie the earlier of source, profile and override is named.
func Effective(l Limits) (time.Duration, contracts.EnumsV1LimitSetBy, error) {
	if l.Source <= 0 || l.Profile < 0 || l.Override < 0 {
		return 0, "", ErrNoLimit
	}
	limit, by := l.Source, contracts.EnumsV1LimitSetBySOURCE
	if l.Profile > 0 && l.Profile < limit {
		limit, by = l.Profile, contracts.EnumsV1LimitSetByTRUSTPROFILE
	}
	if l.Override > 0 && l.Override < limit {
		limit, by = l.Override, contracts.EnumsV1LimitSetByPROFILEOVERRIDE
	}
	return limit, by, nil
}

// Verdict is what the age of one read says about it.
type Verdict int

const (
	// Unusable: no limit, a zero read_at or a read from the future. It is neither fresh nor refreshed on its own.
	Unusable Verdict = iota
	Fresh
	Stale
)

func (v Verdict) String() string {
	switch v {
	case Fresh:
		return "Fresh"
	case Stale:
		return "Stale"
	default:
		return "Unusable"
	}
}

// Check is the verdict of a read made at readAt, seen at now, under limit. Integer arithmetic on durations only.
func Check(readAt, now time.Time, limit time.Duration) Verdict {
	if limit <= 0 || readAt.IsZero() || readAt.After(now) {
		return Unusable
	}
	if now.Sub(readAt) > limit {
		return Stale
	}
	return Fresh
}

// Item is the part of an evidence item a refresh decision needs.
type Item struct {
	EvidenceID string
	ReadAt     time.Time
}

// Outcome of Ensure.
type Outcome string

const (
	OutcomeFresh       Outcome = "FRESH"
	OutcomeRefreshed   Outcome = "REFRESHED"
	OutcomeUnavailable Outcome = "UNAVAILABLE"
)

// Result of Ensure. For UNAVAILABLE the Item is the zero value: a stale item is never handed back. Limit and LimitSetBy are what the Decision
// Record's evidence_freshness[] item needs; they are zero only when no limit was usable.
type Result struct {
	Outcome    Outcome
	Item       Item
	Err        error
	Limit      time.Duration
	LimitSetBy contracts.EnumsV1LimitSetBy
}

// RefreshEvent is the content of EVIDENCE_REFRESHED: ids, the source and numbers, never a payload.
type RefreshEvent struct {
	Source          string
	PriorEvidenceID string
	EvidenceID      string
	Limit           time.Duration
	LimitSetBy      contracts.EnumsV1LimitSetBy
	RefreshedAt     time.Time
}

// Sink records a refresh. It must not return nil unless the event is durably appended.
type Sink interface {
	Refreshed(ctx context.Context, e RefreshEvent) error
}

// Reread makes the one refresh read of a source and returns the new item.
type Reread func(ctx context.Context) (Item, error)

// Session is one evaluation: each source is refreshed at most once in it.
type Session struct {
	clk     clock.Clock
	sink    Sink
	timeout time.Duration

	mu    sync.Mutex
	tried map[string]*attempt
}

type attempt struct {
	done chan struct{}
	res  Result
}

// NewSession refuses a nil clock or sink and a non-positive timeout (a context deadline on every call).
func NewSession(clk clock.Clock, sink Sink, refreshTimeout time.Duration) (*Session, error) {
	switch {
	case clk == nil:
		return nil, errors.New("freshness: a clock is required")
	case sink == nil:
		return nil, errors.New("freshness: a sink is required")
	case refreshTimeout <= 0:
		return nil, errors.New("freshness: the refresh timeout must be positive")
	}
	return &Session{clk: clk, sink: sink, timeout: refreshTimeout, tried: map[string]*attempt{}}, nil
}

// Ensure returns have when it is fresh, and otherwise re-reads the source once. A second ask for a source already refreshed in this evaluation
// is answered from the first attempt: a fresh outcome stays usable, anything else is UNAVAILABLE and nothing is read again.
func (s *Session) Ensure(ctx context.Context, source string, l Limits, have Item, reread Reread) Result {
	limit, by, err := Effective(l)
	if err != nil {
		return Result{Outcome: OutcomeUnavailable, Err: err}
	}
	base := Result{Limit: limit, LimitSetBy: by}
	switch Check(have.ReadAt, s.clk.Now(), limit) {
	case Fresh:
		base.Outcome, base.Item = OutcomeFresh, have
		return base
	case Unusable:
		base.Outcome, base.Err = OutcomeUnavailable, ErrUnusableReadAt
		return base
	}
	s.mu.Lock()
	if prev, ok := s.tried[source]; ok {
		s.mu.Unlock()
		<-prev.done
		out := prev.res
		if out.Outcome != OutcomeRefreshed {
			base.Outcome, base.Err = OutcomeUnavailable, ErrAlreadyRefreshed
			if out.Err != nil {
				base.Err = fmt.Errorf("%w: %w", ErrAlreadyRefreshed, out.Err)
			}
			return base
		}
		return out
	}
	a := &attempt{done: make(chan struct{})}
	s.tried[source] = a // counted before the read runs, so a panic or a timeout is not retried
	s.mu.Unlock()
	defer close(a.done)
	a.res = s.refresh(ctx, source, have, base, reread)
	return a.res
}

func (s *Session) refresh(ctx context.Context, source string, have Item, base Result, reread Reread) Result {
	fail := func(err error) Result {
		base.Outcome, base.Item, base.Err = OutcomeUnavailable, Item{}, err
		return base
	}
	rctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	got, err := reread(rctx)
	if err != nil {
		return fail(fmt.Errorf("%w: %w", ErrRefreshFailed, err))
	}
	now := s.clk.Now()
	if Check(got.ReadAt, now, base.Limit) != Fresh {
		return fail(ErrRefreshStale)
	}
	ev := RefreshEvent{Source: source, PriorEvidenceID: have.EvidenceID, EvidenceID: got.EvidenceID, Limit: base.Limit, LimitSetBy: base.LimitSetBy, RefreshedAt: now}
	if err := s.sink.Refreshed(rctx, ev); err != nil {
		return fail(fmt.Errorf("%w: %w", ErrRecord, err))
	}
	base.Outcome, base.Item = OutcomeRefreshed, got
	return base
}
