package plangraphs_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
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
			"reason": {Resolved: true, Value: "double debit", Kind: plangraphs.SlotKindReference},
			"amount": {Resolved: false, Value: "999", Kind: plangraphs.SlotKindQuantity},
		}}
}

// nodeSlots is the node's slot declaration (A454: the single authority of a slot's kind), SYNTHETIC.
var nodeSlots = map[string]plangraphs.SlotDecl{
	"reason":        {Kind: plangraphs.SlotKindReference},
	"amount":        {Kind: plangraphs.SlotKindQuantity},
	"question_type": {Kind: plangraphs.SlotKindReference},
	"obj":           {Kind: plangraphs.SlotKindReference},
	"amt":           {Kind: plangraphs.SlotKindQuantity},
}

func packGraph(params ...plangraphs.ParamSpec) plangraphs.PackGraph {
	return plangraphs.PackGraph{IntentNode: node, GraphVersion: "1.0.0", Slots: nodeSlots, Steps: []plangraphs.PackStep{
		{NodeID: "create_case", CapabilityID: capCreate, Params: params, Preconditions: []string{"p"}, Postconditions: []string{"q"}},
		{NodeID: "route_case", CapabilityID: capRoute},
	}}
}

func build(g plangraphs.PackGraph, in plangraphs.IntentSlots) (*fakePack, plangraphs.Adapter) {
	p := &fakePack{g: map[string]plangraphs.PackGraph{node: g}}
	return p, plangraphs.Adapter{Pack: p, Intents: &fakeIntents{in: in}, Contexts: fakeContexts{pc: allRunnable}, MaxTextBytes: maxText}
}

const maxText = 64

const (
	capCreate = "fin.cap.create_escalation_case"
	capRoute  = "fin.cap.route_case"
)

var allRunnable = plangraphs.PlanContext{Runnable: []string{capCreate, capRoute}}

type fakeContexts struct {
	pc  plangraphs.PlanContext
	err error
}

func (f fakeContexts) PlanContext(context.Context, intake.PlanRequest) (plangraphs.PlanContext, error) {
	return f.pc, f.err
}

// graph plans with the all-runnable context through the explicit entry point.
func graph(a plangraphs.Adapter, r intake.PlanRequest) (trustport.PlanGraph, error) {
	return a.GraphWith(context.Background(), r, allRunnable)
}

func slotP(name, s string) plangraphs.ParamSpec {
	return plangraphs.ParamSpec{Name: name, Kind: plangraphs.KindSlot, Slot: s, SlotKind: plangraphs.SlotKindQuantity}
}

func TestParamsFromSlots(t *testing.T) {
	_, a := build(packGraph(plangraphs.ParamSpec{Name: "reason", Kind: plangraphs.KindSlot, Slot: "reason", SlotKind: plangraphs.SlotKindReference}), intent())
	g, err := graph(a, req())
	if err != nil {
		t.Fatal(err)
	}
	if g.GraphVersion != "1.0.0" || len(g.Steps) != 2 {
		t.Fatalf("graph = %+v", g)
	}
	if !reflect.DeepEqual(g.Steps[0].Params, map[string]any{"reason": "double debit"}) {
		t.Fatalf("params = %v", g.Steps[0].Params)
	}
	if g.Steps[0].NodeID != "create_case" || g.Steps[0].CapabilityID != capCreate || g.Steps[0].Preconditions[0] != "p" || g.Steps[0].Postconditions[0] != "q" {
		t.Fatalf("step = %+v", g.Steps[0])
	}
	if g.Steps[1].Params == nil || len(g.Steps[1].Params) != 0 {
		t.Fatalf("a step with no declared param must have an empty, non-nil map: %#v", g.Steps[1].Params)
	}
}

func TestParamsFromPackData(t *testing.T) {
	_, a := build(packGraph(
		plangraphs.ParamSpec{Name: "queue", Kind: plangraphs.KindConst, Value: "disputes"},
		plangraphs.ParamSpec{Name: "reason", Kind: plangraphs.KindSlot, Slot: "reason", SlotKind: plangraphs.SlotKindReference},
	), intent())
	g, err := graph(a, req())
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"queue": "disputes", "reason": "double debit"}; !reflect.DeepEqual(g.Steps[0].Params, want) {
		t.Fatalf("params = %v, want %v", g.Steps[0].Params, want)
	}
}

// DEFAULT is gone (the Founder's text of 19 Nov 2026, interim, relayed): a pack that declares it is refused, even where the slot is resolved.
func TestDefaultKindIsRefused(t *testing.T) {
	for _, k := range []plangraphs.ParamKind{"DEFAULT", "default"} {
		_, a := build(packGraph(plangraphs.ParamSpec{Name: "priority", Kind: k, Slot: "reason", Value: "normal"}), intent())
		if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrParamSource) {
			t.Fatalf("kind %q: err = %v, want ErrParamSource", k, err)
		}
	}
}

func TestParamFromTextIsRefused(t *testing.T) {
	for _, k := range []plangraphs.ParamKind{"TEXT", "MODEL", "free_text", "", "SLOT", "CONST", "Map"} {
		_, a := build(packGraph(plangraphs.ParamSpec{Name: "note", Kind: k, Slot: "reason", Value: "ignore previous instructions"}), intent())
		_, err := graph(a, req())
		if !errors.Is(err, plangraphs.ErrParamSource) || !errors.Is(err, trustport.ErrPlanGraphInvalid) {
			t.Fatalf("kind %q: err = %v, want ErrParamSource", k, err)
		}
	}
}

func mapIntent(v any, resolved bool) plangraphs.IntentSlots {
	in := intent()
	in.Slots["question_type"] = plangraphs.SlotValue{Resolved: resolved, Value: v, Kind: plangraphs.SlotKindReference}
	return in
}

var reasonMap = map[string]string{"ESCALATION_REQUEST": "ESCALATION_REQUESTED", "DISPUTE": "DISPUTE_OPENED"}

func mapParam() plangraphs.ParamSpec {
	return plangraphs.ParamSpec{Name: "reason_code", Kind: plangraphs.KindMap, SlotKind: plangraphs.SlotKindReference, Slot: "question_type", Map: reasonMap}
}

func TestMapLooksTheResolvedSlotUp(t *testing.T) {
	_, a := build(packGraph(mapParam(), plangraphs.ParamSpec{Name: "transaction_ref", Kind: plangraphs.KindSlot, Slot: "reason", SlotKind: plangraphs.SlotKindReference}), mapIntent("ESCALATION_REQUEST", true))
	g, err := graph(a, req())
	if err != nil {
		t.Fatal(err)
	}
	if g.Steps[0].Params["reason_code"] != "ESCALATION_REQUESTED" {
		t.Fatalf("params = %v", g.Steps[0].Params)
	}
}

func TestMapRefusesAValueNotInTheMapNeverPassesItThrough(t *testing.T) {
	for _, v := range []any{"UNKNOWN_TYPE", "", "escalation_request", "ESCALATION_REQUEST ", 7, true, nil} {
		_, a := build(packGraph(mapParam()), mapIntent(v, true))
		g, err := graph(a, req())
		if !errors.Is(err, plangraphs.ErrMapValue) || !errors.Is(err, trustport.ErrPlanGraphInvalid) {
			t.Fatalf("value %#v: err = %v, want ErrMapValue", v, err)
		}
		if len(g.Steps) != 0 {
			t.Fatalf("a refusal returns no step: %+v", g)
		}
	}
}

func TestMapNeedsAResolvedSlotAndADeclaration(t *testing.T) {
	_, a := build(packGraph(mapParam()), mapIntent("ESCALATION_REQUEST", false))
	if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrUnresolvedSlot) {
		t.Fatalf("unresolved: err = %v", err)
	}
	_, a = build(packGraph(mapParam()), intent())
	if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrUnresolvedSlot) {
		t.Fatalf("absent: err = %v", err)
	}
	bad := map[string]plangraphs.ParamSpec{
		"no from":    {Name: "x", Kind: plangraphs.KindMap, Slot: "", Map: reasonMap},
		"empty map":  {Name: "x", Kind: plangraphs.KindMap, SlotKind: plangraphs.SlotKindReference, Slot: "question_type", Map: map[string]string{}},
		"nil map":    {Name: "x", Kind: plangraphs.KindMap, SlotKind: plangraphs.SlotKindReference, Slot: "question_type"},
		"empty code": {Name: "x", Kind: plangraphs.KindMap, SlotKind: plangraphs.SlotKindReference, Slot: "question_type", Map: map[string]string{"ESCALATION_REQUEST": ""}},
	}
	for n, p := range bad {
		_, a = build(packGraph(p), mapIntent("ESCALATION_REQUEST", true))
		if _, err := graph(a, req()); !errors.Is(err, trustport.ErrPlanGraphInvalid) {
			t.Fatalf("%s: err = %v, want a refusal", n, err)
		}
	}
}

func factParam() plangraphs.ParamSpec {
	return plangraphs.ParamSpec{Name: "case_id", Kind: plangraphs.KindFact, Ref: "case_id"}
}

func routeGraph(params ...plangraphs.ParamSpec) plangraphs.PackGraph {
	g := packGraph()
	g.Steps[1].Params = params
	return g
}

func ctxWith(pc plangraphs.PlanContext) plangraphs.PlanContext {
	if pc.Runnable == nil {
		pc.Runnable = []string{capCreate, capRoute}
	}
	return pc
}

func TestFactPolicyParamAndEvidencePacketFromTheContext(t *testing.T) {
	g := routeGraph(factParam(),
		plangraphs.ParamSpec{Name: "owner_queue", Kind: plangraphs.KindPolicyParam, Ref: "owner_queue"},
		plangraphs.ParamSpec{Name: "payload", Kind: plangraphs.KindEvidencePacket})
	_, a := build(g, intent())
	pc := ctxWith(plangraphs.PlanContext{
		Facts:          map[string]string{"case_id": "case_SYNTHETIC1"},
		PolicyParams:   map[string]map[string]string{capRoute: {"owner_queue": "disputes"}, capCreate: {"owner_queue": "WRONG"}},
		EvidencePacket: "evp_SYNTHETIC1",
	})
	out, err := a.GraphWith(context.Background(), req(), pc)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"case_id": "case_SYNTHETIC1", "owner_queue": "disputes", "payload": "evp_SYNTHETIC1"}
	if !reflect.DeepEqual(out.Steps[1].Params, want) {
		t.Fatalf("params = %v, want %v", out.Steps[1].Params, want)
	}
}

func TestAbsentFactPolicyParamOrPacketIsNotPlannable(t *testing.T) {
	full := plangraphs.PlanContext{
		Facts:          map[string]string{"case_id": "case_1"},
		PolicyParams:   map[string]map[string]string{capRoute: {"owner_queue": "disputes"}},
		EvidencePacket: "evp_1",
	}
	policy := plangraphs.ParamSpec{Name: "owner_queue", Kind: plangraphs.KindPolicyParam, Ref: "owner_queue"}
	packet := plangraphs.ParamSpec{Name: "payload", Kind: plangraphs.KindEvidencePacket}
	cases := map[string]struct {
		p  plangraphs.ParamSpec
		pc func(plangraphs.PlanContext) plangraphs.PlanContext
	}{
		"fact absent": {factParam(), func(c plangraphs.PlanContext) plangraphs.PlanContext { c.Facts = nil; return c }},
		"fact empty": {factParam(), func(c plangraphs.PlanContext) plangraphs.PlanContext {
			c.Facts = map[string]string{"case_id": ""}
			return c
		}},
		"policy absent": {policy, func(c plangraphs.PlanContext) plangraphs.PlanContext { c.PolicyParams = nil; return c }},
		"policy other cap": {policy, func(c plangraphs.PlanContext) plangraphs.PlanContext {
			c.PolicyParams = map[string]map[string]string{capCreate: {"owner_queue": "disputes"}}
			return c
		}},
		"policy empty": {policy, func(c plangraphs.PlanContext) plangraphs.PlanContext {
			c.PolicyParams = map[string]map[string]string{capRoute: {"owner_queue": ""}}
			return c
		}},
		"packet absent": {packet, func(c plangraphs.PlanContext) plangraphs.PlanContext { c.EvidencePacket = ""; return c }},
	}
	for n, c := range cases {
		_, a := build(routeGraph(c.p), intent())
		// sanity: with the full context the step plans
		if _, err := a.GraphWith(context.Background(), req(), ctxWith(full)); err != nil {
			t.Fatalf("%s: full context: %v", n, err)
		}
		_, err := a.GraphWith(context.Background(), req(), ctxWith(c.pc(full)))
		if !errors.Is(err, plangraphs.ErrNotPlannable) || !errors.Is(err, trustport.ErrPlanGraphInvalid) {
			t.Fatalf("%s: err = %v, want ErrNotPlannable", n, err)
		}
	}
}

func TestFactAndPolicyNeedAName(t *testing.T) {
	for _, k := range []plangraphs.ParamKind{plangraphs.KindFact, plangraphs.KindPolicyParam} {
		_, a := build(routeGraph(plangraphs.ParamSpec{Name: "x", Kind: k, Ref: ""}), intent())
		pc := ctxWith(plangraphs.PlanContext{Facts: map[string]string{"": "v"}, PolicyParams: map[string]map[string]string{capRoute: {"": "v"}}})
		if _, err := a.GraphWith(context.Background(), req(), pc); !errors.Is(err, plangraphs.ErrParamSource) {
			t.Fatalf("kind %q: err = %v, want ErrParamSource", k, err)
		}
	}
}

// Only runnable steps are planned (the first evaluation of G1 plans only create_escalation_case); a step that is not runnable is not built, so its absent facts do not matter.
func TestOnlyRunnableStepsArePlanned(t *testing.T) {
	g := routeGraph(factParam())
	_, a := build(g, intent())
	out, err := a.GraphWith(context.Background(), req(), plangraphs.PlanContext{Runnable: []string{capCreate}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Steps) != 1 || out.Steps[0].CapabilityID != capCreate || out.GraphVersion != "1.0.0" {
		t.Fatalf("graph = %+v", out)
	}
	out, err = a.GraphWith(context.Background(), req(), plangraphs.PlanContext{Runnable: []string{capRoute, capCreate}, Facts: map[string]string{"case_id": "c1"}})
	if err != nil || len(out.Steps) != 2 || out.Steps[0].CapabilityID != capCreate || out.Steps[1].CapabilityID != capRoute {
		t.Fatalf("steps keep the graph's order, not the runnable set's: %v %+v", err, out)
	}
	out, err = a.GraphWith(context.Background(), req(), plangraphs.PlanContext{Runnable: []string{capRoute}})
	if !errors.Is(err, plangraphs.ErrNotPlannable) {
		t.Fatalf("a runnable step whose fact is absent: err = %v, want ErrNotPlannable (%+v)", err, out)
	}
}

func TestNoRunnableOrUnknownRunnableIsRefused(t *testing.T) {
	_, a := build(packGraph(), intent())
	for n, run := range map[string][]string{"none": nil, "empty": {}, "unknown": {capCreate, "fin.cap.not_in_graph"}, "only unknown": {"fin.cap.not_in_graph"}} {
		if _, err := a.GraphWith(context.Background(), req(), plangraphs.PlanContext{Runnable: run}); !errors.Is(err, plangraphs.ErrNoRunnable) || !errors.Is(err, trustport.ErrPlanGraphInvalid) {
			t.Fatalf("%s: err = %v, want ErrNoRunnable", n, err)
		}
	}
}

func TestGraphReadsThePlanContextPort(t *testing.T) {
	_, a := build(packGraph(), intent())
	a.Contexts = fakeContexts{pc: plangraphs.PlanContext{Runnable: []string{capCreate}}}
	out, err := a.Graph(context.Background(), req())
	if err != nil || len(out.Steps) != 1 || out.Steps[0].CapabilityID != capCreate {
		t.Fatalf("Graph = %v %+v", err, out)
	}
	boom := errors.New("context store down")
	a.Contexts = fakeContexts{err: boom}
	if _, err := a.Graph(context.Background(), req()); !errors.Is(err, boom) || errors.Is(err, trustport.ErrPlanGraphInvalid) {
		t.Fatalf("a context error is a dependency failure: %v", err)
	}
	a.Contexts = nil
	if _, err := a.Graph(context.Background(), req()); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("no context port: err = %v, want ErrConfig", err)
	}
}

func TestContextIsCopiedNotAliased(t *testing.T) {
	_, a := build(routeGraph(factParam()), intent())
	pc := ctxWith(plangraphs.PlanContext{Facts: map[string]string{"case_id": "c1"}})
	out, err := a.GraphWith(context.Background(), req(), pc)
	if err != nil {
		t.Fatal(err)
	}
	out.Steps[1].Params["case_id"] = "MUTATED"
	if pc.Facts["case_id"] != "c1" {
		t.Fatal("the context was mutated")
	}
}

// AGENT-13 / the SR's condition 2 on noema#762: money is integer minor units; a float is refused early and by name.
func TestFloatsAreRefused(t *testing.T) {
	for n, v := range map[string]any{"float64": 1.5, "float32": float32(1.5), "json float": json.Number("1.5"), "json exp": json.Number("1e3")} {
		_, a := build(packGraph(plangraphs.ParamSpec{Name: "a", Kind: plangraphs.KindConst, Value: v}), intent())
		if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrParamSource) {
			t.Fatalf("const %s: err = %v, want ErrParamSource", n, err)
		}
		in := intent()
		in.Slots["amt"] = plangraphs.SlotValue{Resolved: true, Value: v, Kind: plangraphs.SlotKindQuantity}
		_, a = build(packGraph(slotP("a", "amt")), in)
		if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrParamSource) {
			t.Fatalf("slot %s: err = %v, want ErrParamSource", n, err)
		}
	}
	in := intent()
	in.Slots["amt"] = plangraphs.SlotValue{Resolved: true, Value: json.Number("1500"), Kind: plangraphs.SlotKindQuantity}
	_, a := build(packGraph(slotP("a", "amt")), in)
	if out, err := graph(a, req()); err != nil || out.Steps[0].Params["a"] != json.Number("1500") {
		t.Fatalf("an integer is allowed: %v %+v", err, out)
	}
}

func TestBadParamDeclarationsAreRefused(t *testing.T) {
	cases := map[string][]plangraphs.ParamSpec{
		"empty name":        {{Name: "", Kind: plangraphs.KindConst, Value: "x"}},
		"duplicate name":    {{Name: "a", Kind: plangraphs.KindConst, Value: "x"}, {Name: "a", Kind: plangraphs.KindConst, Value: "y"}},
		"nil constant":      {{Name: "a", Kind: plangraphs.KindConst, Value: nil}},
		"non-data const":    {{Name: "a", Kind: plangraphs.KindConst, Value: func() {}}},
		"slot with no name": {{Name: "a", Kind: plangraphs.KindSlot, Slot: ""}},
	}
	for name, ps := range cases {
		_, a := build(packGraph(ps...), intent())
		if _, err := graph(a, req()); !errors.Is(err, trustport.ErrPlanGraphInvalid) {
			t.Fatalf("%s: err = %v, want a refusal", name, err)
		}
	}
}

func TestUnresolvedAndAbsentSlotsAreRefused(t *testing.T) {
	for _, slot := range []string{"amount", "missing"} {
		_, a := build(packGraph(plangraphs.ParamSpec{Name: "x", Kind: plangraphs.KindSlot, Slot: slot, SlotKind: plangraphs.SlotKindQuantity}), intent())
		if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrUnresolvedSlot) || !errors.Is(err, trustport.ErrPlanGraphInvalid) {
			t.Fatalf("slot %q: err = %v, want ErrUnresolvedSlot", slot, err)
		}
	}
}

func TestTargetIsTheIntentsNeverAParam(t *testing.T) {
	_, a := build(packGraph(plangraphs.ParamSpec{Name: "target", Kind: plangraphs.KindSlot, Slot: "reason", SlotKind: plangraphs.SlotKindReference}), intent())
	g, err := graph(a, req())
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
	if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrTargetMismatch) || !errors.Is(err, trustport.ErrPlanGraphInvalid) {
		t.Fatalf("err = %v, want ErrTargetMismatch", err)
	}
	pinned := packGraph()
	pinned.Steps[0].TargetRef = target
	_, a = build(pinned, intent())
	if _, err := graph(a, req()); err != nil {
		t.Fatalf("a pinned target equal to the intent's is allowed: %v", err)
	}
}

func TestWriteWithoutIntentTargetIsRefused(t *testing.T) {
	in := intent()
	in.TargetRef = ""
	_, a := build(packGraph(), in)
	if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrTargetMismatch) {
		t.Fatalf("err = %v, want ErrTargetMismatch", err)
	}
}

func TestDeterministic(t *testing.T) {
	_, a := build(packGraph(plangraphs.ParamSpec{Name: "reason", Kind: plangraphs.KindSlot, Slot: "reason", SlotKind: plangraphs.SlotKindReference}, plangraphs.ParamSpec{Name: "queue", Kind: plangraphs.KindConst, Value: "q"}), intent())
	first, err := graph(a, req())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, err := graph(a, req())
		if err != nil || !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d differs: %v %v", i, err, again)
		}
	}
}

func TestReturnedParamsAreCopies(t *testing.T) {
	nested := map[string]any{"k": []any{"a", map[string]any{"z": "1"}}}
	pg := packGraph(plangraphs.ParamSpec{Name: "c", Kind: plangraphs.KindConst, Value: nested})
	in := intent()
	in.Slots["obj"] = plangraphs.SlotValue{Resolved: true, Value: map[string]any{"n": "1"}, Kind: plangraphs.SlotKindReference}
	pg.Steps[0].Params = append(pg.Steps[0].Params, plangraphs.ParamSpec{Name: "o", Kind: plangraphs.KindSlot, Slot: "obj", SlotKind: plangraphs.SlotKindReference})
	pack, a := build(pg, in)
	g, err := graph(a, req())
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
	again, err := graph(a, req())
	if err != nil || again.Steps[0].Params["c"].(map[string]any)["k"].([]any)[0] != "a" || len(again.Steps) != 2 {
		t.Fatalf("second graph affected by the first's mutation: %v %+v", err, again)
	}
}

func TestOtherIntentNodesGraphIsNotReturned(t *testing.T) {
	other := packGraph()
	other.IntentNode = "fin.intent.other"
	p := &fakePack{g: map[string]plangraphs.PackGraph{node: other, "fin.intent.unknown": other}}
	a := plangraphs.Adapter{Pack: p, Intents: &fakeIntents{in: intent()}, Contexts: fakeContexts{pc: allRunnable}, MaxTextBytes: maxText}
	if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrWrongIntent) {
		t.Fatalf("err = %v, want ErrWrongIntent", err)
	}
	r := req()
	r.IntentNode = "fin.intent.nothing"
	in := intent()
	in.IntentNode = r.IntentNode
	a = plangraphs.Adapter{Pack: p, Intents: &fakeIntents{in: in}, MaxTextBytes: maxText}
	if _, err := graph(a, r); !errors.Is(err, trustport.ErrPlanGraphInvalid) {
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
		if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrWrongIntent) {
			t.Fatalf("%s: err = %v, want ErrWrongIntent", name, err)
		}
	}
}

func TestDependencyErrorsAreNotPlanInvalid(t *testing.T) {
	boom := errors.New("store down")
	a := plangraphs.Adapter{Pack: &fakePack{g: map[string]plangraphs.PackGraph{node: packGraph()}}, Intents: &fakeIntents{err: boom}, MaxTextBytes: maxText}
	if _, err := graph(a, req()); !errors.Is(err, boom) || errors.Is(err, trustport.ErrPlanGraphInvalid) {
		t.Fatalf("intent store error: %v", err)
	}
	a = plangraphs.Adapter{Pack: &fakePack{err: boom}, Intents: &fakeIntents{in: intent()}, MaxTextBytes: maxText}
	if _, err := graph(a, req()); !errors.Is(err, boom) || errors.Is(err, trustport.ErrPlanGraphInvalid) {
		t.Fatalf("pack error: %v", err)
	}
}

func TestEmptyGraphAndUnconfiguredAreRefused(t *testing.T) {
	empty := packGraph()
	empty.Steps = nil
	_, a := build(empty, intent())
	if _, err := graph(a, req()); !errors.Is(err, trustport.ErrPlanGraphInvalid) {
		t.Fatalf("an empty graph for a write: err = %v", err)
	}
	if _, err := (plangraphs.Adapter{}).GraphWith(context.Background(), req(), allRunnable); !errors.Is(err, trustport.ErrConfig) {
		t.Fatalf("unconfigured: err = %v, want ErrConfig", err)
	}
}

func TestReadRequestStillBuildsFromSlotsOnly(t *testing.T) {
	_, a := build(packGraph(plangraphs.ParamSpec{Name: "x", Kind: "TEXT"}), intent())
	r := req()
	r.WriteRequired = false
	if _, err := graph(a, r); !errors.Is(err, plangraphs.ErrParamSource) {
		t.Fatalf("a read gets no looser rule: err = %v", err)
	}
}

func TestSlotParamsNeedASlotName(t *testing.T) {
	_, a := build(packGraph(plangraphs.ParamSpec{Name: "a", Kind: plangraphs.KindSlot, Slot: "", Value: "x"}), intent())
	if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrParamSource) {
		t.Fatalf("slot with no slot name: err = %v, want ErrParamSource", err)
	}
}

// A non-string slot value is never coerced to a key, even when the map has an empty key.
func TestMapNeverCoercesANonStringValue(t *testing.T) {
	p := plangraphs.ParamSpec{Name: "x", Kind: plangraphs.KindMap, SlotKind: plangraphs.SlotKindReference, Slot: "question_type", Map: map[string]string{"": "EMPTY_KEY", "7": "SEVEN"}}
	for _, v := range []any{7, true, nil} {
		_, a := build(packGraph(p), mapIntent(v, true))
		if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrMapValue) {
			t.Fatalf("value %#v: err = %v, want ErrMapValue", v, err)
		}
	}
}

// A454 (interim, relayed, not yet committed; amends A440): the kind and resource kind of a slot come from the NODE'S SLOT declaration (the single authority). The param's
// declaration must EQUAL it, and the resolved value's own kind must equal it. Slot types: Resource, Quantity, Party, TimeRef, Reference; Enum is not one.
var allSlotKinds = []plangraphs.SlotKind{plangraphs.SlotKindResource, plangraphs.SlotKindQuantity, plangraphs.SlotKindParty, plangraphs.SlotKindTimeRef, plangraphs.SlotKindReference}

const enumKind = plangraphs.SlotKind("Enum")

type kinds struct {
	declK, declRK, paramK, paramRK, valK, valRK string
}

func (k kinds) plan(pk plangraphs.ParamKind) (plangraphs.PackGraph, plangraphs.IntentSlots) {
	g := packGraph(plangraphs.ParamSpec{Name: "amount_ref", Kind: pk, Slot: "s", SlotKind: plangraphs.SlotKind(k.paramK), ResourceKind: k.paramRK, Map: map[string]string{"x": "X"}})
	g.Slots = map[string]plangraphs.SlotDecl{"s": {Kind: plangraphs.SlotKind(k.declK), ResourceKind: k.declRK}}
	in := intent()
	in.Slots["s"] = plangraphs.SlotValue{Resolved: true, Value: "x", Kind: plangraphs.SlotKind(k.valK), ResourceKind: k.valRK}
	return g, in
}

func same(k plangraphs.SlotKind) kinds {
	rk := ""
	if k == plangraphs.SlotKindResource {
		rk = "account"
	}
	return kinds{string(k), rk, string(k), rk, string(k), rk}
}

func planKinds(k kinds, pk plangraphs.ParamKind) (trustport.PlanGraph, error) {
	g, in := k.plan(pk)
	_, a := build(g, in)
	return graph(a, req())
}

func TestSlotKindsAllEqualPlans(t *testing.T) {
	for _, k := range allSlotKinds {
		for _, pk := range []plangraphs.ParamKind{plangraphs.KindSlot, plangraphs.KindMap} {
			want := "x"
			if pk == plangraphs.KindMap {
				want = "X"
			}
			if g, err := planKinds(same(k), pk); err != nil || g.Steps[0].Params["amount_ref"] != want {
				t.Fatalf("%s kind %s: %v %+v", pk, k, err, g)
			}
		}
	}
}

// The slot's declaration is the authority: a value of another kind, or none, is refused (ErrSlotKind naming the param), whatever the param declares.
func TestValueOfAnotherKindThanTheSlotIsRefused(t *testing.T) {
	for _, pk := range []plangraphs.ParamKind{plangraphs.KindSlot, plangraphs.KindMap} {
		for _, decl := range allSlotKinds {
			for _, have := range append([]plangraphs.SlotKind{""}, allSlotKinds...) {
				if decl == have {
					continue
				}
				k := same(decl)
				k.valK = string(have)
				g, err := planKinds(k, pk)
				if !errors.Is(err, plangraphs.ErrSlotKind) || !errors.Is(err, trustport.ErrPlanGraphInvalid) || !strings.Contains(err.Error(), "amount_ref") {
					t.Fatalf("%s slot %q value %q: err = %v, want ErrSlotKind naming the param", pk, decl, have, err)
				}
				if len(g.Steps) != 0 {
					t.Fatalf("a refusal returns no step: %+v", g)
				}
			}
		}
	}
}

// Defence in depth: a param whose declaration differs from the node slot's, or is missing, is refused even when the value matches the slot.
func TestParamDeclarationMustEqualTheSlots(t *testing.T) {
	for _, pk := range []plangraphs.ParamKind{plangraphs.KindSlot, plangraphs.KindMap} {
		for _, decl := range allSlotKinds {
			for _, p := range append([]plangraphs.SlotKind{"", "resource", "Free"}, allSlotKinds...) {
				if decl == p {
					continue
				}
				k := same(decl)
				k.paramK = string(p)
				if _, err := planKinds(k, pk); !errors.Is(err, plangraphs.ErrSlotKind) {
					t.Fatalf("%s slot %q param %q: err = %v, want ErrSlotKind", pk, decl, p, err)
				}
			}
		}
	}
}

// The planner does not read the kind from the param: a param and a value that agree with each other but not with the node slot are refused.
func TestParamAndValueAgreeButSlotDiffersIsRefused(t *testing.T) {
	for _, pk := range []plangraphs.ParamKind{plangraphs.KindSlot, plangraphs.KindMap} {
		for _, decl := range allSlotKinds {
			for _, other := range allSlotKinds {
				if decl == other {
					continue
				}
				k := same(other)
				k.declK, k.declRK = string(decl), ""
				if decl == plangraphs.SlotKindResource {
					k.declRK = "account"
				}
				if _, err := planKinds(k, pk); !errors.Is(err, plangraphs.ErrSlotKind) {
					t.Fatalf("%s slot %q param and value %q: err = %v, want ErrSlotKind", pk, decl, other, err)
				}
			}
		}
	}
}

func TestSlotWithNoOrInvalidDeclarationIsRefused(t *testing.T) {
	for _, pk := range []plangraphs.ParamKind{plangraphs.KindSlot, plangraphs.KindMap} {
		for _, d := range []plangraphs.SlotKind{"", enumKind, "resource", "Free"} {
			k := kinds{string(d), "", string(d), "", string(d), ""}
			if _, err := planKinds(k, pk); !errors.Is(err, plangraphs.ErrSlotKind) {
				t.Fatalf("%s declared kind %q: err = %v, want ErrSlotKind", pk, d, err)
			}
		}
		g, in := same(plangraphs.SlotKindReference).plan(pk)
		g.Slots = nil
		_, a := build(g, in)
		if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrSlotKind) {
			t.Fatalf("%s no declaration for the slot: err = %v, want ErrSlotKind", pk, err)
		}
	}
}

func TestResourceKindComesFromTheSlotAndMustMatch(t *testing.T) {
	R := string(plangraphs.SlotKindResource)
	for n, k := range map[string]kinds{
		"value other":        {R, "account", R, "account", R, "card"},
		"value none":         {R, "account", R, "account", R, ""},
		"param other":        {R, "account", R, "card", R, "account"},
		"param none":         {R, "account", R, "", R, "account"},
		"slot none":          {R, "", R, "account", R, "account"},
		"slot none all none": {R, "", R, "", R, ""},
		"all none but value": {R, "", R, "", R, "account"},
	} {
		for _, pk := range []plangraphs.ParamKind{plangraphs.KindSlot, plangraphs.KindMap} {
			if _, err := planKinds(k, pk); !errors.Is(err, plangraphs.ErrSlotKind) {
				t.Fatalf("%s %s: err = %v, want ErrSlotKind", pk, n, err)
			}
		}
	}
}

// AGENT-13 and the Security Reviewer's condition on noema#762: text from a slot has no digit run over four and a bound.
func TestSlotTextIsBoundedAndHasNoLongDigitRun(t *testing.T) {
	bad := map[string]any{
		"five digits":     "ref 12345",
		"card number":     "4111111111111111",
		"digits in text":  "pay to 0123456789 now",
		"nested in map":   map[string]any{"k": []any{"ok", "99999"}},
		"over the bound":  strings.Repeat("a", maxText+1),
		"multibyte bound": strings.Repeat("\u00e9", maxText/2+1),
	}
	for n, v := range bad {
		_, a := build(packGraph(refP("amount_ref", "obj")), objIntent(v))
		if _, err := graph(a, req()); !errors.Is(err, plangraphs.ErrFreeText) || !errors.Is(err, trustport.ErrPlanGraphInvalid) {
			t.Fatalf("%s: err = %v, want ErrFreeText", n, err)
		}
	}
	for n, v := range map[string]any{"four digits": "ref 1234 and 5678", "at the bound": strings.Repeat("a", maxText), "date": "2026-11-20"} {
		_, a := build(packGraph(refP("amount_ref", "obj")), objIntent(v))
		if _, err := graph(a, req()); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
	}
}

// Pack constants and run-time facts are the pack's and the platform's own values, not slot text: the text rule does not apply to them.
func TestTextRuleAppliesToSlotsOnly(t *testing.T) {
	_, a := build(packGraph(plangraphs.ParamSpec{Name: "c", Kind: plangraphs.KindConst, Value: "case 123456789"}), intent())
	if _, err := graph(a, req()); err != nil {
		t.Fatalf("const: %v", err)
	}
}

func TestTextBoundHasNoDefault(t *testing.T) {
	for _, n := range []int{0, -1} {
		_, a := build(packGraph(), intent())
		a.MaxTextBytes = n
		if _, err := graph(a, req()); !errors.Is(err, trustport.ErrConfig) {
			t.Fatalf("bound %d: err = %v, want ErrConfig", n, err)
		}
	}
}

func refP(name, s string) plangraphs.ParamSpec {
	return plangraphs.ParamSpec{Name: name, Kind: plangraphs.KindSlot, Slot: s, SlotKind: plangraphs.SlotKindReference}
}

func objIntent(v any) plangraphs.IntentSlots {
	in := intent()
	in.Slots["obj"] = plangraphs.SlotValue{Resolved: true, Value: v, Kind: plangraphs.SlotKindReference}
	return in
}

// A slot that is not a Resource declares no resource kind, even when the param and the value repeat it.
func TestNonResourceSlotWithAResourceKindIsRefused(t *testing.T) {
	for _, k := range allSlotKinds {
		if k == plangraphs.SlotKindResource {
			continue
		}
		if _, err := planKinds(kinds{string(k), "account", string(k), "account", string(k), "account"}, plangraphs.KindSlot); !errors.Is(err, plangraphs.ErrSlotKind) {
			t.Fatalf("kind %s: err = %v, want ErrSlotKind", k, err)
		}
	}
}
