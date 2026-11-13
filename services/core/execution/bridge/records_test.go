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

// A noema#491 review finding (P1, atomicity): the pack declares log_complaint_reference CONDITIONAL_WRITE with the precondition "not exists(complaint for the subject and
// epoch)", E7 can only read absence before the write, and mock-case's create is keyed by idempotency key alone, so two executions with distinct keys can both pass E7 and
// create two irreversible regulatory complaints. Until the target creates a complaint conditionally and atomically, the capability has no mapping: Dispatch makes no
// call, whatever the plan says (REQ-CAP-002, REQ-EXE-004, EC-E06, INV-03; AGENT-20 the safer reading).
func TestAComplaintIsNotDispatchedUntilTheTargetCreatesItConditionally(t *testing.T) {
	rec := &recordsRecorder{write: adapters.WriteResult{Class: adapters.ClassAck2xx, ExternalAck: "cmp_1", RawCode: "201"}}
	for name, mutate := range map[string]func(*controller.DispatchRequest){
		"a well-formed plan": func(*controller.DispatchRequest) {},
		"another case":       func(r *controller.DispatchRequest) { r.Params["case_id"] = "case_02" },
		"no case":            func(r *controller.DispatchRequest) { delete(r.Params, "case_id") },
		"an unknown key":     func(r *controller.DispatchRequest) { r.Params["note"] = "x" },
	} {
		req := complaintReq()
		mutate(&req)
		got := bridge.CaseDispatcher{Case: &recorder{}, Records: rec}.Dispatch(context.Background(), req)
		if got.Class != controller.TransportDefect || got.RawCode != "NO_ADAPTER_FOR_CAPABILITY" {
			t.Errorf("%s: %+v, want an adapter defect NO_ADAPTER_FOR_CAPABILITY", name, got)
		}
	}
	if len(rec.complaints) != 0 || len(rec.evidence) != 0 {
		t.Fatalf("calls made for a complaint: %d complaints, %d evidence", len(rec.complaints), len(rec.evidence))
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
	} {
		rec := &recordsRecorder{write: ack}
		req := c.req()
		c.mutate(&req)
		got := bridge.CaseDispatcher{Case: &recorder{}, Records: rec}.Dispatch(context.Background(), req)
		if got.Class != controller.TransportDefect || got.RawCode != c.raw || len(rec.evidence)+len(rec.complaints) != 0 {
			t.Errorf("%s: %+v, calls %d", c.name, got, len(rec.evidence)+len(rec.complaints))
		}
	}
	if got := (bridge.CaseDispatcher{Case: &recorder{}}).Dispatch(context.Background(), evidenceReq()); got.Class != controller.TransportDefect || got.RawCode != "NO_ADAPTER_FOR_CAPABILITY" {
		t.Errorf("evidence with no records adapter: %+v", got)
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

// The complaint has no dispatch (above), so it has no revalidation either: E7 returns an error whatever the reads would say, the controller voids the grant
// (REVALIDATION_UNAVAILABLE), and no read is made on its behalf. A complaint that cannot be written is never revalidated as if it could be.
func TestAComplaintCannotBeRevalidatedSoItsGrantIsVoided(t *testing.T) {
	step := controller.PlanStep{CapabilityID: bridge.CapLogComplaintReference, Params: complaintReq().Params}
	rec := &recordsRecorder{cpage: adapters.ComplaintPage{Complete: true}}
	r := &recorder{get: openCase()}
	if got, err := (bridge.CaseRevalidator{Case: r}).Revalidate(context.Background(), recordGrant(), step); err == nil || got.Passed {
		t.Fatalf("a complaint revalidated: %+v, %v", got, err)
	}
	if len(rec.cfinds) != 0 {
		t.Fatalf("%d complaint reads were made", len(rec.cfinds))
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
