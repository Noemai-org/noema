package bridge_test

// EXE-05 slice 2, ruling A157 (the complaint again from EXE-09, ruling A185): the bridge dispatches the two append-only capabilities of mock-case, record_investigation_evidence and log_complaint_reference,
// through adapters.CaseRecords, and revalidates them (E7) with the reads their descriptors name. REQ-EXE-003, REQ-EXE-004, REQ-EXE-006, REQ-CAP-002, INV-03.
// The adapter's own behaviour under faults is its contract tests and the run against the real mock-case; here the bridge's mapping and refusals.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
)

type recordsRecorder struct {
	evidence   []adapters.AppendEvidenceInput
	complaints []adapters.LogComplaintInput
	write      adapters.WriteResult
	cpage      adapters.ComplaintPage
	cerr       error
	cfinds     []adapters.RecordQuery
}

func (r *recordsRecorder) AppendEvidence(_ context.Context, in adapters.AppendEvidenceInput) adapters.WriteResult {
	r.evidence = append(r.evidence, in)
	return r.write
}
func (r *recordsRecorder) LogComplaint(_ context.Context, in adapters.LogComplaintInput) adapters.WriteResult {
	r.complaints = append(r.complaints, in)
	return r.write
}
func (r *recordsRecorder) GetEvidence(context.Context, string, string) (adapters.EvidenceRecord, error) {
	return adapters.EvidenceRecord{}, errors.New("not used")
}
func (r *recordsRecorder) GetComplaint(context.Context, string, string) (adapters.ComplaintRecord, error) {
	return adapters.ComplaintRecord{}, errors.New("not used")
}
func (r *recordsRecorder) FindEvidence(context.Context, adapters.RecordQuery) (adapters.EvidencePage, error) {
	return adapters.EvidencePage{}, errors.New("not used")
}
func (r *recordsRecorder) FindComplaints(_ context.Context, q adapters.RecordQuery) (adapters.ComplaintPage, error) {
	r.cfinds = append(r.cfinds, q)
	return r.cpage, r.cerr
}

func evidenceReq() controller.DispatchRequest {
	return controller.DispatchRequest{TenantID: "ten_aa", GrantID: "grt_3", PrincipalID: "usr_100", IdempotencyKey: "idk_v1_ev", Attempt: 1,
		CapabilityID: bridge.CapRecordInvestigationEvidence, TargetRef: "case_01", TimeoutMS: 1500,
		Params: map[string]any{"case_id": "case_01", "payload": `{"note":"card used at 14:02"}`}}
}

func complaintReq() controller.DispatchRequest {
	return controller.DispatchRequest{TenantID: "ten_aa", GrantID: "grt_4", PrincipalID: "usr_100", IdempotencyKey: "idk_v1_cm", Attempt: 1,
		CapabilityID: bridge.CapLogComplaintReference, TargetRef: "case_01", TimeoutMS: 1500, Params: map[string]any{"case_id": "case_01"}}
}

var ack = adapters.WriteResult{Class: adapters.ClassAck2xx, ExternalAck: "att_1", RawCode: "201"}

// REQ-EXE-006: evidence is one keyed append with the grant's tenant, the plan's case and payload and the capability's timeout; the target's reference is the ack.
func TestEvidenceIsOneKeyedAppendWithThePlansParameters(t *testing.T) {
	rec := &recordsRecorder{write: ack}
	got := bridge.CaseDispatcher{Case: &recorder{}, Records: rec}.Dispatch(context.Background(), evidenceReq())
	if got.Class != controller.TransportAck2xx || got.ExternalAck != "att_1" || got.RawCode != "201" {
		t.Fatalf("result %+v", got)
	}
	if len(rec.evidence) != 1 || len(rec.complaints) != 0 {
		t.Fatalf("calls: %d evidence, %d complaints", len(rec.evidence), len(rec.complaints))
	}
	in := rec.evidence[0]
	if in.IdempotencyKey != "idk_v1_ev" || in.TenantID != "ten_aa" || in.CaseID != "case_01" || in.Payload != `{"note":"card used at 14:02"}` || in.Timeout != 1500*time.Millisecond {
		t.Fatalf("input %+v", in)
	}
}

// EXE-09, A185: log_complaint_reference is dispatched again, now that mock-case creates a complaint conditionally and atomically (noema#537). It is one keyed
// conditional create with the grant's tenant, the plan's case and the capability's timeout, and nothing else crosses: the descriptor's input has the case only. The
// target is the guard against a second complaint; E7 (below) is the first line. REQ-EXE-003, REQ-EXE-006, REQ-CAP-002, INV-03.
func TestAComplaintIsOneKeyedConditionalCreateWithThePlansCase(t *testing.T) {
	rec := &recordsRecorder{write: adapters.WriteResult{Class: adapters.ClassAck2xx, ExternalAck: "cmp_1", RawCode: "201"}}
	got := bridge.CaseDispatcher{Case: &recorder{}, Records: rec}.Dispatch(context.Background(), complaintReq())
	if got.Class != controller.TransportAck2xx || got.ExternalAck != "cmp_1" || got.RawCode != "201" {
		t.Fatalf("result %+v", got)
	}
	if len(rec.complaints) != 1 || len(rec.evidence) != 0 {
		t.Fatalf("calls: %d complaints, %d evidence", len(rec.complaints), len(rec.evidence))
	}
	in := rec.complaints[0]
	if in.IdempotencyKey != "idk_v1_cm" || in.TenantID != "ten_aa" || in.CaseID != "case_01" || in.Timeout != 1500*time.Millisecond {
		t.Fatalf("input %+v", in)
	}
}

// A185, EC-E06: the target refused the conditional create because the case already has a complaint under another key. That crosses the bridge as the class the
// controller reads as a business rejection (REJECTED_BY_TARGET): never an acknowledgement, never the existing reference as this write's ack, never a retry.
func TestARefusedComplaintCrossesTheBridgeAsADuplicateCreateAndAdoptsNothing(t *testing.T) {
	rec := &recordsRecorder{write: adapters.WriteResult{Class: adapters.ClassDuplicateCreate, RawCode: "412:PRECONDITION_FAILED", ExistingComplaintRef: "cmp_9"}}
	got := bridge.CaseDispatcher{Case: &recorder{}, Records: rec}.Dispatch(context.Background(), complaintReq())
	if got.Class != controller.TransportDuplicateCreate || got.RawCode != "412:PRECONDITION_FAILED" || got.ExternalAck != "" {
		t.Fatalf("result %+v", got)
	}
	if controller.Classify(got) != controller.StateRejectedByTarget {
		t.Fatalf("state %s, want REJECTED_BY_TARGET", controller.Classify(got))
	}
	if len(rec.complaints) != 1 {
		t.Fatalf("%d calls, want exactly one: a refused create is never retried (AGENT-08)", len(rec.complaints))
	}
}

// EC-E06: a key collision crosses the bridge for evidence, so the controller can raise the security event.
func TestKeyCollisionCrossesTheBridgeForEvidence(t *testing.T) {
	rec := &recordsRecorder{write: adapters.WriteResult{Class: adapters.ClassRejected4xx, RawCode: "409:IDEMPOTENCY_CONFLICT", KeyCollision: true}}
	req := evidenceReq()
	if got := (bridge.CaseDispatcher{Case: &recorder{}, Records: rec}).Dispatch(context.Background(), req); !got.KeyCollision || got.Class != controller.TransportRejected4xx {
		t.Errorf("%s: %+v", req.CapabilityID, got)
	}
}

// REQ-CAP-002, REQ-EXE-004: a parameter the descriptor's schema does not name, a missing one, a case that is not the grant's target, an empty payload and no
// records adapter at all are adapter defects, and none of them makes a call.
func TestRecordDefectsMakeNoAdapterCall(t *testing.T) {
	type tc struct {
		name   string
		req    func() controller.DispatchRequest
		mutate func(*controller.DispatchRequest)
		raw    string
	}
	for _, c := range []tc{
		{"evidence, an unknown parameter", evidenceReq, func(r *controller.DispatchRequest) { r.Params["payload_hash"] = "sha256:00" }, "UNKNOWN_PARAMETER"},
		{"evidence, no payload", evidenceReq, func(r *controller.DispatchRequest) { delete(r.Params, "payload") }, "MISSING_PARAMETER"},
		{"evidence, an empty payload", evidenceReq, func(r *controller.DispatchRequest) { r.Params["payload"] = "" }, "MISSING_PARAMETER"},
		{"evidence, a payload that is not a string", evidenceReq, func(r *controller.DispatchRequest) { r.Params["payload"] = 7 }, "MISSING_PARAMETER"},
		{"evidence, no case", evidenceReq, func(r *controller.DispatchRequest) { delete(r.Params, "case_id") }, "MISSING_PARAMETER"},
		{"evidence, another case than the grant's target", evidenceReq, func(r *controller.DispatchRequest) { r.Params["case_id"] = "case_02" }, "TARGET_MISMATCH"},
		{"complaint, an unknown parameter", complaintReq, func(r *controller.DispatchRequest) { r.Params["note"] = "x" }, "UNKNOWN_PARAMETER"},
		{"complaint, no case", complaintReq, func(r *controller.DispatchRequest) { delete(r.Params, "case_id") }, "MISSING_PARAMETER"},
		{"complaint, another case than the grant's target", complaintReq, func(r *controller.DispatchRequest) { r.Params["case_id"] = "case_02" }, "TARGET_MISMATCH"},
	} {
		rec := &recordsRecorder{write: ack}
		req := c.req()
		c.mutate(&req)
		got := bridge.CaseDispatcher{Case: &recorder{}, Records: rec}.Dispatch(context.Background(), req)
		if got.Class != controller.TransportDefect || got.RawCode != c.raw || len(rec.evidence)+len(rec.complaints) != 0 {
			t.Errorf("%s: %+v, calls %d", c.name, got, len(rec.evidence)+len(rec.complaints))
		}
	}
	for name, req := range map[string]controller.DispatchRequest{"evidence": evidenceReq(), "complaint": complaintReq()} {
		if got := (bridge.CaseDispatcher{Case: &recorder{}}).Dispatch(context.Background(), req); got.Class != controller.TransportDefect || got.RawCode != "NO_ADAPTER_FOR_CAPABILITY" {
			t.Errorf("%s with no records adapter: %+v", name, got)
		}
	}
}

func openCase() adapters.CaseRecord {
	return adapters.CaseRecord{CaseID: "case_01", Status: "OPEN", VersionToken: "v1", TenantID: "ten_aa"}
}

func recordGrant() controller.Grant {
	return controller.Grant{TenantID: "ten_aa", GrantID: "grt_3", PrincipalID: "usr_100", TargetRef: "case_01"}
}

// REQ-EXE-003: evidence revalidates the case status (the descriptor's one read): the case must exist and be open or routed. A read that cannot be made is an error.
func TestEvidenceRevalidation(t *testing.T) {
	step := controller.PlanStep{CapabilityID: bridge.CapRecordInvestigationEvidence, Params: evidenceReq().Params}
	for name, c := range map[string]struct {
		get     adapters.CaseRecord
		getErr  error
		passed  bool
		facts   []string
		wantErr bool
	}{
		"open":         {get: openCase(), passed: true},
		"routed":       {get: adapters.CaseRecord{CaseID: "case_01", Status: "ROUTED", VersionToken: "v2"}, passed: true},
		"closed":       {get: adapters.CaseRecord{CaseID: "case_01", Status: "CLOSED", VersionToken: "v3"}, facts: []string{bridge.FactCaseStatus}},
		"unreadable":   {getErr: &adapters.ReadError{Failure: adapters.ReadUnavailable}, wantErr: true},
		"unknown case": {getErr: &adapters.ReadError{Failure: adapters.ReadNotFound}, wantErr: true},
	} {
		r := &recorder{get: c.get, getErr: c.getErr}
		got, err := bridge.CaseRevalidator{Case: r}.Revalidate(context.Background(), recordGrant(), step)
		if c.wantErr != (err != nil) || (err == nil && (got.Passed != c.passed || !sameFacts(got.ChangedFacts, c.facts))) {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
}

// REQ-EXE-003, A185: the complaint revalidates the two reads its descriptor names: the case is open or routed, and the case holds no complaint. This is the first line
// only; the target's conditional create is the guard, because a rival can write between E7 and E8. A read that cannot be made, a page that is not complete and no
// records adapter are errors (the controller voids the grant): absence is never concluded from what was not read.
func TestComplaintRevalidation(t *testing.T) {
	step := controller.PlanStep{CapabilityID: bridge.CapLogComplaintReference, Params: complaintReq().Params}
	held := adapters.ComplaintPage{Items: []adapters.ComplaintRecord{{ComplaintRef: "cmp_1", CaseID: "case_01", TenantID: "ten_aa"}}, Complete: true}
	for name, c := range map[string]struct {
		get     adapters.CaseRecord
		getErr  error
		page    adapters.ComplaintPage
		findErr error
		noRecs  bool
		passed  bool
		facts   []string
		wantErr bool
	}{
		"open, no complaint":            {get: openCase(), page: adapters.ComplaintPage{Complete: true}, passed: true},
		"routed, no complaint":          {get: adapters.CaseRecord{CaseID: "case_01", Status: "ROUTED", VersionToken: "v2"}, page: adapters.ComplaintPage{Complete: true}, passed: true},
		"a complaint exists":            {get: openCase(), page: held, facts: []string{bridge.FactExistingComplaint}},
		"closed":                        {get: adapters.CaseRecord{CaseID: "case_01", Status: "CLOSED", VersionToken: "v3"}, page: adapters.ComplaintPage{Complete: true}, facts: []string{bridge.FactCaseStatus}},
		"closed and a complaint exists": {get: adapters.CaseRecord{CaseID: "case_01", Status: "CLOSED", VersionToken: "v3"}, page: held, facts: []string{bridge.FactCaseStatus, bridge.FactExistingComplaint}},
		"an incomplete page":            {get: openCase(), page: adapters.ComplaintPage{Complete: false}, wantErr: true},
		"the complaints cannot be read": {get: openCase(), findErr: &adapters.ReadError{Failure: adapters.ReadUnavailable}, wantErr: true},
		"the case cannot be read":       {getErr: &adapters.ReadError{Failure: adapters.ReadUnavailable}, page: adapters.ComplaintPage{Complete: true}, wantErr: true},
		"no records adapter":            {get: openCase(), noRecs: true, wantErr: true},
	} {
		rec := &recordsRecorder{cpage: c.page, cerr: c.findErr}
		rv := bridge.CaseRevalidator{Case: &recorder{get: c.get, getErr: c.getErr}, Records: rec}
		if c.noRecs {
			rv = bridge.CaseRevalidator{Case: &recorder{get: c.get, getErr: c.getErr}}
		}
		got, err := rv.Revalidate(context.Background(), recordGrant(), step)
		if c.wantErr != (err != nil) || (err == nil && (got.Passed != c.passed || !sameFacts(got.ChangedFacts, c.facts))) {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
		if err == nil && len(rec.cfinds) == 1 && (rec.cfinds[0].TenantID != "ten_aa" || rec.cfinds[0].CaseID != "case_01") {
			t.Errorf("%s: the complaints were read with %+v, want the grant's tenant and target", name, rec.cfinds[0])
		}
	}
}

func sameFacts(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
