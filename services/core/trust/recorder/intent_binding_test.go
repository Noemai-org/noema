package recorder_test

// A542 (interim, relayed, not yet committed; REQ-TRU-010, INV-25): a disposition belongs to the same intent as the action decision that follows it. An ESCALATE decision of the same tenant
// for another intent, or for the same intent with another intent hash, is not this decision's disposition: ErrDispositionNotValid, and nothing is written. A disposition of the same intent
// and the same hash is accepted. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

func TestADispositionOfTheSameIntentAndHashIsAccepted(t *testing.T) {
	w := escalationWorld(t)
	disp := w.disposition(t, 1)
	res, err := w.writeAndCommit(t, w.action(t, decide.Allow, disp, capability))
	if err != nil || len(res.Grants) != 1 {
		t.Fatalf("%+v %v; want an ALLOW with one grant", res, err)
	}
}

// An ESCALATE of the tenant for ANOTHER intent is refused, though it exists, is the tenant's and is an ESCALATE.
func TestADispositionOfAnotherIntentOfTheSameTenantIsRefused(t *testing.T) {
	w := escalationWorld(t)
	disp := w.disposition(t, 1)
	r := w.action(t, decide.Allow, disp, capability)
	r.IntentID = "int_01OTHER"
	before := w.counts(t)
	if _, err := w.writeAndCommit(t, r); !errors.Is(err, recorder.ErrDispositionNotValid) {
		t.Fatalf("a disposition of another intent: %v, want ErrDispositionNotValid", err)
	}
	if after := w.counts(t); after != before {
		t.Errorf("the refusal changed the rows from %+v to %+v", before, after)
	}
}

// The same intent with another intent hash (another version of its content) is another decision's disposition, not this one's.
func TestADispositionWithAnotherIntentHashIsRefused(t *testing.T) {
	w := escalationWorld(t)
	disp := w.disposition(t, 1)
	r := w.action(t, decide.Allow, disp, capability)
	r.IntentHash = h2(4242)
	before := w.counts(t)
	if _, err := w.writeAndCommit(t, r); !errors.Is(err, recorder.ErrDispositionNotValid) {
		t.Fatalf("a disposition with another intent hash: %v, want ErrDispositionNotValid", err)
	}
	if after := w.counts(t); after != before {
		t.Errorf("the refusal changed the rows from %+v to %+v", before, after)
	}
}
