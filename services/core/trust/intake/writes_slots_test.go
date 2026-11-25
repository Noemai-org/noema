package intake_test

// A543 (interim, relayed, not yet committed; REQ-TRU-002, REQ-NFR-002; INV-14): the Writes port takes the resolved slot values. T3 hands it the admitted intent as T1 read it, slot values
// included, and nothing else's: the node's `write_when` is a predicate over those values, so the answer rests on what the stored intent says and never on the request. All data is SYNTHETIC.

import (
	"context"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

type writesRecorder struct {
	*spy
	got []intake.AdmittedIntent
}

func (w *writesRecorder) NeedsWrite(ctx context.Context, a intake.AdmittedIntent) (bool, error) {
	w.got = append(w.got, a)
	return w.spy.NeedsWrite(ctx, a)
}

func TestT3HandsTheWritesPortTheIntentWithItsResolvedSlotValues(t *testing.T) {
	s := newSpy()
	s.intent.Slots = map[string]string{"question_type": "ESCALATION_REQUEST", "requested_remedy": "fin.cap.lookup_transaction"}
	w := &writesRecorder{spy: s}
	svc := s.service()
	svc.Ports.Writes = w
	if _, err := svc.Evaluate(ctx(t), request()); err != nil {
		t.Fatal(err)
	}
	if len(w.got) != 1 {
		t.Fatalf("NeedsWrite was called %d times, want once", len(w.got))
	}
	if want := (map[string]string{"question_type": "ESCALATION_REQUEST", "requested_remedy": "fin.cap.lookup_transaction"}); !reflect.DeepEqual(w.got[0].Slots, want) {
		t.Errorf("the port got slots %v, want the stored intent's %v", w.got[0].Slots, want)
	}
	if w.got[0].IntentNode != s.intent.IntentNode || w.got[0].IntentHash != s.intent.IntentHash || w.got[0].IntentID != s.intent.IntentID {
		t.Errorf("the port got another intent than the one T1 read: %+v", w.got[0])
	}
}

// An intent with no resolved slot is handed over with none (a nil map): the rule reads every slot as null, never as a value the request supplied.
func TestT3HandsAnIntentWithNoResolvedSlotAsIs(t *testing.T) {
	s := newSpy()
	w := &writesRecorder{spy: s}
	svc := s.service()
	svc.Ports.Writes = w
	if _, err := svc.Evaluate(ctx(t), request()); err != nil {
		t.Fatal(err)
	}
	if len(w.got) != 1 || len(w.got[0].Slots) != 0 {
		t.Errorf("the port got %d calls with slots %v, want one call and none", len(w.got), w.got)
	}
}
