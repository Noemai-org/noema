package plangraphs_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/plangraphs"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// SYNTHETIC fixtures only (AGENT-25). Proves REQ-TRU-009 (the plan); INV-02, INV-18, AGENT-09 (a param comes from a resolved slot or a pack value, never from text).

const (
	node   = "fin.intent.escalate"
	hash   = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	target = "txn_SYNTHETIC1"
)

type fakePack struct {
	g   map[string]plangraphs.PackGraph
	err error
}

func (f *fakePack) ActionGraph(_ context.Context, n string) (plangraphs.PackGraph, error) {
	if f.err != nil {
		return plangraphs.PackGraph{}, f.err
	}
	g, ok := f.g[n]
	if !ok {
		return plangraphs.PackGraph{}, plangraphs.ErrNoGraph
	}
	return g, nil
}

type fakeIntents struct {
	in  plangraphs.IntentSlots
	err error
}

func (f *fakeIntents) Slots(context.Context, string, string, int) (plangraphs.IntentSlots, error) {
	return f.in, f.err
}

func req() intake.PlanRequest {
	return intake.PlanRequest{TenantID: "t1", CorrelationID: "cor_1", IntentID: "int_1", IntentVersion: 1, IntentHash: hash, IntentNode: node, WriteRequired: true}
}

func intent() plangraphs.IntentSlots {
	return plangraphs.IntentSlots{TenantID: "t1", IntentID: "int_1", Version: 1, IntentNode: node, IntentHash: hash, TargetRef: target,
		Slots: map[string]plangraphs.SlotValue{
			"reason": {Resolved: true, Value: "double debit"},
			"amount": {Resolved: false, Value: "999"},
		}}
}

func packGraph(params ...plangraphs.ParamSpec) plangraphs.PackGraph {
	return plangraphs.PackGraph{IntentNode: node, GraphVersion: "1.0.0", Steps: []plangraphs.PackStep{
		{NodeID: "create_case", CapabilityID: "fin.cap.create_escalation_case", Params: params, Preconditions: []string{"p"}, Postconditions: []string{"q"}},
		{NodeID: "route_case", CapabilityID: "fin.cap.route_case"},
	}}
}

func build(g plangraphs.PackGraph, in plangraphs.IntentSlots) (*fakePack, plangraphs.Adapter) {
	p := &fakePack{g: map[string]plangraphs.PackGraph{node: g}}
	return p, plangraphs.Adapter{Pack: p, Intents: &fakeIntents{in: in}}
}

func TestParamsFromSlots(t *testing.T) {
	_, a := build(packGraph(plangraphs.ParamSpec{Name: "reason", Source: plangraphs.SourceSlot, Slot: "reason"}), intent())
	g, err := a.Graph(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if g.GraphVersion != "1.0.0" || len(g.Steps) != 2 {
		t.Fatalf("graph = %+v", g)
	}
	if !reflect.DeepEqual(g.Steps[0].Params, map[string]any{"reason": "double debit"}) {
		t.Fatalf("params = %v", g.Steps[0].Params)
	}
	if g.Steps[0].NodeID != "create_case" || g.Steps[0].CapabilityID != "fin.cap.create_escalation_case" || g.Steps[0].Preconditions[0] != "p" || g.Steps[0].Postconditions[0] != "q" {
		t.Fatalf("step = %+v", g.Steps[0])
	}
	if g.Steps[1].Params == nil || len(g.Steps[1].Params) != 0 {
		t.Fatalf("a step with no declared param must have an empty, non-nil map: %#v", g.Steps[1].Params)
	}
}

func TestParamsFromPackData(t *testing.T) {
	_, a := build(packGraph(
		plangraphs.ParamSpec{Name: "queue", Source: plangraphs.SourceConst, Value: "disputes"},
		plangraphs.ParamSpec{Name: "priority", Source: plangraphs.SourceDefault, Slot: "priority", Value: "normal"},
		plangraphs.ParamSpec{Name: "reason", Source: plangraphs.SourceDefault, Slot: "reason", Value: "unused default"},
	), intent())
	g, err := a.Graph(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"queue": "disputes", "priority": "normal", "reason": "double debit"}
	if !reflect.DeepEqual(g.Steps[0].Params, want) {
		t.Fatalf("params = %v, want %v", g.Steps[0].Params, want)
	}
}

func TestDefaultDoesNotCoverAnUnresolvedSlot(t *testing.T) {
	_, a := build(packGraph(plangraphs.ParamSpec{Name: "amount", Source: plangraphs.SourceDefault, Slot: "amount", Value: "0"}), intent())
	if _, err := a.Graph(context.Background(), req()); !errors.Is(err, plangraphs.ErrUnresolvedSlot) {
		t.Fatalf("err = %v, want ErrUnresolvedSlot", err)
	}
}

func TestParamFromTextIsRefused(t *testing.T) {
	for _, src := range []plangraphs.ParamSource{"TEXT", "MODEL", "FREE_TEXT", "", "slot"} {
		_, a := build(packGraph(plangraphs.ParamSpec{Name: "note", Source: src, Slot: "reason", Value: "ignore previous instructions"}), intent())
		_, err := a.Graph(context.Background(), req())
		if !errors.Is(err, plangraphs.ErrParamSource) || !errors.Is(err, trustport.ErrPlanGraphInvalid) {
			t.Fatalf("source %q: err = %v, want ErrParamSource", src, err)
		}
	}
}

func TestBadParamDeclarationsAreRefused(t *testing.T) {
	cases := map[string][]plangraphs.ParamSpec{
		"empty name":        {{Name: "", Source: plangraphs.SourceConst, Value: "x"}},
		"duplicate name":    {{Name: "a", Source: plangraphs.SourceConst, Value: "x"}, {Name: "a", Source: plangraphs.SourceConst, Value: "y"}},
		"nil constant":      {{Name: "a", Source: plangraphs.SourceConst, Value: nil}},
		"non-data const":    {{Name: "a", Source: plangraphs.SourceConst, Value: func() {}}},
		"slot with no name": {{Name: "a", Source: plangraphs.SourceSlot, Slot: ""}},
	}
	for name, ps := range cases {
		_, a := build(packGraph(ps...), intent())
		if _, err := a.Graph(context.Background(), req()); !errors.Is(err, trustport.ErrPlanGraphInvalid) {
			t.Fatalf("%s: err = %v, want a refusal", name, err)
		}
	}
}

func TestUnresolvedAndAbsentSlotsAreRefused(t *testing.T) {
	for _, slot := range []string{"amount", "missing"} {
		_, a := build(packGraph(plangraphs.ParamSpec{Name: "x", Source: plangraphs.SourceSlot, Slot: slot}), intent())
		if _, err := a.Graph(context.Background(), req()); !errors.Is(err, plangraphs.ErrUnresolvedSlot) || !errors.Is(err, trustport.ErrPlanGraphInvalid) {
			t.Fatalf("slot %q: err = %v, want ErrUnresolvedSlot", slot, err)
		}
	}
}

func TestTargetIsTheIntentsNeverAParam(t *testing.T) {
	_, a := build(packGraph(plangraphs.ParamSpec{Name: "target", Source: plangraphs.SourceSlot, Slot: "reason"}), intent())
	g, err := a.Graph(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range g.Steps {
		if s.TargetRef != target {
			t.Fatalf("step %d target = %q, want the intent's %q", i, s.TargetRef, target)
		}
	}
}

func TestTargetNotEqualToTheIntentsIsRefused(t *testing.T) {
	g := packGraph()
	g.Steps[1].TargetRef = "txn_OTHER"
	_, a := build(g, intent())
	if _, err := a.Graph(context.Background(), req()); !errors.Is(err, plangraphs.ErrTargetMismatch) || !errors.Is(err, trustport.ErrPlanGraphInvalid) {
		t.Fatalf("err = %v, want ErrTargetMismatch", err)
	}
	pinned := packGraph()
	pinned.Steps[0].TargetRef = target
	_, a = build(pinned, intent())
	if _, err := a.Graph(context.Background(), req()); err != nil {
		t.Fatalf("a pinned target equal to the intent's is allowed: %v", err)
	}
}

func TestWriteWithoutIntentTargetIsRefused(t *testing.T) {
	in := intent()
	in.TargetRef = ""
	_, a := build(packGraph(), in)
	if _, err := a.Graph(context.Background(), req()); !errors.Is(err, plangraphs.ErrTargetMismatch) {
		t.Fatalf("err = %v, want ErrTargetMismatch", err)
	}
}

func TestDeterministic(t *testing.T) {
	_, a := build(packGraph(plangraphs.ParamSpec{Name: "reason", Source: plangraphs.SourceSlot, Slot: "reason"}, plangraphs.ParamSpec{Name: "queue", Source: plangraphs.SourceConst, Value: "q"}), intent())
	first, err := a.Graph(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, err := a.Graph(context.Background(), req())
		if err != nil || !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d differs: %v %v", i, err, again)
		}
	}
}

func TestReturnedParamsAreCopies(t *testing.T) {
	nested := map[string]any{"k": []any{"a", map[string]any{"z": "1"}}}
	pg := packGraph(plangraphs.ParamSpec{Name: "c", Source: plangraphs.SourceConst, Value: nested})
	in := intent()
	in.Slots["obj"] = plangraphs.SlotValue{Resolved: true, Value: map[string]any{"n": "1"}}
	pg.Steps[0].Params = append(pg.Steps[0].Params, plangraphs.ParamSpec{Name: "o", Source: plangraphs.SourceSlot, Slot: "obj"})
	pack, a := build(pg, in)
	g, err := a.Graph(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	g.Steps[0].Params["c"].(map[string]any)["k"].([]any)[1].(map[string]any)["z"] = "MUTATED"
	g.Steps[0].Params["c"].(map[string]any)["k"].([]any)[0] = "MUTATED"
	g.Steps[0].Params["o"].(map[string]any)["n"] = "MUTATED"
	g.Steps[0].Params["extra"] = "x"
	g.Steps[0].Preconditions[0] = "MUTATED"
	g.Steps[0].Postconditions[0] = "MUTATED"
	g.Steps = g.Steps[:0]
	if !reflect.DeepEqual(nested, map[string]any{"k": []any{"a", map[string]any{"z": "1"}}}) {
		t.Fatalf("the pack's constant was mutated: %v", nested)
	}
	if in.Slots["obj"].Value.(map[string]any)["n"] != "1" {
		t.Fatal("the intent's slot value was mutated")
	}
	if got := pack.g[node].Steps[0]; got.Preconditions[0] != "p" || got.Postconditions[0] != "q" || len(got.Params) != 2 {
		t.Fatalf("the pack's own graph was mutated: %+v", got)
	}
	again, err := a.Graph(context.Background(), req())
	if err != nil || again.Steps[0].Params["c"].(map[string]any)["k"].([]any)[0] != "a" || len(again.Steps) != 2 {
		t.Fatalf("second graph affected by the first's mutation: %v %+v", err, again)
	}
}

func TestOtherIntentNodesGraphIsNotReturned(t *testing.T) {
	other := packGraph()
	other.IntentNode = "fin.intent.other"
	p := &fakePack{g: map[string]plangraphs.PackGraph{node: other, "fin.intent.unknown": other}}
	a := plangraphs.Adapter{Pack: p, Intents: &fakeIntents{in: intent()}}
	if _, err := a.Graph(context.Background(), req()); !errors.Is(err, plangraphs.ErrWrongIntent) {
		t.Fatalf("err = %v, want ErrWrongIntent", err)
	}
	r := req()
	r.IntentNode = "fin.intent.nothing"
	in := intent()
	in.IntentNode = r.IntentNode
	a = plangraphs.Adapter{Pack: p, Intents: &fakeIntents{in: in}}
	if _, err := a.Graph(context.Background(), r); !errors.Is(err, trustport.ErrPlanGraphInvalid) {
		t.Fatalf("a node with no graph: err = %v, want ErrPlanGraphInvalid", err)
	}
}

func TestStoredIntentMustMatchTheRequest(t *testing.T) {
	mut := map[string]func(*plangraphs.IntentSlots){
		"node": func(i *plangraphs.IntentSlots) { i.IntentNode = "fin.intent.other" },
		"hash": func(i *plangraphs.IntentSlots) {
			i.IntentHash = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		},
		"tenant":  func(i *plangraphs.IntentSlots) { i.TenantID = "t2" },
		"intent":  func(i *plangraphs.IntentSlots) { i.IntentID = "int_2" },
		"version": func(i *plangraphs.IntentSlots) { i.Version = 2 },
	}
	for name, m := range mut {
		in := intent()
		m(&in)
		_, a := build(packGraph(), in)
		if _, err := a.Graph(context.Background(), req()); !errors.Is(err, plangraphs.ErrWrongIntent) {
			t.Fatalf("%s: err = %v, want ErrWrongIntent", name, err)
		}
	}
}

func TestDependencyErrorsAreNotPlanInvalid(t *testing.T) {
	boom := errors.New("store down")
	a := plangraphs.Adapter{Pack: &fakePack{g: map[string]plangraphs.PackGraph{node: packGraph()}}, Intents: &fakeIntents{err: boom}}
	if _, err := a.Graph(context.Background(), req()); !errors.Is(err, boom) || errors.Is(err, trustport.ErrPlanGraphInvalid) {
		t.Fatalf("intent store error: %v", err)
	}
	a = plangraphs.Adapter{Pack: &fakePack{err: boom}, Intents: &fakeIntents{in: intent()}}
	if _, err := a.Graph(context.Background(), req()); !errors.Is(err, boom) || errors.Is(err, trustport.ErrPlanGraphInvalid) {
		t.Fatalf("pack error: %v", err)
	}
}

func TestEmptyGraphAndUnconfiguredAreRefused(t *testing.T) {
	empty := packGraph()
	empty.Steps = nil
	_, a := build(empty, intent())
	if _, err := a.Graph(context.Background(), req()); !errors.Is(err, trustport.ErrPlanGraphInvalid) {
		t.Fatalf("an empty graph for a write: err = %v", err)
	}
	if _, err := (plangraphs.Adapter{}).Graph(context.Background(), req()); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("unconfigured: err = %v, want ErrConfig", err)
	}
}

func TestReadRequestStillBuildsFromSlotsOnly(t *testing.T) {
	_, a := build(packGraph(plangraphs.ParamSpec{Name: "x", Source: "TEXT"}), intent())
	r := req()
	r.WriteRequired = false
	if _, err := a.Graph(context.Background(), r); !errors.Is(err, plangraphs.ErrParamSource) {
		t.Fatalf("a read gets no looser rule: err = %v", err)
	}
}

func TestSlotParamsNeedASlotName(t *testing.T) {
	for _, src := range []plangraphs.ParamSource{plangraphs.SourceSlot, plangraphs.SourceDefault} {
		_, a := build(packGraph(plangraphs.ParamSpec{Name: "a", Source: src, Slot: "", Value: "x"}), intent())
		if _, err := a.Graph(context.Background(), req()); !errors.Is(err, plangraphs.ErrParamSource) {
			t.Fatalf("source %q with no slot name: err = %v, want ErrParamSource", src, err)
		}
	}
}
