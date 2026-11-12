package bridge_test

// EXE-05 slice 2, ruling A157: the bridge dispatches the two append-only capabilities of mock-case, record_investigation_evidence and log_complaint_reference,
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

func TestAComplaintIsOneKeyedLogWithThePlansCase(t *testing.T) {
	rec := &recordsRecorder{write: adapters.WriteResult{Class: adapters.ClassAck2xx, ExternalAck: "cmp_1", RawCode: "201"}}
	got := bridge.CaseDispatcher{Case: &recorder{}, Records: rec}.Dispatch(context.Background(), complaintReq())
	if got.Class != controller.TransportAck2xx || got.ExternalAck != "cmp_1" {
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

// EC-E06: a key collision crosses the bridge for both capabilities, so the controller can raise the security event.
func TestKeyCollisionCrossesTheBridgeForBothRecords(t *testing.T) {
	rec := &recordsRecorder{write: adapters.WriteResult{Class: adapters.ClassRejected4xx, RawCode: "409:IDEMPOTENCY_CONFLICT", KeyCollision: true}}
	for _, req := range []controller.DispatchRequest{evidenceReq(), complaintReq()} {
		if got := (bridge.CaseDispatcher{Case: &recorder{}, Records: rec}).Dispatch(context.Background(), req); !got.KeyCollision || got.Class != controller.TransportRejected4xx {
			t.Errorf("%s: %+v", req.CapabilityID, got)
		}
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
	for _, req := range []controller.DispatchRequest{evidenceReq(), complaintReq()} {
		if got := (bridge.CaseDispatcher{Case: &recorder{}}).Dispatch(context.Background(), req); got.Class != controller.TransportDefect || got.RawCode != "NO_ADAPTER_FOR_CAPABILITY" {
			t.Errorf("%s with no records adapter: %+v", req.CapabilityID, got)
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
		got, err := bridge.CaseRevalidator{Case: r, Records: &recordsRecorder{}}.Revalidate(context.Background(), recordGrant(), step)
		if c.wantErr != (err != nil) || (err == nil && (got.Passed != c.passed || !sameFacts(got.ChangedFacts, c.facts))) {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
}

// REQ-EXE-003, descriptor preconditions: a complaint is logged once per case and epoch. It revalidates the case status and that no complaint reference exists for
// the case yet; a complaint already there fails the grant; an incomplete page proves nothing and is an error, not a pass.
func TestComplaintRevalidation(t *testing.T) {
	step := controller.PlanStep{CapabilityID: bridge.CapLogComplaintReference, Params: complaintReq().Params}
	one := adapters.ComplaintPage{Complete: true, Items: []adapters.ComplaintRecord{{ComplaintRef: "cmp_1", CaseID: "case_01", TenantID: "ten_aa"}}}
	for name, c := range map[string]struct {
		get     adapters.CaseRecord
		getErr  error
		page    adapters.ComplaintPage
		perr    error
		passed  bool
		facts   []string
		wantErr bool
	}{
		"open case, no complaint":       {get: openCase(), page: adapters.ComplaintPage{Complete: true}, passed: true},
		"a complaint exists":            {get: openCase(), page: one, facts: []string{bridge.FactExistingComplaint}},
		"closed case, no complaint":     {get: adapters.CaseRecord{Status: "CLOSED"}, page: adapters.ComplaintPage{Complete: true}, facts: []string{bridge.FactCaseStatus}},
		"closed case and a complaint":   {get: adapters.CaseRecord{Status: "CLOSED"}, page: one, facts: []string{bridge.FactCaseStatus, bridge.FactExistingComplaint}},
		"an incomplete page":            {get: openCase(), page: adapters.ComplaintPage{Complete: false, NextCursor: "c1"}, wantErr: true},
		"the case cannot be read":       {getErr: &adapters.ReadError{Failure: adapters.ReadUnavailable}, page: adapters.ComplaintPage{Complete: true}, wantErr: true},
		"the complaints cannot be read": {get: openCase(), perr: &adapters.ReadError{Failure: adapters.ReadUnavailable}, wantErr: true},
	} {
		rec := &recordsRecorder{cpage: c.page, cerr: c.perr}
		got, err := bridge.CaseRevalidator{Case: &recorder{get: c.get, getErr: c.getErr}, Records: rec}.Revalidate(context.Background(), recordGrant(), step)
		if c.wantErr != (err != nil) || (err == nil && (got.Passed != c.passed || !sameFacts(got.ChangedFacts, c.facts))) {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
		if len(rec.cfinds) > 0 && (rec.cfinds[0].TenantID != "ten_aa" || rec.cfinds[0].CaseID != "case_01" || rec.cfinds[0].IdempotencyKey != "") {
			t.Errorf("%s: the complaint find was %+v, want this tenant and this case only", name, rec.cfinds[0])
		}
	}
	// With no records adapter there is no revalidation, which the controller turns into a void grant.
	if _, err := (bridge.CaseRevalidator{Case: &recorder{get: openCase()}}).Revalidate(context.Background(), recordGrant(), step); err == nil {
		t.Error("a complaint revalidated with no records adapter")
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
