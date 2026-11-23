package understanding_test

// DEBT-54 part 2 (ruling A520, interim; A551 for the accessor rule; INV-14, INV-12): the core reads which slot of an intent node is its subject from the pack (part 1 put
// `subject_slot` on the node), never from its own code, and takes the subject of an admitted intent from that slot's resolved reference. An intent whose subject slot is not
// resolved has no subject (the epoch stays 1 and the subject is NULL). SYNTHETIC data.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/understanding"
)

type subjectPack struct{}

func (subjectPack) Ref() (string, string) { return "fin", "1.0.0" }
func (subjectPack) Node(id string) (understanding.Node, bool) {
	switch id {
	case "fin.payments.exception.investigate":
		return understanding.Node{Slots: map[string]string{"transaction": "Resource", "amount": "Quantity", "counterparty": "Party", "when": "TimeRef", "question_type": "Reference"},
			Outcomes: map[string]bool{"fin.outcome.explained": true, "fin.outcome.escalated": true}, SubjectSlot: "transaction"}, true
	}
	return understanding.Node{}, false
}

func admittedWith(t *testing.T, p understanding.PackView, mutate func(m map[string]any)) understanding.AdmittedIntent {
	t.Helper()
	g, err := understanding.NewGate(64<<10, p)
	if err != nil {
		t.Fatal(err)
	}
	a, err := g.Admit(raw(t, mutate), session, corr)
	if err != nil {
		t.Fatal(err)
	}
	return a.Intent
}

// The view carries the subject slot name of each node, as the pack declares it; a node that declares none has none.
func TestThePackViewCarriesTheSubjectSlotTheNodeDeclares(t *testing.T) {
	v, err := understanding.NewPackView(realPack(t))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := v.Node("fin.payments.exception.investigate"); n.SubjectSlot != "subject_resource" {
		t.Errorf("the investigate node's subject slot is %q, want subject_resource", n.SubjectSlot)
	}
	if n, _ := v.Node("fin.meta.out_of_scope"); n.SubjectSlot != "" {
		t.Errorf("the out-of-scope node's subject slot is %q, want none", n.SubjectSlot)
	}
}

// A pack whose subject slot is not a Resource slot of its own node, or is off its shape, cannot give a sound view.
func TestAPackWhoseSubjectSlotIsNotAResourceSlotOfTheNodeIsRefused(t *testing.T) {
	for name, slot := range map[string]any{"not a slot of the node": "no_such_slot", "not a Resource slot": "amount", "off its shape": "Subject Resource", "empty": "", "a number": 7, "null": nil} {
		var doc map[string]any
		if err := json.Unmarshal(realPack(t), &doc); err != nil {
			t.Fatal(err)
		}
		nodes := doc["intent_nodes"].([]any)
		for _, n := range nodes {
			if m := n.(map[string]any); m["node_id"] == "fin.payments.exception.investigate" {
				m["subject_slot"] = slot
			}
		}
		b, _ := json.Marshal(doc)
		if _, err := understanding.NewPackView(b); !errors.Is(err, understanding.ErrPack) {
			t.Errorf("%s: err %v, want ErrPack", name, err)
		}
	}
}

// A551: a pack that fails verification yields no accessor: the view is built from the bytes the caller verified, and a refused pack returns no view at all.
func TestARefusedPackYieldsNoView(t *testing.T) {
	v, err := understanding.NewPackView([]byte(`{"pack_id":"fin"`))
	if v != nil || !errors.Is(err, understanding.ErrPack) {
		t.Errorf("view %v err %v, want no view and ErrPack", v, err)
	}
}

// The subject of an admitted intent is the resolved reference of the slot the node names; every other state of that slot gives no subject.
func TestTheSubjectOfAnAdmittedIntentIsTheResolvedReferenceOfTheNodesSubjectSlot(t *testing.T) {
	subject := understanding.SubjectOf(subjectPack{})
	if ref, ok := subject(admittedWith(t, subjectPack{}, nil)); !ok || ref != "fin.transaction:TX88213" {
		t.Errorf("subject %q %v, want fin.transaction:TX88213", ref, ok)
	}
	for name, slot := range map[string]map[string]any{
		"unresolved": {"type": "Resource", "status": "UNRESOLVED"},
		"ambiguous":  {"type": "Resource", "status": "AMBIGUOUS", "candidates_ref": "cand_01J9X3"},
		"absent":     {"type": "Resource", "status": "ABSENT"},
	} {
		slot := slot
		a := admittedWith(t, subjectPack{}, func(m map[string]any) { m["slots"].(map[string]any)["transaction"] = slot })
		if ref, ok := subject(a); ok || ref != "" {
			t.Errorf("%s: subject %q %v, want none", name, ref, ok)
		}
	}
}

// A node that declares no subject slot gives no subject, whatever its slots hold, and a node the view does not know gives none (the gate would have refused it).
func TestANodeWithNoSubjectSlotGivesNoSubject(t *testing.T) {
	v, err := understanding.NewPackView(realPack(t))
	if err != nil {
		t.Fatal(err)
	}
	a := admittedWith(t, testPack{}, nil) // testPack's node declares no subject slot
	if ref, ok := understanding.SubjectOf(testPack{})(a); ok || ref != "" {
		t.Errorf("subject %q %v for a node with no subject slot, want none", ref, ok)
	}
	if ref, ok := understanding.SubjectOf(v)(a); ok || strings.Contains(ref, "TX") {
		t.Errorf("a view that does not know the node gave subject %q %v, want none", ref, ok)
	}
}
