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
