package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/clock"
)

// TraceEntry is one fault that fired. Time and durations are left out on purpose: a trace is compared between
// runs, and the same plan and seed must give the same one (REQ-ENV-004).
type TraceEntry struct {
	Seq        int             `json:"seq"`
	Target     string          `json:"target"`
	Operation  string          `json:"operation"`
	CallNo     int64           `json:"call_no"`
	FaultIndex int             `json:"fault_index"`
	Effect     string          `json:"effect"`
	Params     json.RawMessage `json:"params,omitempty"`
	Detail     map[string]any  `json:"detail,omitempty"`
}

type trace struct {
	mu      sync.Mutex
	entries []TraceEntry
}

func (t *trace) add(e TraceEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e.Seq = len(t.entries) + 1
	t.entries = append(t.entries, e)
}

func (t *trace) snapshot() []TraceEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]TraceEntry{}, t.entries...)
}

func (t *trace) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = nil
}

// hashTrace is the digest of the whole trace, for comparing two runs in one call.
func hashTrace(entries []TraceEntry) string {
	b, err := json.Marshal(entries)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type clockHolder struct {
	mu  sync.Mutex
	clk clock.Clock
	sim *clock.Simulated
}

func newClockHolder() *clockHolder { return &clockHolder{clk: clock.System{}} }

func (c *clockHolder) now() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clk.Now(), c.clk.Simulated()
}

func (c *clockHolder) setSimulated(start time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sim = clock.NewSimulated(start)
	c.clk = c.sim
}

func (c *clockHolder) setSystem() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sim, c.clk = nil, clock.System{}
}

func (c *clockHolder) advance(d time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sim == nil {
		return false
	}
	return c.sim.Advance(d) == nil
}

// cached is an upstream response as it came, before any rewriting or fault.
type cached struct {
	at     time.Time
	status int
	header map[string][]string
	body   []byte
}

const cacheDepth = 16

// stale remembers what each GET returned, so STALE_READ can serve the state as it was.
type stale struct {
	mu      sync.Mutex
	entries map[string][]cached
}

func newStale() *stale { return &stale{entries: map[string][]cached{}} }

func (s *stale) put(key string, c cached) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := append(s.entries[key], c)
	if len(list) > cacheDepth {
		list = list[len(list)-cacheDepth:]
	}
	s.entries[key] = list
}

// atLeastAsOld returns the newest response recorded at or before cutoff.
func (s *stale) atLeastAsOld(key string, cutoff time.Time) (cached, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.entries[key]
	for i := len(list) - 1; i >= 0; i-- {
		if !list[i].at.After(cutoff) {
			return list[i], true
		}
	}
	return cached{}, false
}

func (s *stale) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string][]cached{}
}

// held are events that LATE_EVENT withheld. They go out in a later response of the same feed, out of order.
type held struct {
	mu     sync.Mutex
	events map[string][]heldEvent
}

type heldEvent struct {
	release time.Time
	event   any
}

func newHeld() *held { return &held{events: map[string][]heldEvent{}} }

func (h *held) hold(key string, release time.Time, events []any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range events {
		h.events[key] = append(h.events[key], heldEvent{release: release, event: e})
	}
}

// due removes and returns the events whose time has come, in the order they were held.
func (h *held) due(key string, now time.Time) []any {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []any
	var rest []heldEvent
	for _, e := range h.events[key] {
		if e.release.After(now) {
			rest = append(rest, e)
		} else {
			out = append(out, e.event)
		}
	}
	h.events[key] = rest
	return out
}

func (h *held) pending() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, l := range h.events {
		n += len(l)
	}
	return n
}

func (h *held) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = map[string][]heldEvent{}
}
