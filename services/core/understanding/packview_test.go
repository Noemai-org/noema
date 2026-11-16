package understanding_test

// UND-04, core half, ruling A166 item 4 (REQ-INT-001; PERD 18.4 boundary 4): the gate's PackView is read from the mounted pack data (packs/fintech_v1.json), never
// hard-coded in the core, and a pack that cannot give a sound view is refused. SYNTHETIC data.

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/understanding"
)

const packPath = "../../../packs/fintech_v1.json"

func realPack(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestThePackViewIsReadFromTheMountedPack(t *testing.T) {
	v, err := understanding.NewPackView(realPack(t))
	if err != nil {
		t.Fatal(err)
	}
	if id, ver := v.Ref(); id != "fin" || ver != "1.0.0" {
		t.Errorf("Ref() = %q %q, want fin 1.0.0", id, ver)
	}
	n, ok := v.Node("fin.payments.exception.investigate")
	if !ok {
		t.Fatal("the investigate node is not in the view")
	}
	wantSlots := map[string]string{"subject_resource": "Resource", "amount": "Quantity", "counterparty": "Party", "when": "TimeRef", "question_type": "Reference", "requested_remedy": "Reference"}
	if !reflect.DeepEqual(n.Slots, wantSlots) {
		t.Errorf("slots %v, want %v", n.Slots, wantSlots)
	}
	wantOutcomes := map[string]bool{"fin.outcome.exception.status_explained": true, "fin.outcome.exception.case_opened": true, "fin.outcome.exception.case_routed": true,
		"fin.outcome.exception.evidence_recorded": true, "fin.outcome.exception.complaint_logged": true}
	if !reflect.DeepEqual(n.Outcomes, wantOutcomes) {
		t.Errorf("outcomes %v, want %v", n.Outcomes, wantOutcomes)
	}
	if o, ok := v.Node("fin.meta.out_of_scope"); !ok || len(o.Slots) != 0 || len(o.Outcomes) != 0 {
		t.Errorf("the out-of-scope node: %+v %v, want a node with no slots and no outcomes", o, ok)
	}
	for _, id := range []string{"", "fin.payments.exception", "fin.payments.exception.investigate ", "FIN.PAYMENTS.EXCEPTION.INVESTIGATE", "fin.transfer.send"} {
		if _, ok := v.Node(id); ok {
			t.Errorf("Node(%q) found a node the pack does not define", id)
		}
	}
}

// The gate over the real pack admits an intent for the pack's own node and slot names, and refuses a slot or an outcome the pack does not define: the first end to end
// use of the pack by the gate (INV-02).
func TestTheGateOverTheMountedPackAdmitsItsOwnNodeAndRefusesWhatItDoesNotDefine(t *testing.T) {
	v, err := understanding.NewPackView(realPack(t))
	if err != nil {
		t.Fatal(err)
	}
	g, err := understanding.NewGate(64<<10, v)
	if err != nil {
		t.Fatal(err)
	}
	obj := func(mutate func(m map[string]any)) []byte {
		return raw(t, func(m map[string]any) {
			m["intent_node"] = "fin.payments.exception.investigate"
			m["slots"] = map[string]any{
				"subject_resource": map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX88213"},
				"question_type":    map[string]any{"type": "Reference", "value": "STATUS"},
			}
			m["desired_outcome_refs"] = []any{"fin.outcome.exception.status_explained"}
			if mutate != nil {
				mutate(m)
			}
		})
	}
	if _, err := g.Admit(obj(nil), session, corr); err != nil {
		t.Fatalf("an intent for the pack's own node: %v", err)
	}
	for name, c := range map[string]struct {
		mutate func(m map[string]any)
		want   understanding.Reason
	}{
		"a slot the node does not name": {func(m map[string]any) {
			m["slots"].(map[string]any)["transaction"] = map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX1"}
		}, understanding.ReasonUnknownSlot},
		"an outcome the node lacks":     {func(m map[string]any) { m["desired_outcome_refs"] = []any{"fin.outcome.refunded"} }, understanding.ReasonOutcomeNotInNode},
		"a node the pack does not have": {func(m map[string]any) { m["intent_node"] = "fin.transfer.send" }, understanding.ReasonUnknownNode},
		"a slot of the wrong type": {func(m map[string]any) {
			m["slots"].(map[string]any)["question_type"] = map[string]any{"type": "Party", "surface_form": "Ade"}
		}, understanding.ReasonSlotType},
	} {
		if _, err := g.Admit(obj(c.mutate), session, corr); err == nil || !has(reasons(t, err), c.want) {
			t.Errorf("%s: %v, want %s", name, err, c.want)
		}
	}
}

func TestAPackThatCannotGiveASoundViewIsRefused(t *testing.T) {
	edit := func(f func(p map[string]any)) []byte {
		var p map[string]any
		if err := json.Unmarshal(realPack(t), &p); err != nil {
			t.Fatal(err)
		}
		f(p)
		b, _ := json.Marshal(p)
		return b
	}
	node := func(p map[string]any, i int) map[string]any { return p["intent_nodes"].([]any)[i].(map[string]any) }
	// A pack id or version that is off its shape, with the nodes agreeing with it, so that only the shape rule can refuse it.
	rename := func(p map[string]any, id, version string) {
		p["pack_id"], p["pack_version"] = id, version
		for i := range p["intent_nodes"].([]any) {
			node(p, i)["pack"], node(p, i)["pack_version"] = id, version
		}
	}
	for name, doc := range map[string][]byte{
		"empty":                          nil,
		"not json":                       []byte(`{`),
		"trailing data":                  append(realPack(t), []byte(` {}`)...),
		"an array":                       []byte(`[]`),
		"no pack id":                     edit(func(p map[string]any) { delete(p, "pack_id") }),
		"a pack id that is dotted":       edit(func(p map[string]any) { rename(p, "fin.x", "1.0.0") }),
		"a pack id off shape":            edit(func(p map[string]any) { rename(p, "Fin", "1.0.0") }),
		"no pack version":                edit(func(p map[string]any) { delete(p, "pack_version") }),
		"a pack version off shape":       edit(func(p map[string]any) { rename(p, "fin", "1.0") }),
		"no nodes":                       edit(func(p map[string]any) { p["intent_nodes"] = []any{} }),
		"nodes null":                     edit(func(p map[string]any) { p["intent_nodes"] = nil }),
		"a node without an id":           edit(func(p map[string]any) { delete(node(p, 0), "node_id") }),
		"a node id twice":                edit(func(p map[string]any) { node(p, 1)["node_id"] = node(p, 0)["node_id"] }),
		"a node of another pack":         edit(func(p map[string]any) { node(p, 0)["pack"] = "other" }),
		"a node of another pack version": edit(func(p map[string]any) { node(p, 0)["pack_version"] = "2.0.0" }),
		"a slot name twice": edit(func(p map[string]any) {
			s := node(p, 0)["slots"].([]any)
			node(p, 0)["slots"] = append(s, s[0])
		}),
		"a slot name off shape":              edit(func(p map[string]any) { node(p, 0)["slots"].([]any)[0].(map[string]any)["name"] = "Subject-Resource" }),
		"a slot type the gate does not know": edit(func(p map[string]any) { node(p, 0)["slots"].([]any)[0].(map[string]any)["type"] = "Blob" }),
		"a node outcome the pack does not define": edit(func(p map[string]any) {
			node(p, 0)["outcome_definitions"] = append(node(p, 0)["outcome_definitions"].([]any), "fin.outcome.invented")
		}),
		"an outcome twice in a node": edit(func(p map[string]any) {
			o := node(p, 0)["outcome_definitions"].([]any)
			node(p, 0)["outcome_definitions"] = append(o, o[0])
		}),
		"an outcome definition twice": edit(func(p map[string]any) {
			o := p["outcome_definitions"].([]any)
			p["outcome_definitions"] = append(o, o[0])
		}),
	} {
		v, err := understanding.NewPackView(doc)
		if err == nil || v != nil {
			t.Errorf("%s: accepted (%v)", name, err)
			continue
		}
		if strings.Contains(err.Error(), "fin.payments") || strings.Contains(err.Error(), "SYNTHETIC") {
			t.Errorf("%s: the error quotes the pack: %v", name, err)
		}
		if !errors.Is(err, understanding.ErrPack) {
			t.Errorf("%s: %v is not ErrPack", name, err)
		}
	}
}

// What the view holds does not change when the caller changes the bytes it passed.
func TestThePackViewDoesNotShareTheCallersBytes(t *testing.T) {
	doc := realPack(t)
	v, err := understanding.NewPackView(doc)
	if err != nil {
		t.Fatal(err)
	}
	for i := range doc {
		doc[i] = 'x'
	}
	if _, ok := v.Node("fin.payments.exception.investigate"); !ok {
		t.Error("the view changed when the caller's bytes did")
	}
	n, _ := v.Node("fin.payments.exception.investigate")
	n.Slots["injected"] = "Resource"
	again, _ := v.Node("fin.payments.exception.investigate")
	if _, has := again.Slots["injected"]; has {
		t.Error("a caller that edits the node it was given changes the view")
	}
}
