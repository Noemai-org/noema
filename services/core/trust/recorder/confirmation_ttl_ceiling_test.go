package recorder_test

// A561 and A724 (interim, relayed; REQ-TRU-009; INV-25, INV-03): the Writer refuses a confirmation request whose time to live is above the pack's ceiling, and writes none when no ceiling is
// configured. The ceiling is the Writer's own (a missing ceiling must never read as no ceiling). All data is SYNTHETIC.

import (
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

func writeWithTTL(t *testing.T, ceiling time.Duration, ttlSeconds int64) (*world, error) {
	t.Helper()
	w := newWorld(t)
	w.w.ConfirmationTTLCeiling = ceiling
	r := confirmRequest(t, w, "evs_01ABC", true)
	r.ConfirmationRequest.ExpiresAt = epoch.Unix() + ttlSeconds
	_, err := w.write(t, r, true)
	return w, err
}

func TestARequestAboveTheCeilingIsRefusedAndNothingIsWritten(t *testing.T) {
	w, err := writeWithTTL(t, 10*time.Minute, 601)
	if !errors.Is(err, recorder.ErrRequest) {
		t.Fatalf("a request of 601 s under a 600 s ceiling: %v, want ErrRequest", err)
	}
	w.nothingWritten(t, "a request above the ceiling")
	if n := len(w.requestRows(t)); n != 0 {
		t.Fatalf("%d request rows", n)
	}
}

func TestARequestAtTheCeilingIsWritten(t *testing.T) {
	w, err := writeWithTTL(t, 10*time.Minute, 600)
	if err != nil {
		t.Fatalf("a request of 600 s under a 600 s ceiling: %v", err)
	}
	if n := len(w.requestRows(t)); n != 1 {
		t.Fatalf("%d request rows, want 1", n)
	}
}

func TestWithNoCeilingNoRequestIsWritten(t *testing.T) {
	for _, ceiling := range []time.Duration{0, -time.Second} {
		w, err := writeWithTTL(t, ceiling, 60)
		if !errors.Is(err, recorder.ErrRequest) {
			t.Fatalf("ceiling %v: %v, want ErrRequest (a missing ceiling is not no ceiling)", ceiling, err)
		}
		w.nothingWritten(t, "a CONFIRM with no ceiling")
	}
}

// A758 (interim, relayed): the TTL is compared in whole seconds against the ceiling in whole seconds (ceiling_ms / 1000), never as a time.Duration: an expiry far in the future (year
// 2400) wraps a Duration product to a small or negative value and would pass. The ceiling is clamped to platform/ceilings' 600000 ms, so a Writer configured above it is held to it.
func TestAnExpiryInTheYear2400IsRefusedNotWrapped(t *testing.T) {
	const year2400 = 13569465600 - 1790000000 // seconds from the test clock (epoch is Nov 2026) to 1 Jan 2400
	w := newWorld(t)
	w.w.ConfirmationTTLCeiling = 10 * time.Minute
	r := confirmRequest(t, w, "evs_01ABC", true)
	r.ConfirmationRequest.ExpiresAt = epoch.Unix() + year2400
	if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrRequest) {
		t.Fatalf("an expiry in the year 2400: %v, want ErrRequest", err)
	}
	w.nothingWritten(t, "an expiry in the year 2400")
}

func TestACeilingAboveThePlatformBoundIsClampedToIt(t *testing.T) {
	w, err := writeWithTTL(t, time.Hour, 601)
	if !errors.Is(err, recorder.ErrRequest) {
		t.Fatalf("601 s under a one hour ceiling: %v, want ErrRequest (the ceiling is clamped to 600 s)", err)
	}
	w.nothingWritten(t, "a request above the clamped ceiling")
}

// A ceiling of a fraction of a second rounds down to whole seconds: 1500 ms is 1 s.
func TestTheCeilingIsInWholeSeconds(t *testing.T) {
	if _, err := writeWithTTL(t, 1500*time.Millisecond, 2); !errors.Is(err, recorder.ErrRequest) {
		t.Fatalf("2 s under a 1500 ms ceiling (1 s): %v, want ErrRequest", err)
	}
	if _, err := writeWithTTL(t, 1500*time.Millisecond, 1); err != nil {
		t.Fatalf("1 s under a 1500 ms ceiling (1 s): %v", err)
	}
}
