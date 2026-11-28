package request_test

// LANE-5 slice 3 (A547, A620, A635; REQ-VER-001, REQ-OUT-001; INV-03, INV-04, INV-07, INV-12, AGENT-13, AGENT-21): the Outcome request builder.
// A pure function: from the Execution receipt's facts, the signed plan's facts, the pack's outcome definition and the capability's
// retry safety, it builds the verifier.Request, or it refuses. It reads nothing, writes nothing and decides no verdict. A receipt is
// a pointer and never proof, so an unsigned receipt, one whose signature was not checked VALID, a success claim with no external
// acknowledgement, and any mismatch of tenant, intent or plan hash are refused with a typed error that names no value. All data is SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/outcome/request"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

const (
	tenant   = "ten_aa"
	rcpt     = "rcp_0001"
	intent   = "int_1"
	princ    = "usr_100"
	subject  = "TX1"
	planHash = "sha256:" + "aaaa"
	intHash  = "sha256:" + "bbbb"
	capID    = "fin.cap.create_escalation_case"
	verifyID = "evt_0001"
	ack      = "case_77"
)

func ptr(s string) *string { return &s }

// good is a valid input: an acknowledged receipt of a QUERYABLE_BY_KEY capability, a plan with one step of that capability.
func good() request.Inputs {
	return request.Inputs{
		VerificationID: verifyID,
		Receipt: request.Receipt{
			ReceiptID: rcpt, TenantID: tenant, GrantID: "grt_1", PrincipalID: princ, IdempotencyKey: "idk_v1_abc",
			State: "ACKNOWLEDGED", ExternalAck: ptr(ack), SigKeyID: "core-execution.1", SigValue: "c2ln", Document: []byte(`{"d":1}`),
		},
		Authenticity: verifier.AuthValid,
		Plan: request.Plan{
			TenantID: tenant, IntentID: intent, IntentHash: intHash, PlanHash: planHash,
			Steps: []request.Step{{CapabilityID: capID, Params: map[string]string{"transaction_ref": subject, "owner_queue": "PAYMENTS_OPERATIONS"}}},
		},
		Expect:           request.Expect{TenantID: tenant, IntentID: intent, IntentHash: intHash, PlanHash: planHash},
		StepCapabilityID: capID,
		Capability:       request.Capability{ID: capID, RetrySafety: "QUERYABLE_BY_KEY"},
		Outcome:          request.Definition{OutcomeID: "fin.outcome.exception.case_opened", SuccessPredicate: "exists(case where case.subject == subject_resource)", SettleWindowSeconds: 30},
		SubjectVariable:  "subject_resource",
		Bindings: map[string]request.Source{
			"subject_resource": {Kind: request.FromPlanParam, Name: "transaction_ref"},
			"intent_id":        {Kind: request.FromIntentID},
			"principal_id":     {Kind: request.FromPrincipalID},
			"case_id":          {Kind: request.FromExternalAck},
			"planned_queue":    {Kind: request.FromPlanParam, Name: "owner_queue"},
		},
	}
}

func build(t *testing.T, in request.Inputs) verifier.Request {
	t.Helper()
	r, err := request.Build(in)
	if err != nil {
		t.Fatalf("Build refused a valid input: %v", err)
	}
	return r
}

// REQ-VER-001: every member of the verifier request comes from the receipt, the plan, the definition and the capability.
func TestAValidInputBuildsTheVerifierRequest(t *testing.T) {
	r := build(t, good())
	if r.TenantID != tenant || r.ReceiptID != rcpt || r.VerificationID != verifyID {
		t.Errorf("identity members wrong: %+v", r)
	}
	if r.Definition.OutcomeID != "fin.outcome.exception.case_opened" || r.Definition.SettleWindowSeconds != 30 || !strings.Contains(r.Definition.SuccessPredicate, "case.subject") {
		t.Errorf("definition wrong: %+v", r.Definition)
	}
	if r.Definition.Pending != nil {
		t.Errorf("the pack has no pending condition: Pending = %+v, want nil", r.Definition.Pending)
	}
	want := map[string]string{"subject_resource": subject, "intent_id": intent, "principal_id": princ, "case_id": ack, "planned_queue": "PAYMENTS_OPERATIONS"}
	if len(r.Bindings) != len(want) {
		t.Errorf("bindings = %v, want %v", r.Bindings, want)
	}
	for k, v := range want {
		if r.Bindings[k] != v {
			t.Errorf("binding %s = %q, want %q", k, r.Bindings[k], v)
		}
	}
	if r.TransactionRef != subject {
		t.Errorf("TransactionRef = %q, want the subject binding %q", r.TransactionRef, subject)
	}
	if !r.QueryableByKey {
		t.Error("QUERYABLE_BY_KEY capability: QueryableByKey = false")
	}
}

// REQ-VER-003: only QUERYABLE_BY_KEY lets the verifier look for the effect by its key.
func TestQueryableByKeyFollowsTheCapabilitysRetrySafetyOnly(t *testing.T) {
	for safety, want := range map[string]bool{"QUERYABLE_BY_KEY": true, "NATIVE_IDEMPOTENT": false, "NOT_SAFE": false, "": false, "queryable_by_key": false} {
		in := good()
		in.Capability.RetrySafety = safety
		if got := build(t, in).QueryableByKey; got != want {
			t.Errorf("retry safety %q: QueryableByKey = %v, want %v", safety, got, want)
		}
	}
}

// INV-21 (replayable): same inputs, same bytes, whatever order the maps were filled in.
func TestTheSameInputsGiveTheSameBytes(t *testing.T) {
	first, err := json.Marshal(build(t, good()))
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(20261124)) // SYNTHETIC seed
	for i := 0; i < 1000; i++ {
		in := good()
		keys := []string{"subject_resource", "intent_id", "principal_id", "case_id", "planned_queue"}
		rng.Shuffle(len(keys), func(a, b int) { keys[a], keys[b] = keys[b], keys[a] })
		b := map[string]request.Source{}
		for _, k := range keys {
			b[k] = good().Bindings[k]
		}
		in.Bindings = b
		params := map[string]string{}
		for _, k := range []string{"owner_queue", "transaction_ref"} {
			params[k] = good().Plan.Steps[0].Params[k]
		}
		in.Plan.Steps = []request.Step{{CapabilityID: capID, Params: params}}
		got, err := json.Marshal(build(t, in))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(first) {
			t.Fatalf("iteration %d: bytes differ:\n%s\n%s", i, first, got)
		}
	}
}

func refuses(t *testing.T, name string, mutate func(*request.Inputs), want error) {
	t.Helper()
	in := good()
	mutate(&in)
	r, err := request.Build(in)
	if err == nil {
		t.Errorf("%s: Build returned a request %+v, want a refusal", name, r)
		return
	}
	if !errors.Is(err, want) {
		t.Errorf("%s: error = %v, want %v", name, err, want)
	}
	for _, secret := range []string{tenant, rcpt, intent, princ, subject, planHash, intHash, ack, "idk_v1_abc", "grt_1", "c2ln"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the error carries a value (%q): %v", name, secret, err)
		}
	}
	if r.ReceiptID != "" || r.TenantID != "" || len(r.Bindings) != 0 {
		t.Errorf("%s: a refusal returned a partly built request: %+v", name, r)
	}
}

// INV-04, AGENT-21: a receipt is a pointer. Not signed, or not checked VALID, is refused.
func TestAReceiptThatIsNotSignedAndVerifiedIsRefused(t *testing.T) {
	refuses(t, "no signature key", func(in *request.Inputs) { in.Receipt.SigKeyID = "" }, request.ErrReceiptUnsigned)
	refuses(t, "no signature value", func(in *request.Inputs) { in.Receipt.SigValue = "" }, request.ErrReceiptUnsigned)
	refuses(t, "no document", func(in *request.Inputs) { in.Receipt.Document = nil }, request.ErrReceiptUnsigned)
	for _, a := range []verifier.Authenticity{verifier.AuthNotChecked, verifier.AuthInvalid, "", "valid", "VALID "} {
		a := a
		refuses(t, "authenticity "+string(a), func(in *request.Inputs) { in.Authenticity = a }, request.ErrReceiptNotVerified)
	}
}

// AGENT-21, INV-04: HTTP 200 is not an outcome. An ACKNOWLEDGED claim needs the target's own acknowledgement.
func TestASuccessClaimWithNoExternalAcknowledgementIsRefused(t *testing.T) {
	refuses(t, "nil ack", func(in *request.Inputs) { in.Receipt.ExternalAck = nil }, request.ErrAckMissing)
	refuses(t, "empty ack", func(in *request.Inputs) { in.Receipt.ExternalAck = ptr("") }, request.ErrAckMissing)
	refuses(t, "blank ack", func(in *request.Inputs) { in.Receipt.ExternalAck = ptr("  ") }, request.ErrAckMissing)
	refuses(t, "http 200 only", func(in *request.Inputs) { in.Receipt.ExternalAck = nil; in.Receipt.RawStatus = "HTTP_200" }, request.ErrAckMissing)
}

// INV-12, INV-03: the receipt, the plan and what the caller expects name one tenant, one intent and one plan.
func TestAMismatchOfTenantIntentOrPlanIsRefused(t *testing.T) {
	refuses(t, "receipt tenant", func(in *request.Inputs) { in.Receipt.TenantID = "ten_bb" }, request.ErrMismatch)
	refuses(t, "plan tenant", func(in *request.Inputs) { in.Plan.TenantID = "ten_bb" }, request.ErrMismatch)
	refuses(t, "expected tenant", func(in *request.Inputs) { in.Expect.TenantID = "ten_bb" }, request.ErrMismatch)
	refuses(t, "intent id", func(in *request.Inputs) { in.Plan.IntentID = "int_2" }, request.ErrMismatch)
	refuses(t, "intent hash", func(in *request.Inputs) { in.Plan.IntentHash = "sha256:cccc" }, request.ErrMismatch)
	refuses(t, "plan hash", func(in *request.Inputs) { in.Plan.PlanHash = "sha256:dddd" }, request.ErrMismatch)
	refuses(t, "empty expected plan hash", func(in *request.Inputs) { in.Expect.PlanHash = ""; in.Plan.PlanHash = "" }, request.ErrMismatch)
	refuses(t, "no tenant at all", func(in *request.Inputs) {
		in.Receipt.TenantID, in.Plan.TenantID, in.Expect.TenantID = "", "", ""
	}, request.ErrMismatch)
}

// A receipt in a state no Execution produces is corrupt data, never a request.
func TestAStateNoExecutionProducesIsRefused(t *testing.T) {
	for _, s := range []string{"", "SUCCEEDED", "acknowledged", "READY"} {
		s := s
		refuses(t, "state "+s, func(in *request.Inputs) { in.Receipt.State = s }, request.ErrState)
	}
	for _, s := range []string{"DISPATCHED", "OUTCOME_UNKNOWN", "REJECTED_BY_TARGET", "REJECTED_STALE_VERSION", "FAILED_TERMINAL"} {
		in := good()
		in.Receipt.State, in.Receipt.ExternalAck = s, nil
		if _, err := request.Build(in); err != nil {
			t.Errorf("state %s with no acknowledgement must still build (the verifier decides): %v", s, err)
		}
	}
}

// INV-07: an unknown receipt builds the verification of an unknown outcome, never a success one: nothing sourced from the
// acknowledgement is bound, even if a row carries one, so the verifier cannot reach ACHIEVED on it.
func TestAnUnknownReceiptBindsNothingFromTheAcknowledgement(t *testing.T) {
	for _, s := range []string{"OUTCOME_UNKNOWN", "DISPATCHED", "REJECTED_BY_TARGET", "REJECTED_STALE_VERSION", "FAILED_TERMINAL"} {
		for _, a := range []*string{nil, ptr(ack)} {
			in := good()
			in.Receipt.State, in.Receipt.ExternalAck = s, a
			r := build(t, in)
			if v, ok := r.Bindings["case_id"]; ok {
				t.Errorf("state %s ack %v: case_id bound to %q from a receipt that is not ACKNOWLEDGED", s, a != nil, v)
			}
			if r.Bindings["subject_resource"] != subject || r.Bindings["intent_id"] != intent {
				t.Errorf("state %s: the plan-sourced bindings are missing: %v", s, r.Bindings)
			}
		}
	}
}

// A binding the plan cannot supply is refused: nothing is guessed, nothing is bound to an empty string.
func TestABindingTheInputsCannotSupplyIsRefused(t *testing.T) {
	refuses(t, "missing plan param", func(in *request.Inputs) {
		in.Bindings["planned_queue"] = request.Source{Kind: request.FromPlanParam, Name: "no_such_param"}
	}, request.ErrBinding)
	refuses(t, "empty plan param", func(in *request.Inputs) { in.Plan.Steps[0].Params["transaction_ref"] = "" }, request.ErrBinding)
	refuses(t, "unknown source kind", func(in *request.Inputs) { in.Bindings["x"] = request.Source{Kind: "MODEL_SAYS"} }, request.ErrBinding)
	refuses(t, "param source without a name", func(in *request.Inputs) { in.Bindings["x"] = request.Source{Kind: request.FromPlanParam} }, request.ErrBinding)
	refuses(t, "empty variable name", func(in *request.Inputs) { in.Bindings[""] = request.Source{Kind: request.FromIntentID} }, request.ErrBinding)
	refuses(t, "no subject variable", func(in *request.Inputs) { in.SubjectVariable = "" }, request.ErrBinding)
	refuses(t, "subject variable not bound", func(in *request.Inputs) { in.SubjectVariable = "other" }, request.ErrBinding)
	refuses(t, "no principal", func(in *request.Inputs) { in.Receipt.PrincipalID = "" }, request.ErrBinding)
}

// The plan step is the one of the capability the receipt executed: none, or two, is refused.
func TestThePlanStepMustBeUnique(t *testing.T) {
	refuses(t, "no such step", func(in *request.Inputs) { in.StepCapabilityID = "fin.cap.other" }, request.ErrPlanStep)
	refuses(t, "two steps of the capability", func(in *request.Inputs) {
		in.Plan.Steps = append(in.Plan.Steps, request.Step{CapabilityID: capID, Params: map[string]string{"transaction_ref": "TX2", "owner_queue": "DISPUTES"}})
	}, request.ErrPlanStep)
	refuses(t, "capability and step disagree", func(in *request.Inputs) { in.Capability.ID = "fin.cap.other" }, request.ErrPlanStep)
}

// The definition and the identifiers the verifier needs are all present.
func TestAnIncompleteDefinitionOrIdentifierIsRefused(t *testing.T) {
	refuses(t, "no verification id", func(in *request.Inputs) { in.VerificationID = "" }, request.ErrIncomplete)
	refuses(t, "no receipt id", func(in *request.Inputs) { in.Receipt.ReceiptID = "" }, request.ErrIncomplete)
	refuses(t, "no outcome id", func(in *request.Inputs) { in.Outcome.OutcomeID = "" }, request.ErrIncomplete)
	refuses(t, "no predicate", func(in *request.Inputs) { in.Outcome.SuccessPredicate = "" }, request.ErrIncomplete)
	refuses(t, "negative settle window", func(in *request.Inputs) { in.Outcome.SettleWindowSeconds = -1 }, request.ErrIncomplete)
	refuses(t, "control character in an id", func(in *request.Inputs) { in.VerificationID = "evt_1\n" }, request.ErrIncomplete)
}

// The builder owns no clock, no I/O and no power: its imports are the standard library's pure parts, the verifier's own types
// and the contracts. It imports nothing under trust/, execution/ or evidence/ (the verifier-independence lint, SoT s51).
func TestTheBuilderImportsNoPowerAndNoIO(t *testing.T) {
	allowed := map[string]bool{
		"errors": true, "fmt": true, "sort": true, "strings": true, "unicode": true,
		"github.com/Noemai-org/noema/services/core/outcome/verifier": true,
	}
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source files found: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "context" && f == "reader.go" { // the Reader port takes a context (slice 4); the pure builder, request.go, still may not
				continue
			}
			if !allowed[p] {
				t.Errorf("%s imports %q: not on the builder's list (no clock, no I/O, no other power)", f, p)
			}
		}
	}
}
