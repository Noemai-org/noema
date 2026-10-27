package bridge_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// recorder is a Case that remembers the calls it received and answers as told. It is used only to check what the
// bridge sends and how it reports the answer; how the real adapter behaves under faults is the adapter's own
// contract tests and the end-to-end run against mock-case.
type recorder struct {
	creates []adapters.CreateCaseInput
	routes  []adapters.RouteCaseInput
	write   adapters.WriteResult
	get     adapters.CaseRecord
	getErr  error
	page    adapters.CasePage
	findErr error
	finds   []adapters.CaseQuery
}

func (r *recorder) CreateCase(_ context.Context, in adapters.CreateCaseInput) adapters.WriteResult {
	r.creates = append(r.creates, in)
	return r.write
}
func (r *recorder) RouteCase(_ context.Context, in adapters.RouteCaseInput) adapters.WriteResult {
	r.routes = append(r.routes, in)
	return r.write
}
func (r *recorder) GetCase(context.Context, string, string) (adapters.CaseRecord, error) {
	return r.get, r.getErr
}
func (r *recorder) FindCases(_ context.Context, q adapters.CaseQuery) (adapters.CasePage, error) {
	r.finds = append(r.finds, q)
	return r.page, r.findErr
}

func createReq() controller.DispatchRequest {
	return controller.DispatchRequest{TenantID: "ten_aa", GrantID: "grt_1", PrincipalID: "usr_100", IdempotencyKey: "idk_v1_abc", Attempt: 1,
		CapabilityID: bridge.CapCreateEscalationCase, TargetRef: "TX1", TimeoutMS: 1500,
		Params: map[string]any{"transaction_ref": "TX1", "reason_code": "CUSTOMER_DISPUTE_UNRECOGNIZED", "created_by_intent": "int_01", "epoch": int64(1)}}
}

func routeReq() controller.DispatchRequest {
	return controller.DispatchRequest{TenantID: "ten_aa", GrantID: "grt_2", PrincipalID: "usr_100", IdempotencyKey: "idk_v1_def", Attempt: 1,
		CapabilityID: bridge.CapRouteToInternalOwner, TargetRef: "case_01", TimeoutMS: 1500, Params: map[string]any{"owner_queue": "disputes_l2"},
		ExpectedFrom: []controller.TargetVersion{{ResourceRef: "case:case_01", Source: "mock-case", VersionToken: "v7"}}}
}

// REQ-EXE-002, REQ-ENV-001: create sends the controller's key, the grant's principal and the plan's parameters, once.
func TestCreateSendsTheKeyThePrincipalAndTheParametersOnce(t *testing.T) {
	r := &recorder{write: adapters.WriteResult{Class: adapters.ClassAck2xx, ExternalAck: "CMP-1", RawCode: "201"}}
	got := bridge.CaseDispatcher{Case: r}.Dispatch(context.Background(), createReq())
	if got.Class != controller.TransportAck2xx || got.ExternalAck != "CMP-1" || got.RawCode != "201" || got.KeyCollision {
		t.Fatalf("result %+v", got)
	}
	if len(r.creates) != 1 || len(r.routes) != 0 {
		t.Fatalf("calls: %d creates, %d routes; want one create", len(r.creates), len(r.routes))
	}
	in := r.creates[0]
	if in.IdempotencyKey != "idk_v1_abc" || in.TenantID != "ten_aa" || in.PrincipalID != "usr_100" || in.TransactionRef != "TX1" ||
		in.ReasonCode != "CUSTOMER_DISPUTE_UNRECOGNIZED" || in.CreatedByIntent != "int_01" || in.Epoch != 1 || in.Timeout != 1500*time.Millisecond {
		t.Fatalf("create input %+v", in)
	}
}

// REQ-EXE-004: a route is a conditional write; it sends the expected version token from the grant.
func TestRouteSendsTheExpectedVersionToken(t *testing.T) {
	r := &recorder{write: adapters.WriteResult{Class: adapters.ClassVersionConflict, RawCode: "409:VERSION_MISMATCH", CurrentVersionToken: "v8"}}
	got := bridge.CaseDispatcher{Case: r}.Dispatch(context.Background(), routeReq())
	if got.Class != controller.TransportVersionConflict || got.RawCode != "409:VERSION_MISMATCH" {
		t.Fatalf("result %+v", got)
	}
	if len(r.routes) != 1 || r.routes[0].ExpectedVersionToken != "v7" || r.routes[0].CaseID != "case_01" || r.routes[0].OwnerQueue != "disputes_l2" || r.routes[0].TenantID != "ten_aa" {
		t.Fatalf("route input %+v", r.routes)
	}
}

// EC-E06: the key collision flag crosses the bridge, so the controller can raise the security event.
func TestKeyCollisionCrossesTheBridge(t *testing.T) {
	r := &recorder{write: adapters.WriteResult{Class: adapters.ClassRejected4xx, RawCode: "409:IDEMPOTENCY_CONFLICT", KeyCollision: true}}
	if got := (bridge.CaseDispatcher{Case: r}).Dispatch(context.Background(), createReq()); !got.KeyCollision || got.Class != controller.TransportRejected4xx {
		t.Fatalf("result %+v", got)
	}
}

// Every class the adapter can report is a class the controller knows, and silence stays silence (REQ-EXE-005).
func TestEveryAdapterClassIsAControllerClass(t *testing.T) {
	known := map[controller.TransportClass]bool{}
	for _, c := range []controller.TransportClass{controller.TransportAck2xx, controller.TransportVersionConflict, controller.TransportDuplicate409,
		controller.TransportDuplicateCreate, controller.TransportRejected4xx, controller.TransportTimeout, controller.TransportConnectionReset, controller.TransportServer5xx,
		controller.TransportNoResponse, controller.TransportDefect} {
		known[c] = true
	}
	for _, c := range []adapters.TransportClass{adapters.ClassAck2xx, adapters.ClassVersionConflict, adapters.ClassDuplicate409, adapters.ClassDuplicateCreate, adapters.ClassRejected4xx,
		adapters.ClassTimeout, adapters.ClassConnectionReset, adapters.ClassServer5xx, adapters.ClassNoResponse, adapters.ClassDefect} {
		r := &recorder{write: adapters.WriteResult{Class: c}}
		got := bridge.CaseDispatcher{Case: r}.Dispatch(context.Background(), createReq())
		if !known[got.Class] || string(got.Class) != string(c) {
			t.Errorf("adapter class %q became %q", c, got.Class)
		}
	}
}

// REQ-CAP-002, REQ-EXE-004: what has no mapping, or is malformed, makes no call at all.
func TestDefectsMakeNoAdapterCall(t *testing.T) {
	cases := map[string]func(*controller.DispatchRequest){
		"a capability with no adapter": func(r *controller.DispatchRequest) { r.CapabilityID = "fin.cap.transfer_funds" },
		"an unknown parameter on create": func(r *controller.DispatchRequest) {
			r.Params = map[string]any{"transaction_ref": "TX1", "reason_code": "X", "created_by_intent": "i", "epoch": int64(1), "amount_minor": int64(5)}
		},
		"a missing parameter on create":   func(r *controller.DispatchRequest) { delete(r.Params, "reason_code") },
		"an epoch that is not an integer": func(r *controller.DispatchRequest) { r.Params["epoch"] = "1" },
	}
	for name, mutate := range cases {
		r := &recorder{write: adapters.WriteResult{Class: adapters.ClassAck2xx, ExternalAck: "x"}}
		req := createReq()
		mutate(&req)
		got := bridge.CaseDispatcher{Case: r}.Dispatch(context.Background(), req)
		if got.Class != controller.TransportDefect || len(r.creates)+len(r.routes) != 0 {
			t.Errorf("%s: result %+v, calls %d", name, got, len(r.creates)+len(r.routes))
		}
	}
	routeCases := map[string]func(*controller.DispatchRequest){
		"a route with no expected version":   func(r *controller.DispatchRequest) { r.ExpectedFrom = nil },
		"a route with an empty version":      func(r *controller.DispatchRequest) { r.ExpectedFrom[0].VersionToken = "" },
		"a route with two expected versions": func(r *controller.DispatchRequest) { r.ExpectedFrom = append(r.ExpectedFrom, r.ExpectedFrom[0]) },
		"a route with an unknown parameter":  func(r *controller.DispatchRequest) { r.Params["priority"] = "HIGH" },
		"a route with no queue":              func(r *controller.DispatchRequest) { delete(r.Params, "owner_queue") },
		"a route with no case":               func(r *controller.DispatchRequest) { r.TargetRef = "" },
	}
	for name, mutate := range routeCases {
		r := &recorder{write: adapters.WriteResult{Class: adapters.ClassAck2xx, ExternalAck: "x"}}
		req := routeReq()
		mutate(&req)
		got := bridge.CaseDispatcher{Case: r}.Dispatch(context.Background(), req)
		if got.Class != controller.TransportDefect || len(r.creates)+len(r.routes) != 0 {
			t.Errorf("%s: result %+v, calls %d", name, got, len(r.creates)+len(r.routes))
		}
	}
}

func grant() controller.Grant {
	return controller.Grant{TenantID: "ten_aa", PrincipalID: "usr_100", TargetRef: "case_01",
		TargetVersions: []controller.TargetVersion{{ResourceRef: "case:case_01", Source: "mock-case", VersionToken: "v7"}}}
}

// REQ-EXE-003, US-19: the route's revalidation reads the case fresh and compares status and version token.
func TestRouteRevalidation(t *testing.T) {
	step := controller.PlanStep{CapabilityID: bridge.CapRouteToInternalOwner}
	cases := []struct {
		name    string
		rec     adapters.CaseRecord
		err     error
		passed  bool
		facts   []string
		wantErr bool
	}{
		{"unchanged", adapters.CaseRecord{Status: "OPEN", VersionToken: "v7"}, nil, true, nil, false},
		{"already routed, same version", adapters.CaseRecord{Status: "ROUTED", VersionToken: "v7"}, nil, true, nil, false},
		{"version moved", adapters.CaseRecord{Status: "OPEN", VersionToken: "v8"}, nil, false, []string{bridge.FactCaseVersion}, false},
		{"status no longer open or routed", adapters.CaseRecord{Status: "CLOSED", VersionToken: "v7"}, nil, false, []string{bridge.FactCaseStatus}, false},
		{"both changed", adapters.CaseRecord{Status: "CLOSED", VersionToken: "v9"}, nil, false, []string{bridge.FactCaseStatus, bridge.FactCaseVersion}, false},
		{"read unavailable", adapters.CaseRecord{}, &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "503"}, false, nil, true},
	}
	for _, c := range cases {
		got, err := bridge.CaseRevalidator{Case: &recorder{get: c.rec, getErr: c.err}}.Revalidate(context.Background(), grant(), step)
		if (err != nil) != c.wantErr || got.Passed != c.passed || len(got.ChangedFacts) != len(c.facts) {
			t.Errorf("%s: %+v, %v", c.name, got, err)
			continue
		}
		for i := range c.facts {
			if got.ChangedFacts[i] != c.facts[i] {
				t.Errorf("%s: facts %v, want %v", c.name, got.ChangedFacts, c.facts)
			}
		}
	}
	g := grant()
	g.TargetVersions = nil
	if _, err := (bridge.CaseRevalidator{Case: &recorder{}}).Revalidate(context.Background(), g, step); err == nil {
		t.Error("a conditional write without an expected version revalidated")
	}
}

// The create's revalidation: the dedupe read is scoped to the principal and transaction, and it never concludes
// absence from an incomplete page.
func TestCreateRevalidation(t *testing.T) {
	step := controller.PlanStep{CapabilityID: bridge.CapCreateEscalationCase, Params: map[string]any{"transaction_ref": "TX1", "epoch": int64(1)}}
	open := adapters.CaseRecord{Status: "OPEN", Epoch: 1}
	for name, c := range map[string]struct {
		page   adapters.CasePage
		err    error
		passed bool
		isErr  bool
	}{
		"none exist":                    {page: adapters.CasePage{Complete: true}, passed: true},
		"an open case in this epoch":    {page: adapters.CasePage{Complete: true, Items: []adapters.CaseRecord{open}}},
		"a routed case in this epoch":   {page: adapters.CasePage{Complete: true, Items: []adapters.CaseRecord{{Status: "ROUTED", Epoch: 1}}}},
		"an open case of another epoch": {page: adapters.CasePage{Complete: true, Items: []adapters.CaseRecord{{Status: "OPEN", Epoch: 2}}}, passed: true},
		"an incomplete page":            {page: adapters.CasePage{Complete: false}, isErr: true},
		"the read failed":               {err: errors.New("unavailable"), isErr: true},
	} {
		r := &recorder{page: c.page, findErr: c.err}
		got, err := bridge.CaseRevalidator{Case: r}.Revalidate(context.Background(), grant(), step)
		if (err != nil) != c.isErr || got.Passed != c.passed {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
		if len(r.finds) != 1 || r.finds[0].TenantID != "ten_aa" || r.finds[0].PrincipalID != "usr_100" || r.finds[0].TransactionRef != "TX1" {
			t.Errorf("%s: find query %+v", name, r.finds)
		}
	}
	if _, err := (bridge.CaseRevalidator{Case: &recorder{}}).Revalidate(context.Background(), grant(), controller.PlanStep{CapabilityID: "fin.cap.adjust_balance"}); err == nil {
		t.Error("a capability with no mapping revalidated")
	}
}

// The read-back is complete and names exactly one case of this tenant and key; anything else is an error.
func TestReadCaseByKey(t *testing.T) {
	one := adapters.CaseRecord{CaseID: "case_1", Reference: "CMP-1", Status: "OPEN", OwnerQueue: "DISPUTES", VersionToken: "v3", IdempotencyKey: "idk_v1_k", TenantID: "ten_aa"}
	for name, c := range map[string]struct {
		page adapters.CasePage
		err  error
		ok   bool
	}{
		"exactly one":      {page: adapters.CasePage{Complete: true, Items: []adapters.CaseRecord{one}}, ok: true},
		"none":             {page: adapters.CasePage{Complete: true}},
		"incomplete":       {page: adapters.CasePage{Complete: false, Items: []adapters.CaseRecord{one}}},
		"two":              {page: adapters.CasePage{Complete: true, Items: []adapters.CaseRecord{one, one}}},
		"another key":      {page: adapters.CasePage{Complete: true, Items: []adapters.CaseRecord{{CaseID: "c", VersionToken: "v", IdempotencyKey: "idk_v1_other", TenantID: "ten_aa"}}}},
		"another tenant":   {page: adapters.CasePage{Complete: true, Items: []adapters.CaseRecord{{CaseID: "c", VersionToken: "v", IdempotencyKey: "idk_v1_k", TenantID: "ten_bb"}}}},
		"no version token": {page: adapters.CasePage{Complete: true, Items: []adapters.CaseRecord{{CaseID: "c", IdempotencyKey: "idk_v1_k", TenantID: "ten_aa"}}}},
		"the read failed":  {err: &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "503"}},
	} {
		r := &recorder{page: c.page, findErr: c.err}
		got, err := bridge.CaseReadBack{Case: r}.ReadCaseByKey(context.Background(), "ten_aa", "idk_v1_k")
		if (err == nil) != c.ok {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
		if c.ok && (got.CaseID != "case_1" || got.VersionToken != "v3" || got.Reference != "CMP-1") {
			t.Errorf("%s: view %+v", name, got)
		}
		if len(r.finds) != 1 || r.finds[0].IdempotencyKey != "idk_v1_k" || r.finds[0].TenantID != "ten_aa" || r.finds[0].PrincipalID != "" {
			t.Errorf("%s: query %+v, want a scoped find by key only", name, r.finds)
		}
	}
}
