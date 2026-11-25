package freshness_test

// EVD-02 (REQ-CTX-003, INV-23, INV-16, INV-21): effective freshness is the stricter of the source's limit, the trust profile's limit and a
// per-source override, measured from read_at on the injected clock; a stale source is re-read at most once per evaluation; a failed or still-stale
// refresh makes the source UNAVAILABLE and never reuses stale evidence. All times are SYNTHETIC.

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/freshness"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

const (
	bySource   = contracts.EnumsV1LimitSetBySOURCE
	byProfile  = contracts.EnumsV1LimitSetByTRUSTPROFILE
	byOverride = contracts.EnumsV1LimitSetByPROFILEOVERRIDE
	limit      = 5 * time.Minute
)

var t0 = time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC) // SYNTHETIC

func TestEffectiveIsTheStrictestOfSourceProfileAndOverride(t *testing.T) {
	cases := []struct {
		name string
		in   freshness.Limits
		want time.Duration
		by   contracts.EnumsV1LimitSetBy
	}{
		{"source alone", freshness.Limits{Source: 5 * time.Minute}, 5 * time.Minute, bySource},
		{"a tighter profile wins", freshness.Limits{Source: 5 * time.Minute, Profile: 2 * time.Minute}, 2 * time.Minute, byProfile},
		{"a looser profile never loosens the source", freshness.Limits{Source: 5 * time.Minute, Profile: 9 * time.Minute}, 5 * time.Minute, bySource},
		{"a tighter override wins over both", freshness.Limits{Source: 5 * time.Minute, Profile: 2 * time.Minute, Override: time.Minute}, time.Minute, byOverride},
		{"a looser override never loosens", freshness.Limits{Source: 5 * time.Minute, Profile: 2 * time.Minute, Override: 8 * time.Minute}, 2 * time.Minute, byProfile},
		{"an override alone tightens the source", freshness.Limits{Source: 5 * time.Minute, Override: 3 * time.Minute}, 3 * time.Minute, byOverride},
		{"a tie goes to the source", freshness.Limits{Source: 5 * time.Minute, Profile: 5 * time.Minute, Override: 5 * time.Minute}, 5 * time.Minute, bySource},
		{"a tie of profile and override goes to the profile", freshness.Limits{Source: 9 * time.Minute, Profile: 5 * time.Minute, Override: 5 * time.Minute}, 5 * time.Minute, byProfile},
		{"a zero profile and a zero override set none", freshness.Limits{Source: 5 * time.Minute, Profile: 0, Override: 0}, 5 * time.Minute, bySource},
	}
	for _, c := range cases {
		got, by, err := freshness.Effective(c.in)
		if err != nil || got != c.want || by != c.by {
			t.Errorf("%s: Effective(%+v) = %v, %v, %v; want %v, %v", c.name, c.in, got, by, err, c.want, c.by)
		}
	}
}

func TestNoUsableLimitIsRefusedNeverDefaulted(t *testing.T) {
	for _, in := range []freshness.Limits{
		{},
		{Source: 0, Profile: time.Minute},
		{Source: -time.Second},
		{Source: time.Minute, Profile: -time.Second},
		{Source: time.Minute, Override: -time.Second},
	} {
		if d, by, err := freshness.Effective(in); !errors.Is(err, freshness.ErrNoLimit) || d != 0 || by != "" {
			t.Errorf("Effective(%+v) = %v, %q, %v; want ErrNoLimit and no limit", in, d, by, err)
		}
	}
}

// The age is measured from read_at, and the boundary is PERD 21.1's "older than its effective limit": exactly at the limit is fresh, one tick over is stale.
func TestBoundaryAtLimitMinusAtAndPlusOne(t *testing.T) {
	read := t0
	cases := []struct {
		name string
		now  time.Time
		want freshness.Verdict
	}{
		{"limit minus one millisecond", read.Add(limit - time.Millisecond), freshness.Fresh},
		{"exactly the limit", read.Add(limit), freshness.Fresh},
		{"limit plus one millisecond", read.Add(limit + time.Millisecond), freshness.Stale},
		{"limit plus one nanosecond", read.Add(limit + time.Nanosecond), freshness.Stale},
		{"the instant of the read", read, freshness.Fresh},
		{"a read from the future", read.Add(-time.Nanosecond), freshness.Unusable},
	}
	for _, c := range cases {
		if got := freshness.Check(read, c.now, limit); got != c.want {
			t.Errorf("%s: Check = %v, want %v", c.name, got, c.want)
		}
	}
	if got := freshness.Check(time.Time{}, t0, limit); got != freshness.Unusable {
		t.Errorf("a zero read_at is %v, want Unusable", got)
	}
	for _, l := range []time.Duration{0, -time.Second} {
		if got := freshness.Check(t0, t0, l); got != freshness.Unusable {
			t.Errorf("limit %v is %v, want Unusable", l, got)
		}
	}
}

// REQ-CTX-003 test: a property test with a generated clock skew. Fresh exactly when 0 <= age <= limit, over 50,000 seeded cases.
func TestPropertyGeneratedClockSkew(t *testing.T) {
	rng := rand.New(rand.NewSource(20261124))
	for i := 0; i < 50000; i++ {
		l := time.Duration(1+rng.Intn(86_400_000)) * time.Millisecond
		skew := time.Duration(rng.Int63n(int64(3*l))) - l/3 // from a third of the limit before the read to just under twice the limit after it
		got := freshness.Check(t0, t0.Add(skew), l)
		var want freshness.Verdict
		switch {
		case skew < 0:
			want = freshness.Unusable
		case skew > l:
			want = freshness.Stale
		default:
			want = freshness.Fresh
		}
		if got != want {
			t.Fatalf("seed case %d: limit %v skew %v: Check = %v, want %v", i, l, skew, got, want)
		}
		s, p, o := l, time.Duration(rng.Int63n(int64(2*l))), time.Duration(rng.Int63n(int64(2*l)))
		e, _, err := freshness.Effective(freshness.Limits{Source: s, Profile: p, Override: o})
		if err != nil || e > s || (p > 0 && e > p) || (o > 0 && e > o) {
			t.Fatalf("seed case %d: Effective(%v, %v, %v) = %v, %v: it must never exceed a positive input", i, s, p, o, e, err)
		}
	}
}

type sink struct {
	mu     sync.Mutex
	events []freshness.RefreshEvent
	err    error
}

func (s *sink) Refreshed(_ context.Context, e freshness.RefreshEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, e)
	return nil
}

func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

type rig struct {
	clk  *clock.Manual
	sink *sink
	sess *freshness.Session
}

func newRig(t *testing.T) *rig {
	t.Helper()
	clk := clock.NewManual(t0)
	sk := &sink{}
	s, err := freshness.NewSession(clk, sk, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return &rig{clk: clk, sink: sk, sess: s}
}

var lim = freshness.Limits{Source: limit, Profile: 2 * time.Minute}

func TestAFreshItemIsUsedWithoutARefresh(t *testing.T) {
	r := newRig(t)
	calls := 0
	have := freshness.Item{EvidenceID: "evr_old", ReadAt: t0}
	r.clk.Advance(2 * time.Minute) // exactly the effective limit (the profile's 2 minutes)
	res := r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, func(context.Context) (freshness.Item, error) { calls++; return freshness.Item{}, nil })
	if res.Outcome != freshness.OutcomeFresh || res.Item != have || res.Err != nil || calls != 0 || r.sink.count() != 0 {
		t.Fatalf("fresh: %+v calls=%d events=%d", res, calls, r.sink.count())
	}
	if res.Limit != 2*time.Minute || res.LimitSetBy != byProfile {
		t.Errorf("the limit that applied was %v by %v, want 2m by TRUST_PROFILE (it is what the Decision Record's evidence_freshness records)", res.Limit, res.LimitSetBy)
	}
}

func TestAStaleItemIsReReadOnceAndTheRefreshIsRecorded(t *testing.T) {
	r := newRig(t)
	have := freshness.Item{EvidenceID: "evr_old", ReadAt: t0}
	r.clk.Advance(2*time.Minute + time.Millisecond)
	calls := 0
	res := r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, func(context.Context) (freshness.Item, error) {
		calls++
		return freshness.Item{EvidenceID: "evr_new", ReadAt: r.clk.Now()}, nil
	})
	if res.Outcome != freshness.OutcomeRefreshed || res.Err != nil || res.Item.EvidenceID != "evr_new" || calls != 1 {
		t.Fatalf("refresh: %+v calls=%d", res, calls)
	}
	if r.sink.count() != 1 {
		t.Fatalf("EVIDENCE_REFRESHED events = %d, want exactly 1", r.sink.count())
	}
	e := r.sink.events[0]
	if e.Source != "fin.src.ledger" || e.PriorEvidenceID != "evr_old" || e.EvidenceID != "evr_new" || e.Limit != 2*time.Minute || e.LimitSetBy != byProfile || !e.RefreshedAt.Equal(r.clk.Now()) {
		t.Errorf("event %+v does not name the source, both items, the limit that applied and the clock's instant", e)
	}
}

func TestAFailedRefreshMakesTheSourceUnavailableAndNeverReusesTheStaleItem(t *testing.T) {
	r := newRig(t)
	have := freshness.Item{EvidenceID: "evr_old", ReadAt: t0}
	r.clk.Advance(time.Hour)
	res := r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, func(context.Context) (freshness.Item, error) { return freshness.Item{}, errors.New("source down") })
	if res.Outcome != freshness.OutcomeUnavailable || !errors.Is(res.Err, freshness.ErrRefreshFailed) || res.Item != (freshness.Item{}) {
		t.Fatalf("a failed refresh: %+v; want UNAVAILABLE, ErrRefreshFailed and no item (the stale one is never returned)", res)
	}
	if r.sink.count() != 0 {
		t.Errorf("a failed refresh recorded %d EVIDENCE_REFRESHED events, want 0", r.sink.count())
	}
}

func TestARefreshThatIsStillStaleMakesTheSourceUnavailable(t *testing.T) {
	r := newRig(t)
	have := freshness.Item{EvidenceID: "evr_old", ReadAt: t0}
	r.clk.Advance(time.Hour)
	res := r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, func(context.Context) (freshness.Item, error) {
		return freshness.Item{EvidenceID: "evr_new", ReadAt: t0.Add(time.Minute)}, nil // the source answered with an old read
	})
	if res.Outcome != freshness.OutcomeUnavailable || !errors.Is(res.Err, freshness.ErrRefreshStale) || res.Item != (freshness.Item{}) || r.sink.count() != 0 {
		t.Fatalf("a stale re-read: %+v events=%d", res, r.sink.count())
	}
}

func TestASecondStaleResultForTheSameSourceIsNotRefreshedAgain(t *testing.T) {
	r := newRig(t)
	have := freshness.Item{EvidenceID: "evr_old", ReadAt: t0}
	r.clk.Advance(time.Hour)
	calls := 0
	reread := func(context.Context) (freshness.Item, error) { calls++; return freshness.Item{}, errors.New("down") }
	_ = r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, reread)
	res := r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, reread)
	if calls != 1 || res.Outcome != freshness.OutcomeUnavailable || !errors.Is(res.Err, freshness.ErrAlreadyRefreshed) {
		t.Fatalf("second ask: calls=%d %+v; want one re-read in all and ErrAlreadyRefreshed", calls, res)
	}
	other := r.sess.Ensure(context.Background(), "fin.src.case", lim, have, func(context.Context) (freshness.Item, error) {
		calls++
		return freshness.Item{EvidenceID: "evr_c", ReadAt: r.clk.Now()}, nil
	})
	if calls != 2 || other.Outcome != freshness.OutcomeRefreshed {
		t.Errorf("another source must have its own one refresh: calls=%d %+v", calls, other)
	}
}

func TestARefreshIsCountedBeforeItRunsSoAPanicOrATimeoutIsNotRetried(t *testing.T) {
	r := newRig(t)
	have := freshness.Item{EvidenceID: "evr_old", ReadAt: t0}
	r.clk.Advance(time.Hour)
	var calls int32
	slow := func(ctx context.Context) (freshness.Item, error) {
		atomic.AddInt32(&calls, 1)
		<-ctx.Done() // blocks until the session's deadline
		return freshness.Item{}, ctx.Err()
	}
	res := r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, slow)
	if res.Outcome != freshness.OutcomeUnavailable || !errors.Is(res.Err, freshness.ErrRefreshFailed) {
		t.Fatalf("a re-read past the deadline: %+v", res)
	}
	_ = r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, slow)
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("re-reads = %d, want 1: a timed-out refresh still counts", calls)
	}
}

// Record first (PERD 32.4): a refresh that cannot be recorded is not used.
func TestARefreshThatCannotBeRecordedIsNotUsed(t *testing.T) {
	r := newRig(t)
	r.sink.err = errors.New("append failed")
	have := freshness.Item{EvidenceID: "evr_old", ReadAt: t0}
	r.clk.Advance(time.Hour)
	res := r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, func(context.Context) (freshness.Item, error) {
		return freshness.Item{EvidenceID: "evr_new", ReadAt: r.clk.Now()}, nil
	})
	if res.Outcome != freshness.OutcomeUnavailable || !errors.Is(res.Err, freshness.ErrRecord) || res.Item != (freshness.Item{}) {
		t.Fatalf("an unrecorded refresh: %+v; want UNAVAILABLE, ErrRecord and no item", res)
	}
}

func TestNoUsableLimitOrAnUnusableReadAtIsUnavailableWithoutARead(t *testing.T) {
	r := newRig(t)
	calls := 0
	reread := func(context.Context) (freshness.Item, error) { calls++; return freshness.Item{}, nil }
	res := r.sess.Ensure(context.Background(), "fin.src.ledger", freshness.Limits{}, freshness.Item{EvidenceID: "e", ReadAt: t0}, reread)
	if res.Outcome != freshness.OutcomeUnavailable || !errors.Is(res.Err, freshness.ErrNoLimit) || calls != 0 {
		t.Errorf("no limit: %+v calls=%d", res, calls)
	}
	res = r.sess.Ensure(context.Background(), "fin.src.case", lim, freshness.Item{EvidenceID: "e", ReadAt: t0.Add(time.Hour)}, reread) // a read from the future
	if res.Outcome != freshness.OutcomeUnavailable || !errors.Is(res.Err, freshness.ErrUnusableReadAt) || calls != 0 {
		t.Errorf("a read from the future: %+v calls=%d", res, calls)
	}
}

func TestConcurrentAsksForOneSourceRefreshItOnce(t *testing.T) {
	r := newRig(t)
	have := freshness.Item{EvidenceID: "evr_old", ReadAt: t0}
	r.clk.Advance(time.Hour)
	var calls int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, func(context.Context) (freshness.Item, error) {
				atomic.AddInt32(&calls, 1)
				return freshness.Item{EvidenceID: "evr_new", ReadAt: r.clk.Now()}, nil
			})
		}()
	}
	wg.Wait()
	if calls != 1 || r.sink.count() != 1 {
		t.Errorf("50 concurrent asks: re-reads=%d events=%d, want 1 and 1", calls, r.sink.count())
	}
}

func TestNewSessionRefusesAnUnsafeConfiguration(t *testing.T) {
	clk := clock.NewManual(t0)
	if _, err := freshness.NewSession(nil, &sink{}, time.Second); err == nil {
		t.Error("a nil clock was accepted")
	}
	if _, err := freshness.NewSession(clk, nil, time.Second); err == nil {
		t.Error("a nil sink was accepted")
	}
	for _, d := range []time.Duration{0, -time.Second} {
		if _, err := freshness.NewSession(clk, &sink{}, d); err == nil {
			t.Errorf("a refresh timeout of %v was accepted (a context deadline on every call)", d)
		}
	}
}

// Codex on 78956901b (INV-16): an answer taken from a source's earlier attempt is checked against the CURRENT clock and the CURRENT effective limit before
// it is returned. One refresh per source never authorises the reuse of an item that has become stale since.
func refreshOnce(t *testing.T, r *rig, calls *int) freshness.Result {
	t.Helper()
	r.clk.Advance(time.Hour) // the first item is stale
	return r.sess.Ensure(context.Background(), "fin.src.ledger", lim, freshness.Item{EvidenceID: "evr_old", ReadAt: t0}, func(context.Context) (freshness.Item, error) {
		*calls++
		return freshness.Item{EvidenceID: "evr_new", ReadAt: r.clk.Now()}, nil
	})
}

func again(r *rig, l freshness.Limits, calls *int) freshness.Result {
	return r.sess.Ensure(context.Background(), "fin.src.ledger", l, freshness.Item{EvidenceID: "evr_old", ReadAt: t0}, func(context.Context) (freshness.Item, error) {
		*calls++
		return freshness.Item{EvidenceID: "evr_other", ReadAt: r.clk.Now()}, nil
	})
}

func TestARefreshedItemIsNotReturnedOnceItHasBecomeStale(t *testing.T) {
	r := newRig(t)
	calls := 0
	if first := refreshOnce(t, r, &calls); first.Outcome != freshness.OutcomeRefreshed {
		t.Fatalf("setup: %+v", first)
	}
	r.clk.Advance(2*time.Minute + time.Nanosecond) // one tick past the effective limit (the profile's 2 minutes)
	res := again(r, lim, &calls)
	if res.Outcome != freshness.OutcomeUnavailable || res.Item != (freshness.Item{}) || calls != 1 {
		t.Fatalf("a refreshed item past its limit: %+v calls=%d; want UNAVAILABLE, no item, and still one re-read", res, calls)
	}
	if !errors.Is(res.Err, freshness.ErrAlreadyRefreshed) || !errors.Is(res.Err, freshness.ErrRefreshStale) {
		t.Errorf("cause %v: want ErrAlreadyRefreshed and ErrRefreshStale", res.Err)
	}
}

func TestARefreshedItemIsStillUsableExactlyAtTheLimitAndNotOneTickAfter(t *testing.T) {
	r := newRig(t)
	calls := 0
	_ = refreshOnce(t, r, &calls)
	r.clk.Advance(2 * time.Minute) // exactly the limit: fresh
	at := again(r, lim, &calls)
	if at.Outcome != freshness.OutcomeRefreshed || at.Item.EvidenceID != "evr_new" || calls != 1 {
		t.Fatalf("exactly at the limit: %+v calls=%d; want the refreshed item and one re-read", at, calls)
	}
	r.clk.Advance(time.Nanosecond)
	past := again(r, lim, &calls)
	if past.Outcome != freshness.OutcomeUnavailable || past.Item != (freshness.Item{}) || calls != 1 {
		t.Fatalf("one tick past the limit: %+v calls=%d; want UNAVAILABLE with no item", past, calls)
	}
}

func TestACachedRefreshIsCheckedAgainstTheLimitOfTheCurrentAsk(t *testing.T) {
	r := newRig(t)
	calls := 0
	_ = refreshOnce(t, r, &calls)
	r.clk.Advance(90 * time.Second)
	tighter := freshness.Limits{Source: limit, Profile: time.Minute}
	if res := again(r, tighter, &calls); res.Outcome != freshness.OutcomeUnavailable || res.Item != (freshness.Item{}) || calls != 1 {
		t.Fatalf("a tighter limit on the second ask: %+v calls=%d; want UNAVAILABLE and one re-read", res, calls)
	}
	if res := again(r, lim, &calls); res.Outcome != freshness.OutcomeRefreshed || res.Limit != 2*time.Minute || calls != 1 {
		t.Errorf("the original limit still holds the item fresh at 90 s: %+v calls=%d", res, calls)
	}
}

func TestAFreshItemIsCheckedAgainstTheCurrentClockOnEveryAsk(t *testing.T) {
	r := newRig(t)
	calls := 0
	have := freshness.Item{EvidenceID: "evr_old", ReadAt: t0}
	reread := func(context.Context) (freshness.Item, error) {
		calls++
		return freshness.Item{EvidenceID: "evr_new", ReadAt: r.clk.Now()}, nil
	}
	if res := r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, reread); res.Outcome != freshness.OutcomeFresh || calls != 0 {
		t.Fatalf("first ask: %+v calls=%d", res, calls)
	}
	r.clk.Advance(2*time.Minute + time.Nanosecond)
	res := r.sess.Ensure(context.Background(), "fin.src.ledger", lim, have, reread)
	if res.Outcome != freshness.OutcomeRefreshed || res.Item.EvidenceID != "evr_new" || calls != 1 {
		t.Fatalf("second ask after expiry: %+v calls=%d; want one refresh now (the first ask refreshed nothing)", res, calls)
	}
}

func TestACachedUnavailableStaysUnavailableWhateverTheClockDoes(t *testing.T) {
	r := newRig(t)
	calls := 0
	r.clk.Advance(time.Hour)
	reread := func(context.Context) (freshness.Item, error) { calls++; return freshness.Item{}, errors.New("down") }
	_ = r.sess.Ensure(context.Background(), "fin.src.ledger", lim, freshness.Item{EvidenceID: "evr_old", ReadAt: t0}, reread)
	for _, step := range []time.Duration{0, time.Minute, time.Hour} {
		r.clk.Advance(step)
		res := r.sess.Ensure(context.Background(), "fin.src.ledger", lim, freshness.Item{EvidenceID: "evr_old", ReadAt: t0}, reread)
		if res.Outcome != freshness.OutcomeUnavailable || res.Item != (freshness.Item{}) || calls != 1 {
			t.Fatalf("step %v: %+v calls=%d", step, res, calls)
		}
	}
}
