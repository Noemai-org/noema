package clock

import (
	"sync"
	"time"
)

// Clock is the only way Noema code reads the time (INV-21).
type Clock interface {
	Now() time.Time
}

// System is the real clock. It is the one place that calls time.Now.
type System struct{}

// Now returns the current instant in UTC.
func (System) Now() time.Time { return time.Now().UTC() }

// Manual is the simulated clock: it moves only when told to. Every simulated run labels its clock SYNTHETIC.
type Manual struct {
	mu  sync.Mutex
	now time.Time
}

// NewManual starts a simulated clock at the given instant.
func NewManual(start time.Time) *Manual { return &Manual{now: start.UTC()} }

// Now returns the simulated instant.
func (m *Manual) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

// Advance moves the simulated clock forward by d. A negative step is ignored: time never runs backwards.
func (m *Manual) Advance(d time.Duration) {
	if d <= 0 {
		return
	}
	m.mu.Lock()
	m.now = m.now.Add(d)
	m.mu.Unlock()
}
