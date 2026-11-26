package main

// STM-01 final composition (A550, interim, relayed, not yet committed; REQ-TRU-009, REQ-INT-001; INV-02, INV-14; AGENT-09): the plan context of a plan request is its own fields, copied, and
// nothing else: the capabilities to plan now, the winning PERMIT rule's params as T11 read them, the facts a continuation learned from verified results, and the evidence packet. The
// adapter is the composition's (plangraphs says so); it reads no store and no pack. SYNTHETIC.

import (
	"context"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

func pcRequest() intake.PlanRequest {
	return intake.PlanRequest{
		TenantID: "ten_aa", IntentID: "int_synthetic1", IntentVersion: 1, IntentNode: "synthetic.node",
		Capabilities:   []string{"synthetic.cap.one", "synthetic.cap.two"},
		PolicyParams:   map[string]map[string]string{"synthetic.cap.one": {"queue": "SYNTHETIC_QUEUE"}},
		Facts:          map[string]string{"case_id": "case_synthetic1"},
		EvidencePacket: "evp_synthetic1",
	}
}

func TestThePlanContextIsTheRequestsOwnFields(t *testing.T) {
	r := pcRequest()
	pc, err := planContextsFromRequest{}.PlanContext(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pc.Runnable, r.Capabilities) || !reflect.DeepEqual(pc.PolicyParams, r.PolicyParams) || !reflect.DeepEqual(pc.Facts, r.Facts) || pc.EvidencePacket != r.EvidencePacket {
		t.Errorf("plan context %+v does not carry the request's capabilities, policy params, facts and evidence packet", pc)
	}
}

// What the plan reads is a copy: a change to the context reaches neither the request nor a later context (INV-02: the planner builds params from data it owns).
func TestThePlanContextIsACopy(t *testing.T) {
	r := pcRequest()
	pc, err := planContextsFromRequest{}.PlanContext(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	pc.Runnable[0] = "synthetic.cap.tampered"
	pc.PolicyParams["synthetic.cap.one"]["queue"] = "TAMPERED"
	pc.Facts["case_id"] = "tampered"
	if !reflect.DeepEqual(r, pcRequest()) {
		t.Errorf("a change to the plan context reached the request: %+v", r)
	}
}

// A request with none of the optional fields gives an empty context, not an error: a read plans no step and needs none.
func TestAPlanRequestWithNoRuntimeFieldsGivesAnEmptyContext(t *testing.T) {
	pc, err := planContextsFromRequest{}.PlanContext(t.Context(), intake.PlanRequest{TenantID: "ten_aa"})
	if err != nil || len(pc.Runnable) != 0 || len(pc.PolicyParams) != 0 || len(pc.Facts) != 0 || pc.EvidencePacket != "" {
		t.Errorf("got %+v, %v; want an empty context and no error", pc, err)
	}
}

// A context that is done is an error, never a context built from a request nobody will use.
func TestADoneContextGivesNoPlanContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (planContextsFromRequest{}).PlanContext(ctx, pcRequest()); err == nil {
		t.Error("a done context gave a plan context")
	}
}
