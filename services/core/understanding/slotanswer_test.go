package understanding_test

// A563 and A743 (interim, relayed; AMB-03; REQ-AMB-001; INV-02, INV-12; AGENT-09): a SLOT answer joins the intent as v(N+1) with `supersedes`, built from the stored intent by the core (never by
// the model), and the gate re-runs over it. The answer fills a slot the intent lacked and never overwrites one; the gate still judges the slot's name and type. All data is SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/understanding"
)

var whenAnswer = json.RawMessage(`{"type":"TimeRef","surface_form":"last friday","window":["2026-09-25T00:00:00Z","2026-09-25T23:59:59Z"]}`)

var answeredAt = time.Date(2026, 10, 1, 9, 50, 0, 0, time.UTC)

// prevWithoutWhen is an admitted v1 whose `when` slot the customer was asked for.
func prevWithoutWhen(t *testing.T) (*understanding.Gate, understanding.AdmittedIntent) {
	t.Helper()
	g := gate(t)
	adm, err := g.Admit(raw(t, func(m map[string]any) {
		delete(m["slots"].(map[string]any), "when")
		m["ambiguity_flags"] = []any{"MISSING_WHEN"}
	}), session, corr)
	if err != nil {
		t.Fatal(err)
	}
	return g, adm.Intent
}

func TestAnAnsweredSlotJoinsTheIntentAsTheNextVersionAndTheGateAdmitsIt(t *testing.T) {
	g, prev := prevWithoutWhen(t)
	doc, err := understanding.SupersedeWithSlot(prev, "when", whenAnswer, answeredAt)
	if err != nil {
		t.Fatalf("SupersedeWithSlot: %v", err)
	}
	adm, err := g.Admit(doc, session, corr)
	if err != nil {
		t.Fatalf("the gate refused the superseding version: %v", err)
	}
	o, p := adm.Intent.Object(), prev.Object()
	if o.Version != p.Version+1 || o.Supersedes == nil || string(*o.Supersedes) != string(p.IntentId)+"@1" || o.IntentId != p.IntentId {
		t.Fatalf("version %d supersedes %v id %s, want version 2 superseding %s@1 of the same intent", o.Version, o.Supersedes, o.IntentId, p.IntentId)
	}
	if _, ok := o.Slots["when"]; !ok || len(o.Slots) != len(p.Slots)+1 {
		t.Errorf("slots %v, want the previous slots and `when`", o.Slots)
	}
	if string(o.CreatedAt) != "2026-10-01T09:50:00Z" || adm.Intent.Hash() == prev.Hash() {
		t.Errorf("created_at %s, hash equal to the previous: %v", o.CreatedAt, adm.Intent.Hash() == prev.Hash())
	}
	if o.Principal.PrincipalId != p.Principal.PrincipalId || o.CorrelationId != p.CorrelationId {
		t.Errorf("the principal and the correlation are the session's and the investigation's, not the answer's")
	}
}

func TestAnAnswerNeverOverwritesASlotTheIntentHolds(t *testing.T) {
	_, prev := prevWithoutWhen(t)
	if _, err := understanding.SupersedeWithSlot(prev, "amount", json.RawMessage(`{"type":"Quantity","value":1,"unit":"NGN"}`), answeredAt); !errors.Is(err, understanding.ErrSlotAlreadySet) {
		t.Fatalf("got %v, want ErrSlotAlreadySet", err)
	}
}

func TestTheGateStillJudgesTheNameAndTheTypeOfTheAnsweredSlot(t *testing.T) {
	g, prev := prevWithoutWhen(t)
	doc, err := understanding.SupersedeWithSlot(prev, "not_a_slot", whenAnswer, answeredAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Admit(doc, session, corr); !has(reasons(t, err), understanding.ReasonUnknownSlot) {
		t.Errorf("an unknown slot: %v, want UNKNOWN_SLOT", err)
	}
	doc, err = understanding.SupersedeWithSlot(prev, "when", json.RawMessage(`{"type":"Quantity","value":1,"unit":"NGN"}`), answeredAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Admit(doc, session, corr); err == nil {
		t.Errorf("a Quantity value for a TimeRef slot was admitted")
	}
}

func TestAnAnswerThatIsNotAJSONObjectIsRefused(t *testing.T) {
	_, prev := prevWithoutWhen(t)
	for _, v := range []string{``, `null`, `"yesterday"`, `[1]`, `{"type":`, `{} {}`} {
		if doc, err := understanding.SupersedeWithSlot(prev, "when", json.RawMessage(v), answeredAt); err == nil {
			t.Errorf("%q gave a document: %s", v, doc)
		}
	}
}
