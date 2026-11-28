package clock_test

// DATA-02 / REQ-DATA-002 and REQ-ENV-003: every instant the clock gives is UTC (a tenant zone only decides a local day, A634), the simulated clock never runs backwards, and its arithmetic is
// exact. Property tests; every instant is SYNTHETIC.

import (
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// zones are fixed offsets only: the property holds for any zone a caller could have the start instant in, and needs no tz database.
func instantIn() *rapid.Generator[time.Time] {
	return rapid.Custom(func(t *rapid.T) time.Time {
		seconds := rapid.Int64Range(0, 4_102_444_800).Draw(t, "unix_seconds") // 1970 to 2100
		nanos := rapid.Int64Range(0, 999_999_999).Draw(t, "nanos")
		offset := rapid.IntRange(-14*3600, 14*3600).Draw(t, "offset_seconds")
		return time.Unix(seconds, nanos).In(time.FixedZone("synthetic", offset))
	})
}

func TestTheSimulatedClockGivesUTCWhateverZoneItStartedIn(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		start := instantIn().Draw(t, "start")
		m := clock.NewManual(start)
		if m.Now().Location() != time.UTC {
			t.Fatalf("Now() is in %v, not UTC", m.Now().Location())
		}
		if !m.Now().Equal(start) {
			t.Fatalf("starting in another zone moved the instant: %v became %v", start, m.Now())
		}
	})
}

func TestTheSimulatedClockAddsExactlyAndNeverRunsBackwards(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		start := instantIn().Draw(t, "start")
		m := clock.NewManual(start)
		want := start.UTC()
		for _, step := range rapid.SliceOfN(rapid.Int64Range(-int64(48*time.Hour), int64(48*time.Hour)), 0, 30).Draw(t, "steps") {
			before := m.Now()
			m.Advance(time.Duration(step))
			if step > 0 {
				want = want.Add(time.Duration(step))
			}
			if !m.Now().Equal(want) {
				t.Fatalf("after a step of %d ns the clock is %v, want %v", step, m.Now(), want)
			}
			if m.Now().Before(before) {
				t.Fatalf("the clock ran backwards: %v then %v", before, m.Now())
			}
			if m.Now().Location() != time.UTC {
				t.Fatalf("Now() left UTC after Advance: %v", m.Now().Location())
			}
		}
	})
}

func TestTheSystemClockGivesUTC(t *testing.T) {
	if got := (clock.System{}).Now().Location(); got != time.UTC {
		t.Fatalf("System.Now() is in %v, not UTC", got)
	}
}
