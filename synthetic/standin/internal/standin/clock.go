package standin

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// The simulated clock (REQ-ENV-004, INV-21). With STANDIN_CLOCK_START set, the service reads time from a clock that starts there and moves only
// when the benchmark advances it, the same way the hostility proxy's clock does. The identity check (maxAssertionLifetime, expiry) then uses the
// same clock as the client that mints the assertion, so a run is reproducible. Without a start the service uses the system clock and cannot be
// advanced. SYNTHETIC (AGENT-25): the clock source is labelled in every answer.

// maxAdvance bounds one step, so a typo cannot move the run a day ahead.
const maxAdvance = 24 * time.Hour

type simClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *simClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *simClock) advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

var timeDuration = regexp.MustCompile(`^PT(?:(\d{1,5})H)?(?:(\d{1,5})M)?(?:(\d{1,7})S)?$`)

var errBadDuration = errors.New("by must be an ISO 8601 time duration such as PT10S, PT5M or PT1H, above zero and at most 24 hours")

// parseAdvance reads PT#H#M#S. Days, weeks, fractions and negative values are refused.
func parseAdvance(s string) (time.Duration, error) {
	m := timeDuration.FindStringSubmatch(s)
	if m == nil || (m[1] == "" && m[2] == "" && m[3] == "") {
		return 0, errBadDuration
	}
	var d time.Duration
	for i, unit := range []time.Duration{time.Hour, time.Minute, time.Second} {
		if m[i+1] == "" {
			continue
		}
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return 0, errBadDuration
		}
		d += time.Duration(n) * unit
	}
	if d <= 0 || d > maxAdvance {
		return 0, errBadDuration
	}
	return d, nil
}

func (s *Server) getClock(w http.ResponseWriter, _ *http.Request) {
	jsonOut(w, 200, map[string]any{"data_source": Label, "synthetic": true, "clock_source": "SYNTHETIC", "simulated": s.sim != nil, "now": s.opt.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Server) advanceClock(w http.ResponseWriter, r *http.Request) {
	if s.sim == nil {
		fail(w, http.StatusConflict, "CLOCK_NOT_SIMULATED", "the clock is the system clock; set STANDIN_CLOCK_START to run on a simulated one")
		return
	}
	var body struct {
		By string `json:"by"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		fail(w, http.StatusBadRequest, "BAD_REQUEST", errBadDuration.Error())
		return
	}
	d, err := parseAdvance(body.By)
	if err != nil {
		fail(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	now := s.sim.advance(d)
	jsonOut(w, 200, map[string]any{"data_source": Label, "synthetic": true, "clock_source": "SYNTHETIC", "simulated": true, "now": now.UTC().Format(time.RFC3339Nano)})
}
