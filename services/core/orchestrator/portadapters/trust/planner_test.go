package trustport_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"regexp"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/planner"
	"github.com/Noemai-org/noema/services/core/execution/registry"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// SYNTHETIC fixtures only (AGENT-25). Proves REQ-TRU-009; INV-03, INV-16, INV-17, INV-18 (ruling A205 Q3, relayed, not yet committed).

var plannerHashRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type plannerGraphs struct {
	g   trustport.PlanGraph
	err error
	got []intake.PlanRequest
}

func (f *plannerGraphs) Graph(_ context.Context, r intake.PlanRequest) (trustport.PlanGraph, error) {
	f.got = append(f.got, r)
	return f.g, f.err
}

func plannerRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	b, err := os.ReadFile("../../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := registry.Load(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func plannerSteps() []trustport.PlanGraphStep {
	return []trustport.PlanGraphStep{
		{NodeID: "lookup_transaction", CapabilityID: "fin.cap.lookup_transaction", TargetRef: "fin.transaction:TX1", Params: map[string]any{"transaction_id": "txn_01"}},
		{NodeID: "create_escalation_case", CapabilityID: "fin.cap.create_escalation_case", TargetRef: "fin.transaction:TX1", Params: map[string]any{"owner_queue": "DISPUTES", "priority": int64(2)}},
	}
}

func plannerReq(write bool) intake.PlanRequest {
	return intake.PlanRequest{TenantID: "ten_1", CorrelationID: "cor_1", IntentID: "int_1", IntentVersion: 1, IntentHash: "sha256:" + "a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0",
		IntentNode: "dispute_transaction", WriteRequired: write}
}

func plannerWith(t *testing.T, g *plannerGraphs) trustport.PlannerAdapter {
	return trustport.PlannerAdapter{Registry: plannerRegistry(t), Graphs: g}
}

func TestPlannerWriteGivesStepsInOrderWithTheHash(t *testing.T) {
	g := &plannerGraphs{g: trustport.PlanGraph{GraphVersion: "1.0.0", Steps: plannerSteps()}}
	p, err := plannerWith(t, g).Plan(context.Background(), plannerReq(true))
	if err != nil {
		t.Fatal(err)
	}
	if !plannerHashRe.MatchString(p.Hash) || len(p.Steps) != 2 {
		t.Fatalf("plan = %+v", p)
	}
	if p.Steps[0].CapabilityID != "fin.cap.lookup_transaction" || p.Steps[1].CapabilityID != "fin.cap.create_escalation_case" || p.Steps[1].TargetRef != "fin.transaction:TX1" {
		t.Fatalf("steps = %+v", p.Steps)
	}
	for _, s := range p.Steps {
		if !plannerHashRe.MatchString(s.ParamsHash) {
			t.Fatalf("params hash %q", s.ParamsHash)
		}
	}
	if p.Steps[0].ParamsHash == p.Steps[1].ParamsHash {
		t.Fatal("different params, same hash")
	}
	if len(g.got) != 1 || g.got[0].IntentNode != "dispute_transaction" {
		t.Fatalf("graph asked with %+v", g.got)
	}
}

func TestPlannerHashIsThePlannersOwn(t *testing.T) {
	reg := plannerRegistry(t)
	specs := []planner.StepSpec{{NodeID: "lookup_transaction", CapabilityID: "fin.cap.lookup_transaction", Params: map[string]any{"transaction_id": "txn_01"}}}
	want, err := planner.New(reg, "1.0.0", "dispute_transaction", specs)
	if err != nil {
		t.Fatal(err)
	}
	wantHash, _ := want.Hash()
	g := &plannerGraphs{g: trustport.PlanGraph{GraphVersion: "1.0.0", Steps: plannerSteps()[:1]}}
	p, err := plannerWith(t, g).Plan(context.Background(), plannerReq(true))
	if err != nil || p.Hash != wantHash {
		t.Fatalf("hash %q want %q err %v", p.Hash, wantHash, err)
	}
}

func TestPlannerReadHasNoStepsAndAHash(t *testing.T) {
	g := &plannerGraphs{g: trustport.PlanGraph{GraphVersion: "1.0.0", Steps: plannerSteps()}}
	p, err := plannerWith(t, g).Plan(context.Background(), plannerReq(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 0 || !plannerHashRe.MatchString(p.Hash) {
		t.Fatalf("plan = %+v", p)
	}
	w, err := plannerWith(t, g).Plan(context.Background(), plannerReq(true))
	if err != nil || w.Hash == p.Hash {
		t.Fatalf("a read and a write share a hash: %q err %v", p.Hash, err)
	}
}

func TestPlannerIsDeterministic(t *testing.T) {
	g := &plannerGraphs{g: trustport.PlanGraph{GraphVersion: "1.0.0", Steps: plannerSteps()}}
	a, errA := plannerWith(t, g).Plan(context.Background(), plannerReq(true))
	b, errB := plannerWith(t, g).Plan(context.Background(), plannerReq(true))
	if errA != nil || errB != nil || a.Hash != b.Hash {
		t.Fatalf("%q %q %v %v", a.Hash, b.Hash, errA, errB)
	}
	r1, _ := plannerWith(t, g).Plan(context.Background(), plannerReq(false))
	r2, _ := plannerWith(t, g).Plan(context.Background(), plannerReq(false))
	if r1.Hash == "" || r1.Hash != r2.Hash {
		t.Fatalf("read hashes %q %q", r1.Hash, r2.Hash)
	}
}

func TestPlannerRefusesWhatCannotBePlanned(t *testing.T) {
	step := func(id string) []trustport.PlanGraphStep {
		return []trustport.PlanGraphStep{{NodeID: "n1", CapabilityID: id, TargetRef: "fin.transaction:TX1", Params: map[string]any{}}}
	}
	cases := map[string]trustport.PlanGraph{
		"unregistered": {GraphVersion: "1.0.0", Steps: step("fin.cap.does_not_exist")},
		"prohibited":   {GraphVersion: "1.0.0", Steps: step("fin.cap.transfer_funds")},
		"float param":  {GraphVersion: "1.0.0", Steps: []trustport.PlanGraphStep{{NodeID: "n1", CapabilityID: "fin.cap.lookup_transaction", TargetRef: "x", Params: map[string]any{"a": 1.5}}}},
		"no steps":     {GraphVersion: "1.0.0"},
		"no version":   {Steps: plannerSteps()},
		"a cycle":      {GraphVersion: "1.0.0", Steps: append(plannerSteps(), plannerSteps()[0])},
	}
	for name, graph := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := plannerWith(t, &plannerGraphs{g: graph}).Plan(context.Background(), plannerReq(true))
			if !errors.Is(err, intake.ErrPlanInvalid) || p.Hash != "" || len(p.Steps) != 0 {
				t.Fatalf("plan %+v err %v", p, err)
			}
		})
	}
}

func TestPlannerGraphRefusalIsPlanInvalidAndOtherErrorsAreNot(t *testing.T) {
	_, err := plannerWith(t, &plannerGraphs{err: trustport.ErrPlanGraphInvalid}).Plan(context.Background(), plannerReq(true))
	if !errors.Is(err, intake.ErrPlanInvalid) {
		t.Fatalf("err = %v", err)
	}
	boom := errors.New("graph store down")
	_, err = plannerWith(t, &plannerGraphs{err: boom}).Plan(context.Background(), plannerReq(false))
	if !errors.Is(err, boom) || errors.Is(err, intake.ErrPlanInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestPlannerErrorThatIsNotARefusalIsADependencyFailure(t *testing.T) {
	boom := errors.New("planner exploded")
	a := plannerWith(t, &plannerGraphs{g: trustport.PlanGraph{GraphVersion: "1.0.0", Steps: plannerSteps()}})
	a.Build = func(*registry.Registry, string, string, []planner.StepSpec) (planner.Plan, error) {
		return planner.Plan{}, boom
	}
	_, err := a.Plan(context.Background(), plannerReq(true))
	if !errors.Is(err, boom) || errors.Is(err, intake.ErrPlanInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestPlannerIsNotConfiguredWithoutItsParts(t *testing.T) {
	g := &plannerGraphs{g: trustport.PlanGraph{GraphVersion: "1.0.0", Steps: plannerSteps()}}
	for name, a := range map[string]trustport.PlannerAdapter{
		"no registry": {Graphs: g},
		"no graphs":   {Registry: plannerRegistry(t)},
	} {
		if _, err := a.Plan(context.Background(), plannerReq(true)); !errors.Is(err, trustport.ErrConfig) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}
