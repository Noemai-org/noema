package request_test

// LANE-5 slice 4 (A675, A692; interim, relayed; REQ-VER-001, REQ-OUT-001; INV-04, INV-07, INV-12, AGENT-13): GrantRequests implements the write path's Requests over Build. It reads the
// facts Execution wrote for a grant through a Reader, has the signature checked, takes the outcome definition from the pack, and builds the verifier request or refuses. A fact the
// reader returns for another tenant or another grant, a capability that is not the grant's, an intent hash the decision and the grant do not share, and a signature not found VALID
// are each refused. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/outcome/request"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

var errBoom = errors.New("synthetic failure")

type readerFake struct {
	facts   request.Facts
	err     error
	tenant  string
	grant   string
	callsTo int
}

func (r *readerFake) Load(_ context.Context, tenantID, grantID string) (request.Facts, error) {
	r.callsTo++
	r.tenant, r.grant = tenantID, grantID
	return r.facts, r.err
}

type sigsFake struct {
	res verifier.Authenticity
	err error
	got verifier.Receipt
}

func (s *sigsFake) Check(_ context.Context, r verifier.Receipt) (verifier.Authenticity, error) {
	s.got = r
	return s.res, s.err
}

type outcomesFake struct {
	out    request.Outcome
	err    error
	tenant string
	cap    string
}

func (o *outcomesFake) For(_ context.Context, tenantID, capabilityID string) (request.Outcome, error) {
	o.tenant, o.cap = tenantID, capabilityID
	return o.out, o.err
}

type idsFake struct {
	id     string
	err    error
	prefix string
}

func (i *idsFake) ID(prefix string) (string, error) { i.prefix = prefix; return i.id, i.err }

const grantID = "grt_0001"

func facts() request.Facts {
	return request.Facts{
		Receipt: verifier.Receipt{
			ReceiptID: rcpt, TenantID: tenant, GrantID: grantID, PrincipalID: princ, Attempt: 1, IdempotencyKey: "idk_v1_abc", LogicalAction: capID, State: "ACKNOWLEDGED",
			CorrelationID: "corr_1", RawStatus: "HTTP_201", ExternalAck: ptr(ack), Document: []byte(`{"d":1}`), SigKeyID: "core-execution.ed25519.1", SigValue: "c2ln",
		},
		CapabilityID: capID, IntentID: intent, DecisionIntentHash: intHash, GrantIntentHash: intHash, PlanHash: planHash,
		StepParams: map[string]string{"transaction_ref": subject},
	}
}

func outcome() request.Outcome {
	return request.Outcome{
		Definition:      request.Definition{OutcomeID: "fin.outcome.exception.case_opened", SuccessPredicate: "exists(case where case.subject == subject_resource)", SettleWindowSeconds: 30},
		SubjectVariable: "subject_resource",
		Bindings: map[string]request.Source{
			"subject_resource": {Kind: request.FromPlanParam, Name: "transaction_ref"},
			"intent_id":        {Kind: request.FromIntentID},
			"case_id":          {Kind: request.FromExternalAck},
		},
		Capability: request.Capability{ID: capID, RetrySafety: "QUERYABLE_BY_KEY"},
	}
}

type rig struct {
	rd  *readerFake
	sg  *sigsFake
	oc  *outcomesFake
	ids *idsFake
}

func newRig() *rig {
	return &rig{rd: &readerFake{facts: facts()}, sg: &sigsFake{res: verifier.AuthValid}, oc: &outcomesFake{out: outcome()}, ids: &idsFake{id: "vrf_0001"}}
}

func (r *rig) src() request.GrantRequests {
	return request.GrantRequests{Reader: r.rd, Signatures: r.sg, Outcomes: r.oc, IDs: r.ids}
}

func (r *rig) call() (verifier.Request, error) {
	return r.src().For(context.Background(), tenant, grantID)
}

// REQ-VER-001: the request is built from the facts the reader found, the pack's outcome and a new verification id.
func TestARecordedGrantBuildsTheVerifierRequest(t *testing.T) {
	r := newRig()
	got, err := r.call()
	if err != nil {
		t.Fatalf("a valid grant was refused: %v", err)
	}
	if got.TenantID != tenant || got.ReceiptID != rcpt || got.VerificationID != "vrf_0001" || got.TransactionRef != subject || !got.QueryableByKey {
		t.Errorf("request wrong: %+v", got)
	}
	want := map[string]string{"subject_resource": subject, "intent_id": intent, "case_id": ack}
	if len(got.Bindings) != len(want) {
		t.Errorf("bindings = %v, want %v", got.Bindings, want)
	}
	for k, v := range want {
		if got.Bindings[k] != v {
			t.Errorf("binding %s = %q, want %q", k, got.Bindings[k], v)
		}
	}
	if got.Definition.OutcomeID != "fin.outcome.exception.case_opened" || got.Definition.SettleWindowSeconds != 30 {
		t.Errorf("definition wrong: %+v", got.Definition)
	}
}

// INV-12: the reader is asked for the session's tenant and the grant; the pack is asked for the GRANT's capability, never one the receipt row names.
func TestTheReaderAndThePackAreAskedForTheTenantsGrantAndTheGrantsCapability(t *testing.T) {
	r := newRig()
	if _, err := r.call(); err != nil {
		t.Fatal(err)
	}
	if r.rd.tenant != tenant || r.rd.grant != grantID {
		t.Errorf("reader asked for (%q, %q), want (%q, %q)", r.rd.tenant, r.rd.grant, tenant, grantID)
	}
	if r.oc.tenant != tenant || r.oc.cap != capID {
		t.Errorf("pack asked for (%q, %q), want (%q, %q)", r.oc.tenant, r.oc.cap, tenant, capID)
	}
	if r.ids.prefix != "vrf" {
		t.Errorf("verification id prefix = %q, want vrf", r.ids.prefix)
	}
}

// INV-04: the signature check gets the receipt exactly as stored; a receipt is a pointer and the check is the caller's.
func TestTheSignatureIsCheckedOnTheStoredReceipt(t *testing.T) {
	r := newRig()
	if _, err := r.call(); err != nil {
		t.Fatal(err)
	}
	if r.sg.got.ReceiptID != rcpt || string(r.sg.got.Document) != `{"d":1}` || r.sg.got.SigValue != "c2ln" {
		t.Errorf("the check saw %+v, want the stored receipt", r.sg.got)
	}
}

// INV-04: a signature not found VALID gives no request; a failed check is an error, never a request.
func TestASignatureNotFoundValidGivesNoRequest(t *testing.T) {
	r := newRig()
	r.sg.res = verifier.AuthInvalid
	if got, err := r.call(); !errors.Is(err, request.ErrReceiptNotVerified) || got.ReceiptID != "" {
		t.Errorf("an INVALID signature: got (%+v, %v), want ErrReceiptNotVerified and no request", got, err)
	}
	r = newRig()
	r.sg.err = errBoom
	if got, err := r.call(); !errors.Is(err, errBoom) || got.ReceiptID != "" {
		t.Errorf("a failed check: got (%+v, %v), want the cause and no request", got, err)
	}
}

// INV-12: the reader is a port that may be wrong; what it returns for another tenant or another grant is refused before anything is built.
func TestFactsOfAnotherTenantGrantOrCapabilityAreRefused(t *testing.T) {
	for name, edit := range map[string]func(f *request.Facts){
		"another tenant":                 func(f *request.Facts) { f.Receipt.TenantID = "ten_bb" },
		"another grant":                  func(f *request.Facts) { f.Receipt.GrantID = "grt_other" },
		"the receipt ran another action": func(f *request.Facts) { f.Receipt.LogicalAction = "fin.cap.other" },
		"decision and grant disagree":    func(f *request.Facts) { f.DecisionIntentHash = "sha256:cccc" },
		"no capability":                  func(f *request.Facts) { f.CapabilityID = "" },
		"no plan hash":                   func(f *request.Facts) { f.PlanHash = "" },
		"no intent":                      func(f *request.Facts) { f.IntentID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig()
			edit(&r.rd.facts)
			got, err := r.call()
			if err == nil || got.ReceiptID != "" {
				t.Fatalf("got (%+v, %v), want a refusal and no request", got, err)
			}
			if !errors.Is(err, request.ErrMismatch) && !errors.Is(err, request.ErrIncomplete) {
				t.Errorf("error %v is neither ErrMismatch nor ErrIncomplete", err)
			}
		})
	}
}

// A missing row is the reader's ErrNotFound and stays one; any other failure keeps its cause. Neither builds a request.
func TestAReaderFailureGivesNoRequest(t *testing.T) {
	for _, cause := range []error{request.ErrNotFound, errBoom} {
		r := newRig()
		r.rd.err = cause
		got, err := r.call()
		if !errors.Is(err, cause) || got.ReceiptID != "" {
			t.Errorf("reader error %v: got (%+v, %v)", cause, got, err)
		}
		if r.sg.got.ReceiptID != "" {
			t.Errorf("the signature was checked after the reader failed")
		}
	}
}

// The pack and the id source are parts that may fail: each failure is an error with its cause and no request.
func TestAPackOrIdFailureGivesNoRequest(t *testing.T) {
	r := newRig()
	r.oc.err = errBoom
	if got, err := r.call(); !errors.Is(err, errBoom) || got.ReceiptID != "" {
		t.Errorf("pack error: got (%+v, %v)", got, err)
	}
	r = newRig()
	r.ids.err = errBoom
	if got, err := r.call(); !errors.Is(err, errBoom) || got.ReceiptID != "" {
		t.Errorf("id error: got (%+v, %v)", got, err)
	}
}

// INV-07: a receipt that is not ACKNOWLEDGED binds nothing from the acknowledgement, so the verifier cannot reach a success verdict on it.
func TestAReceiptThatIsNotAcknowledgedBindsNoAcknowledgement(t *testing.T) {
	r := newRig()
	r.rd.facts.Receipt.State, r.rd.facts.Receipt.ExternalAck, r.rd.facts.Receipt.RawStatus = "OUTCOME_UNKNOWN", nil, "TIMEOUT"
	got, err := r.call()
	if err != nil {
		t.Fatalf("an OUTCOME_UNKNOWN receipt was refused: %v", err)
	}
	if v, ok := got.Bindings["case_id"]; ok {
		t.Errorf("case_id bound to %q on a receipt that is not ACKNOWLEDGED", v)
	}
}

// AGENT-13: a param the pack's bindings do not name is not bound, even when the reader returns it.
func TestOnlyTheBoundParamsReachTheRequest(t *testing.T) {
	r := newRig()
	r.rd.facts.StepParams = map[string]string{"transaction_ref": subject, "owner_queue": "PAYMENTS_OPERATIONS"}
	got, err := r.call()
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range got.Bindings {
		if v == "PAYMENTS_OPERATIONS" {
			t.Errorf("an unbound param reached the request as %q", k)
		}
	}
}

// A bound param the receipt does not carry (an older receipt, before A692) gives a typed refusal, never an empty binding.
func TestABoundParamTheReceiptDoesNotCarryIsRefused(t *testing.T) {
	r := newRig()
	r.rd.facts.StepParams = nil
	got, err := r.call()
	if !errors.Is(err, request.ErrBinding) || got.ReceiptID != "" {
		t.Errorf("got (%+v, %v), want ErrBinding and no request", got, err)
	}
}

// A source with a missing part, or a call without a tenant or a grant, reads nothing.
func TestAnUnusableSourceReadsNothing(t *testing.T) {
	good := func() request.GrantRequests { return newRig().src() }
	for name, s := range map[string]request.GrantRequests{
		"no reader":     func() request.GrantRequests { g := good(); g.Reader = nil; return g }(),
		"no signatures": func() request.GrantRequests { g := good(); g.Signatures = nil; return g }(),
		"no outcomes":   func() request.GrantRequests { g := good(); g.Outcomes = nil; return g }(),
		"no ids":        func() request.GrantRequests { g := good(); g.IDs = nil; return g }(),
	} {
		if _, err := s.For(context.Background(), tenant, grantID); !errors.Is(err, request.ErrIncomplete) {
			t.Errorf("%s: got %v, want ErrIncomplete", name, err)
		}
	}
	r := newRig()
	for _, c := range [][2]string{{"", grantID}, {tenant, ""}} {
		if _, err := r.src().For(context.Background(), c[0], c[1]); !errors.Is(err, request.ErrIncomplete) {
			t.Errorf("call %v: got %v, want ErrIncomplete", c, err)
		}
	}
	if r.rd.callsTo != 0 {
		t.Errorf("the reader was called %d times for an unusable call", r.rd.callsTo)
	}
}
