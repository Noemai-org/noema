package interpreter_test

// UND-04, core half (ruling A166, A177; REQ-INT-001, REQ-INT-002, REQ-INP-002, REQ-AUD-001; INV-02, INV-12, INV-17, EC-E16): the Interpreter takes what the model service
// compiled, runs the core's gate over it, records what the Record must hold (a rejection, a real identity override) before anything else, and saves the admitted intent
// through the store. No database here: the model service, the Record and the store are spies. All data is SYNTHETIC.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

const (
	corr  = "corr_4c21f9"
	actor = "customer:usr_88213"
)

var session = understanding.Session{PrincipalID: "usr_88213", TenantID: "ten_demo", AuthnRef: "ses_7a1b", PrincipalType: "end_user", AssertedBy: "noema-poc-idp"}

// calls is the order in which the spies were called.
type calls struct{ order []string }

type spyCompiler struct {
	c   *calls
	out understanding.Compiled
	err error
	got understanding.CompileRequest
}

func (s *spyCompiler) Compile(_ context.Context, r understanding.CompileRequest) (understanding.Compiled, error) {
	s.c.order = append(s.c.order, "compile")
	s.got = r
	return s.out, s.err
}

type spyRecorder struct {
	c         *calls
	rejected  [][]understanding.Reason
	overrides [][]string
	failWith  error
}

func (s *spyRecorder) Rejected(_ context.Context, _ interpreter.Subject, reasons []understanding.Reason) error {
	s.c.order = append(s.c.order, "rejected")
	s.rejected = append(s.rejected, reasons)
	return s.failWith
}

func (s *spyRecorder) OverrideAttempt(_ context.Context, _ interpreter.Subject, fields []string) error {
	s.c.order = append(s.c.order, "override")
	s.overrides = append(s.overrides, fields)
	return s.failWith
}

type spySaver struct {
	c     *calls
	saved []intentstore.Admitted
	actor []string
	err   error
}

func (s *spySaver) Save(_ context.Context, a string, x intentstore.Admitted) (intentstore.Ref, error) {
	s.c.order = append(s.c.order, "save")
	s.saved, s.actor = append(s.saved, x), append(s.actor, a)
	return intentstore.Ref{EventID: "recev_01J9X3", Seq: 7, Hash: "sha256:" + strings.Repeat("a", 64)}, s.err
}

// proposal is what the model service's gate returns: the markers where the model has nothing to say.
func proposal(mutate func(m map[string]any)) []byte {
	m := map[string]any{
		"schema_version": "1.0", "intent_id": "int_01J9X3", "version": 1, "supersedes": nil, "tenant_id": session.TenantID, "correlation_id": corr,
		"created_at":  "2026-10-01T09:47:03Z",
		"principal":   map[string]any{"principal_id": session.PrincipalID, "principal_type": "unset", "authn_ref": session.AuthnRef, "asserted_by": "unset"},
		"source":      map[string]any{"mode": "MODE_1", "channel": "text", "locale_detected": []any{"en-NG"}, "asr_confidence": nil, "raw_ref": "raw_01J9X3"},
		"pack":        map[string]any{"pack_id": "fin", "pack_version": "1.0.0"},
		"intent_node": "fin.payments.exception.investigate",
		"slots": map[string]any{
			"subject_resource": map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX88213"},
			"question_type":    map[string]any{"type": "Reference", "value": "STATUS"},
		},
		"ambiguity_flags": []any{}, "desired_outcome_refs": []any{"fin.outcome.exception.status_explained"}, "status": "VALIDATED",
		"sig": map[string]any{"key_id": "unsigned", "value": "unsigned"},
	}
	if mutate != nil {
		mutate(m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

type world struct {
	calls *calls
	comp  *spyCompiler
	rec   *spyRecorder
	save  *spySaver
	in    *interpreter.Interpreter
}

func newWorld(t *testing.T, out []byte) *world {
	t.Helper()
	raw, err := os.ReadFile("../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	view, err := understanding.NewPackView(raw)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := understanding.NewGate(64<<10, view)
	if err != nil {
		t.Fatal(err)
	}
	c := &calls{}
	w := &world{calls: c, comp: &spyCompiler{c: c, out: understanding.Compiled{IntentObject: out, CanonicalInput: json.RawMessage(`{"synthetic":true}`)}}, rec: &spyRecorder{c: c}, save: &spySaver{c: c}}
	w.in, err = interpreter.New(interpreter.Config{Compiler: w.comp, Gate: gate, Recorder: w.rec, Saver: w.save})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func input() interpreter.Input {
	return interpreter.Input{Session: session, Actor: actor, CorrelationID: corr, Request: understanding.CompileRequest{CanonicalInput: json.RawMessage(`{"synthetic":true}`), PackRef: "fin@1.0.0"}}
}

// A normal compile: nothing is recorded by the Interpreter itself (the store appends INTENT_ADMITTED), the saved intent is the admitted one with the session's identity, and
// what the store gets is bound to itself (the hash is the sha-256 of the canonical document it carries).
func TestANormalCompileIsAdmittedAndSavedWithoutAnyEventOfItsOwn(t *testing.T) {
	w := newWorld(t, proposal(nil))
	res, err := w.in.Interpret(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(w.calls.order, ","); got != "compile,save" {
		t.Fatalf("calls %s, want compile then save and no event of the Interpreter's own", got)
	}
	if len(w.save.saved) != 1 || w.save.actor[0] != actor {
		t.Fatalf("saved %d, actor %v", len(w.save.saved), w.save.actor)
	}
	a := w.save.saved[0]
	sum := sha256.Sum256(a.JSON)
	if a.IntentHash != "sha256:"+hex.EncodeToString(sum[:]) || a.TenantID != session.TenantID || a.IntentID != "int_01J9X3" || a.Version != 1 || a.Supersedes != "" ||
		a.IntentNode != "fin.payments.exception.investigate" || a.CorrelationID != corr {
		t.Errorf("saved %+v: not the admitted intent bound to its hash and fields", a)
	}
	if strings.Contains(string(a.JSON), `"unset"`) || !strings.Contains(string(a.JSON), `"asserted_by":"noema-poc-idp"`) {
		t.Errorf("the saved document is not the one with the session's identity written over the markers: %s", a.JSON)
	}
	if res.Ref.EventID != "recev_01J9X3" || res.Intent.Hash() != a.IntentHash {
		t.Errorf("result %+v", res)
	}
	if !reflect.DeepEqual(w.comp.got.PackRef, "fin@1.0.0") {
		t.Errorf("the request was not passed to the model service as given: %+v", w.comp.got)
	}
}

// A refusal at the gate is recorded as INTENT_REJECTED_AT_GATE with its sorted reasons, before the caller hears of it, and nothing is saved (EC-E16, REQ-INT-001).
func TestARefusalAtTheGateIsRecordedFirstAndNothingIsSaved(t *testing.T) {
	w := newWorld(t, proposal(func(m map[string]any) {
		m["intent_node"] = "fin.transfer.send"
		m["sig"] = map[string]any{"key_id": "k", "value": "v"}
	}))
	_, err := w.in.Interpret(context.Background(), input())
	var rej *understanding.Rejection
	if !errors.As(err, &rej) {
		t.Fatalf("err %v, want the gate's *Rejection", err)
	}
	if got := strings.Join(w.calls.order, ","); got != "compile,rejected" {
		t.Fatalf("calls %s, want compile then rejected only", got)
	}
	want := []understanding.Reason{understanding.ReasonSig, understanding.ReasonUnknownNode}
	if !reflect.DeepEqual(w.rec.rejected[0], want) || !reflect.DeepEqual(rej.Reasons, want) {
		t.Errorf("recorded %v, returned %v, want %v", w.rec.rejected[0], rej.Reasons, want)
	}
	if len(w.save.saved) != 0 {
		t.Error("a refused intent was saved")
	}
}

func TestWhenTheRejectionCannotBeRecordedTheCallerIsToldTheRecordFailedNotThatItWasRefused(t *testing.T) {
	w := newWorld(t, proposal(func(m map[string]any) { m["intent_node"] = "fin.transfer.send" }))
	w.rec.failWith = errors.New("synthetic: the Record refused")
	_, err := w.in.Interpret(context.Background(), input())
	if !errors.Is(err, interpreter.ErrRecord) {
		t.Fatalf("err %v, want ErrRecord", err)
	}
	var rej *understanding.Rejection
	if errors.As(err, &rej) || len(w.save.saved) != 0 {
		t.Errorf("a rejection that was not recorded was reported as one (%v) or something was saved", err)
	}
}

// A real override (the model service returned another tenant, principal or session) is recorded by field NAME before the intent is saved, and the intent is still the
// session's (REQ-INP-002). The values are never recorded.
func TestARealIdentityOverrideIsRecordedByNameBeforeTheSaveAndTheSessionsIdentityIsSaved(t *testing.T) {
	w := newWorld(t, proposal(func(m map[string]any) {
		m["tenant_id"] = "ten_other"
		m["principal"].(map[string]any)["principal_id"] = "usr_attacker"
	}))
	if _, err := w.in.Interpret(context.Background(), input()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(w.calls.order, ","); got != "compile,override,save" {
		t.Fatalf("calls %s, want the override recorded before the save", got)
	}
	if want := []string{"tenant_id", "principal_id"}; !reflect.DeepEqual(w.rec.overrides[0], want) {
		t.Errorf("recorded %v, want the names %v", w.rec.overrides[0], want)
	}
	if s := string(w.save.saved[0].JSON); strings.Contains(s, "attacker") || strings.Contains(s, "ten_other") || w.save.saved[0].TenantID != session.TenantID {
		t.Errorf("a model-supplied identity was saved: %s", s)
	}
}

func TestWhenTheOverrideCannotBeRecordedNothingIsSaved(t *testing.T) {
	w := newWorld(t, proposal(func(m map[string]any) { m["tenant_id"] = "ten_other" }))
	w.rec.failWith = errors.New("synthetic: the Record refused")
	if _, err := w.in.Interpret(context.Background(), input()); !errors.Is(err, interpreter.ErrRecord) || len(w.save.saved) != 0 {
		t.Fatalf("err %v saved %d, want ErrRecord and nothing saved (the Record comes first)", err, len(w.save.saved))
	}
}

func TestEveryWayTheModelServiceCanFailIsClassifiedAndNothingIsRecordedOrSaved(t *testing.T) {
	for _, f := range []understanding.Failure{understanding.FailureProviderUnavailable, understanding.FailureInputInvalid, understanding.FailureUnavailable, understanding.FailureMalformed} {
		w := newWorld(t, nil)
		w.comp.err = &understanding.CompileError{Failure: f}
		_, err := w.in.Interpret(context.Background(), input())
		var ce *interpreter.CompileFailure
		if !errors.As(err, &ce) || ce.Failure != f {
			t.Errorf("%s: err %v, want a *CompileFailure carrying it", f, err)
		}
		if got := strings.Join(w.calls.order, ","); got != "compile" {
			t.Errorf("%s: calls %s, want only the compile", f, got)
		}
	}
	w := newWorld(t, nil)
	w.comp.err = errors.New("synthetic: not a CompileError")
	if _, err := w.in.Interpret(context.Background(), input()); !errors.Is(err, interpreter.ErrCompile) {
		t.Errorf("an unclassified failure: %v, want ErrCompile", err)
	}
}

func TestAFailedSaveIsReportedWithoutQuotingTheDocument(t *testing.T) {
	w := newWorld(t, proposal(nil))
	w.save.err = errors.New("synthetic: unique violation")
	_, err := w.in.Interpret(context.Background(), input())
	if !errors.Is(err, interpreter.ErrSave) {
		t.Fatalf("err %v, want ErrSave", err)
	}
	if strings.Contains(err.Error(), "int_01J9X3") || strings.Contains(err.Error(), "fin.payments") {
		t.Errorf("the error quotes the document: %v", err)
	}
}

func TestAnInputThatIsNotCompleteIsRefusedBeforeTheModelServiceIsCalled(t *testing.T) {
	for name, mutate := range map[string]func(in *interpreter.Input){
		"no actor":                      func(in *interpreter.Input) { in.Actor = "" },
		"an actor off shape":            func(in *interpreter.Input) { in.Actor = "end_user:usr_1" },
		"no correlation":                func(in *interpreter.Input) { in.CorrelationID = "" },
		"a correlation off shape":       func(in *interpreter.Input) { in.CorrelationID = "corr x" },
		"no session":                    func(in *interpreter.Input) { in.Session = understanding.Session{} },
		"an actor of another principal": func(in *interpreter.Input) { in.Actor = "customer:usr_other" },
	} {
		w := newWorld(t, proposal(nil))
		in := input()
		mutate(&in)
		if _, err := w.in.Interpret(context.Background(), in); !errors.Is(err, interpreter.ErrInput) {
			t.Errorf("%s: err %v, want ErrInput", name, err)
		}
		if len(w.calls.order) != 0 {
			t.Errorf("%s: %v was called before the input was refused", name, w.calls.order)
		}
	}
}

// Codex P2 on noema#519: the session is validated in full, by the gate's own rule, BEFORE the model service is called. A session that is present but off its shapes (a
// principal type with a space, a tenant without its prefix, an authn reference of another kind, an asserted_by with capitals) is the caller's bug, and the caller's user
// input must not reach the model service for it. REQ-INP-002, INV-12; ErrInput before any call.
func TestAMalformedSessionIsRefusedBeforeTheModelServiceIsCalled(t *testing.T) {
	for name, mutate := range map[string]func(s *understanding.Session){
		"a principal type with a space":            func(s *understanding.Session) { s.PrincipalType = "End User" },
		"a principal type in capitals":             func(s *understanding.Session) { s.PrincipalType = "END_USER" },
		"a principal id without its prefix":        func(s *understanding.Session) { s.PrincipalID = "88213" },
		"a principal id that is too short":         func(s *understanding.Session) { s.PrincipalID = "usr_" },
		"a tenant without its prefix":              func(s *understanding.Session) { s.TenantID = "demo" },
		"an authn reference of another kind":       func(s *understanding.Session) { s.AuthnRef = "tok_7a1b" },
		"an asserted_by with capitals":             func(s *understanding.Session) { s.AssertedBy = "Noema IdP" },
		"an asserted_by that starts with a hyphen": func(s *understanding.Session) { s.AssertedBy = "-noema" },
	} {
		w := newWorld(t, proposal(nil))
		in := input()
		mutate(&in.Session)
		if _, err := w.in.Interpret(context.Background(), in); !errors.Is(err, interpreter.ErrInput) {
			t.Errorf("%s: err %v, want ErrInput", name, err)
		}
		if len(w.calls.order) != 0 {
			t.Errorf("%s: %v was called before the session was refused: the model service saw the input of a caller that should have been refused", name, w.calls.order)
		}
	}
}

func TestNewRefusesWhatItCannotRunWith(t *testing.T) {
	w := newWorld(t, nil)
	good := interpreter.Config{Compiler: w.comp, Gate: mustGate(t), Recorder: w.rec, Saver: w.save}
	if _, err := interpreter.New(good); err != nil {
		t.Fatalf("the base config: %v", err)
	}
	for name, mutate := range map[string]func(c *interpreter.Config){
		"no compiler": func(c *interpreter.Config) { c.Compiler = nil },
		"no gate":     func(c *interpreter.Config) { c.Gate = nil },
		"no recorder": func(c *interpreter.Config) { c.Recorder = nil },
		"no saver":    func(c *interpreter.Config) { c.Saver = nil },
	} {
		c := good
		mutate(&c)
		if _, err := interpreter.New(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func mustGate(t *testing.T) *understanding.Gate {
	t.Helper()
	raw, err := os.ReadFile("../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	view, err := understanding.NewPackView(raw)
	if err != nil {
		t.Fatal(err)
	}
	g, err := understanding.NewGate(64<<10, view)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// A session the caller passed that the gate finds incomplete is the caller's bug, not a rejection of the model's proposal: it is ErrInput and nothing is recorded.
func TestASessionTheGateFindsMalformedIsTheCallersBugAndRecordsNothing(t *testing.T) {
	w := newWorld(t, proposal(nil))
	in := input()
	in.Session.PrincipalType = "End User"
	_, err := w.in.Interpret(context.Background(), in)
	if !errors.Is(err, interpreter.ErrInput) {
		t.Fatalf("err %v, want ErrInput", err)
	}
	if len(w.rec.rejected) != 0 || len(w.save.saved) != 0 {
		t.Errorf("rejected %d saved %d: a caller's bug was recorded as a rejection or saved", len(w.rec.rejected), len(w.save.saved))
	}
}

// The cause of a failure stays reachable but is never printed: a database error that carries a key must not reach a message.
func TestACauseIsReachableButNeverPrinted(t *testing.T) {
	cause := errors.New("synthetic key (ten_demo, int_01J9X3, 1) already exists")
	w := newWorld(t, proposal(nil))
	w.save.err = cause
	_, err := w.in.Interpret(context.Background(), input())
	if !errors.Is(err, cause) || !errors.Is(err, interpreter.ErrSave) {
		t.Fatalf("err %v: the cause or the kind is not reachable", err)
	}
	if strings.Contains(err.Error(), "int_01J9X3") || strings.Contains(err.Error(), "ten_demo") {
		t.Errorf("the message prints the cause: %v", err)
	}
}
