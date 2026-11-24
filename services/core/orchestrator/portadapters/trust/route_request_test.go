package trustport_test

// A639 (interim, relayed, not yet committed) amends this guard NARROWLY, in the same change as the field: RouteRequest gains Session (A625's typed data, the principal and role the
// investigation recorded, handed by the orchestrator) and nothing else; the guard stays exact.
// TRU-14, ruling A504 (interim; noema#785; the Security Reviewer's P2): a grant binds one capability, one target, one parameter hash and the plan step, so the route request carries only
// what that hash binds. RouteRequest keeps case_id and owner_queue, both inside the route step's parameter hash (the pack's graph: case_id is a fact, owner_queue a policy param, and
// nothing else is a parameter); VersionToken is read fresh by Execution at dispatch and CreateReceiptID is never needed (the case_id comes from the verified create outcome), so
// neither is on the request. If another field is ever needed it goes inside the hash with a test, never outside it. REQ-EXE-006, REQ-TRU-002; INV-03, AGENT-08. Values are SYNTHETIC.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const packRoot = "../../../../../packs"

// The request holds the identity the decision is asked under (tenant, correlation), the capability, and the two parameters of the step: nothing the hash does not bind.
func TestRouteRequestHoldsOnlyTheCaseTheQueueAndWhatIdentifiesTheAsk(t *testing.T) { // A504
	typ := reflect.TypeOf(trustport.RouteRequest{})
	var got []string
	for i := 0; i < typ.NumField(); i++ {
		got = append(got, typ.Field(i).Name)
	}
	sort.Strings(got)
	want := []string{"CapabilityID", "CaseID", "CorrelationID", "OwnerQueue", "Session", "TenantID"} // A639: Session is the one typed field A625 names; nothing else
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RouteRequest fields %v, want exactly %v: a field outside the route step's parameter hash is believed decided and is not (A504, A639)", got, want)
	}
	// A639: the one added field is the session the investigation recorded, as the intake's own type (principal and role), and no other type.
	if f, _ := typ.FieldByName("Session"); f.Type != reflect.TypeOf(intake.Session{}) {
		t.Fatalf("RouteRequest.Session is %v, want intake.Session", f.Type)
	}
}

type graphParam struct {
	Name        string `json:"name"`
	Fact        string `json:"fact"`
	PolicyParam string `json:"policy_param"`
	Evidence    bool   `json:"evidence_packet"`
}

func routeNodeParams(t *testing.T) []graphParam {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(packRoot, "fintech", "graph", "g2_escalate.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Nodes []struct {
			NodeID string       `json:"node_id"`
			Params []graphParam `json:"params"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.NodeID == "route_to_internal_owner" {
			return n.Params
		}
	}
	t.Fatal("the pack's escalation graph has no route_to_internal_owner node")
	return nil
}

// Both kept fields are inside the step's parameter hash and nothing else is: the step's parameters are case_id (a fact, which the decider sends) and owner_queue (a policy param, read by
// Trust from the winning PERMIT rule), and the capability's input schema is closed over those two. So the Controller refuses a dispatch whose case or queue is not the one the grant binds.
func TestTheRouteStepsParameterHashBindsTheCaseAndTheQueueAndNothingElse(t *testing.T) { // A504, INV-03
	params := routeNodeParams(t)
	if len(params) != 2 || params[0] != (graphParam{Name: "case_id", Fact: "case_id"}) || params[1] != (graphParam{Name: "owner_queue", PolicyParam: "owner_queue"}) {
		t.Fatalf("the route step's parameters are %+v, want case_id (fact) and owner_queue (policy_param) only", params)
	}
	raw, err := os.ReadFile(filepath.Join(packRoot, "schemas", "fin", "route_to_internal_owner.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Required             []string                   `json:"required"`
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties bool                       `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	sort.Strings(schema.Required)
	if !reflect.DeepEqual(schema.Required, []string{"case_id", "owner_queue"}) || len(schema.Properties) != 2 || schema.AdditionalProperties {
		t.Fatalf("the route capability's input is %v required, %d properties, additionalProperties %v: want a closed object of case_id and owner_queue", schema.Required, len(schema.Properties), schema.AdditionalProperties)
	}
}

// The worker's queue is not a fact Trust is given: the queue in the hash is the policy's, so a worker that names another queue gets a dispatch the grant does not cover.
func TestTheQueueTheWorkerNamesIsNotAFactAndTheCaseIsTheOnlyFact(t *testing.T) { // A504
	for _, queue := range []string{"queue_1", "another_queue", ""} {
		s := &stepSpy{res: withAllow(t, allowed("grt_1"))}
		r := routeReq()
		r.OwnerQueue = queue
		if _, err := decider(s, routeScope()).DecideRoute(ctx(t), r); err != nil {
			t.Fatalf("queue %q: %v", queue, err)
		}
		if len(s.got) != 1 || !reflect.DeepEqual(s.got[0].Facts, map[string]string{"case_id": "cas_1"}) {
			t.Fatalf("queue %q: facts %v, want {case_id: cas_1} only", queue, s.got)
		}
	}
}
