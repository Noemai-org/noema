// Package clock is the only time source in mock-case. Every clock it exposes is SYNTHETIC (REQ-ENV-006):
// either the host wall clock, or a simulated clock a fault plan starts and a benchmark run advances (REQ-ENV-003).
package clock

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// Clock reports the time stamped on case, complaint and evidence records.
type Clock interface {
	Now() time.Time
	// Simulated reports whether the clock is driven by a fault plan rather than by the wall.
	Simulated() bool
}

// System is the wall clock, in UTC.
type System struct{}

func (System) Now() time.Time  { return time.Now().UTC() }
func (System) Simulated() bool { return false }

// Simulated starts at a fixed instant and moves only when told to.
type Simulated struct {
	mu  sync.Mutex
	now time.Time
}

// NewSimulated returns a simulated clock at start (converted to UTC).
func NewSimulated(start time.Time) *Simulated { return &Simulated{now: start.UTC()} }

func (s *Simulated) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

func (s *Simulated) Simulated() bool { return true }

// Advance moves the clock forward. A negative duration is refused: simulated time never runs backwards.
func (s *Simulated) Advance(by time.Duration) error {
	if by < 0 {
		return errors.New("simulated time cannot move backwards")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = s.now.Add(by)
	return nil
}

var isoDuration = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// ParseDuration reads an ISO 8601 duration made of days, hours, minutes and seconds ("PT31S", "P1DT2H").
// Years, months and weeks are refused: their length depends on a calendar.
func ParseDuration(s string) (time.Duration, error) {
	m := isoDuration.FindStringSubmatch(s)
	if m == nil || s == "P" || s[len(s)-1] == 'T' {
		return 0, fmt.Errorf("%q is not a duration of days, hours, minutes and seconds (for example PT31S)", s)
	}
	var total time.Duration
	for i, unit := range []time.Duration{24 * time.Hour, time.Hour, time.Minute, time.Second} {
		if m[i+1] == "" {
			continue
		}
		n, err := strconv.ParseInt(m[i+1], 10, 32)
		if err != nil {
			return 0, fmt.Errorf("%q: %w", s, err)
		}
		total += time.Duration(n) * unit
	}
	return total, nil
}
