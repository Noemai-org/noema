package interpreterport_test

// UND-04 (ruling A262 on noema#565; REQ-INT-001, REQ-INT-002, REQ-INP-002, REQ-API-001; INV-02, INV-12, INV-21; AGENT-09, AGENT-10): the orchestrator's Interpreter port over the core's
// Interpreter. The adapter builds the canonical input from the verified session and the admitted turn (never from the text), asks the Interpreter, and maps what comes back to the three
// kinds the orchestrator knows. The real Interpreter and the real gate run here with the pack of the repository; the model service, the Record and the store are spies. SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	interpreterport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/interpreter"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

const (
	corr    = "corr_4c21f9"
	reqID   = "0b8f3c1e-52a4-4d7e-9a61-5f1d2b8c4a97"
	packRef = "fin@1.0.0"
)

var (
	tenant  = orchestrator.Session{TenantID: "ten_demo", PrincipalID: "usr_88213", Role: "customer"}
	session = understanding.Session{PrincipalID: "usr_88213", TenantID: "ten_demo", AuthnRef: "ses_7a1b", PrincipalType: "end_user", AssertedBy: "noema-poc-idp"}
	now     = time.Date(2026, 11, 17, 9, 30, 15, 0, time.UTC)
)

type sessions struct {
	out   interpreterport.Resolved
	err   error
	calls int
}

func (s *sessions) Resolve(context.Context, orchestrator.Session) (interpreterport.Resolved, error) {
	s.calls++
	return s.out, s.err
}

const objectRef = "req_01J9X3A1B2C3D4E5F6G7H8J9K0" // SYNTHETIC: the object_ref of the recorded INPUT_RECEIVED

// admissions is the Record-backed Admissions port, as a spy: it answers for the turn's InputReceivedRef.
type admissions struct {
	out   orchestrator.Admission
	err   error
	asked []string
}

func newAdmissions() *admissions {
	return &admissions{out: orchestrator.Admission{EventType: "INPUT_RECEIVED", TenantID: "ten_demo", PrincipalID: "usr_88213", CorrelationID: corr, RequestID: reqID, ObjectRef: objectRef}}
}

func (a *admissions) Lookup(_ context.Context, s orchestrator.Session, ref string) (orchestrator.Admission, error) {
	a.asked = append(a.asked, s.TenantID+"/"+ref)
	return a.out, a.err
}

type compiler struct {
	out  []byte
	err  error
	reqs []understanding.CompileRequest
}

func (c *compiler) Compile(_ context.Context, r understanding.CompileRequest) (understanding.Compiled, error) {
	c.reqs = append(c.reqs, r)
	return understanding.Compiled{IntentObject: c.out, CanonicalInput: r.CanonicalInput}, c.err
}

type recorder struct{ rejected int }

func (r *recorder) Rejected(context.Context, interpreter.Subject, []understanding.Reason) error {
	r.rejected++
	return nil
}
func (r *recorder) OverrideAttempt(context.Context, interpreter.Subject, []string) error { return nil }

type saver struct {
	docs   [][]byte
	actors []string
	err    error
}

func (s *saver) Save(_ context.Context, actor string, a intentstore.Admitted) (intentstore.Ref, error) {
	s.actors = append(s.actors, actor)
	s.docs = append(s.docs, a.JSON)
	return intentstore.Ref{EventID: "recev_01J9X3", Seq: 1, Hash: "sha256:" + strings.Repeat("a", 64)}, s.err
}

func proposal(mutate func(m map[string]any)) []byte {
	m := map[string]any{
		"schema_version": "3.0", "intent_id": "int_01J9X3", "version": 1, "supersedes": nil, "tenant_id": session.TenantID, "correlation_id": corr, "created_at": "2026-10-01T09:47:03Z",
		"principal":   map[string]any{"principal_id": session.PrincipalID, "principal_type": "unset", "authn_ref": session.AuthnRef, "asserted_by": "unset"},
		"source":      map[string]any{"mode": "MODE_1", "channel": "text", "locale_detected": []any{"en-NG"}, "asr_confidence": nil, "raw_ref": "unset"},
		"pack":        map[string]any{"pack_id": "fin", "pack_version": "1.0.0"},
		"intent_node": "fin.payments.exception.investigate",
		"slots": map[string]any{
			"subject_resource": map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX88213"},
			"question_type":    map[string]any{"type": "Reference", "value": "STATUS"},
		},
		"ambiguity_flags": []any{}, "desired_outcome_refs": []any{"fin.outcome.exception.status_explained"}, "status": "VALIDATED", "sig": map[string]any{"key_id": "unsigned", "value": "unsigned"},
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
	port *interpreterport.Port
	sess *sessions
	comp *compiler
	rec  *recorder
	save *saver
	adm  *admissions
}

func pack(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newWorld(t *testing.T, out []byte) *world {
	t.Helper()
	raw := pack(t)
	view, err := understanding.NewPackView(raw)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := understanding.NewGate(64<<10, view)
	if err != nil {
		t.Fatal(err)
	}
	required, err := interpreterport.RequiredFromPack(raw)
	if err != nil {
		t.Fatal(err)
	}
	w := &world{sess: &sessions{out: interpreterport.Resolved{Session: session, ActorKind: "customer"}}, comp: &compiler{out: out}, rec: &recorder{}, save: &saver{}, adm: newAdmissions()}
	in, err := interpreter.New(interpreter.Config{Compiler: w.comp, Gate: gate, Recorder: w.rec, Saver: w.save})
	if err != nil {
		t.Fatal(err)
	}
	w.port, err = interpreterport.New(interpreterport.Config{Sessions: w.sess, Interpreter: in, Clock: clock.NewManual(now), PackRef: packRef, Required: required, Admissions: w.adm})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func turn(text string) orchestrator.Turn {
	return orchestrator.Turn{InvestigationID: "inv_01J9X3", CorrelationID: corr, Text: text, InputReceivedRef: "recev_01J9X0", RequestID: reqID}
}

// A normal turn: the admitted intent becomes ADMITTED with the reference `<intent_id>@<version>` and nothing else of the intent (INV-02); the canonical input is built from the verified
// session and the turn, and the actor of the save is `<kind>:<principal>`.
func TestAnAdmittedTurnIsAdmittedAndTheCanonicalInputIsBuiltFromTheSessionAndTheTurn(t *testing.T) {
	w := newWorld(t, proposal(nil))
	got, err := w.port.Interpret(context.Background(), tenant, turn("  Wetin happen to the 20k wey I send Tunde this morning?\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got != (orchestrator.Interpretation{Kind: orchestrator.Admitted, IntentRef: "int_01J9X3@1"}) {
		t.Fatalf("got %+v", got)
	}
	if len(w.save.actors) != 1 || w.save.actors[0] != "customer:usr_88213" {
		t.Errorf("actors %v", w.save.actors)
	}
	if len(w.comp.reqs) != 1 || w.comp.reqs[0].PackRef != packRef || len(w.comp.reqs[0].OpenIntent) != 0 {
		t.Fatalf("request %+v", w.comp.reqs)
	}
	var ci map[string]any
	if err := json.Unmarshal(w.comp.reqs[0].CanonicalInput, &ci); err != nil {
		t.Fatal(err)
	}
	// received_at is the injected clock's reading cut to whole seconds (the contract's format), so a retry of the same request within the same second builds the same canonical input (INV-21).
	want := map[string]any{"schema_version": "2.0", "request_id": reqID, "correlation_id": corr, "tenant_id": "ten_demo", "session_id": "ses_7a1b", "principal_ref": "usr_88213", "channel": "text",
		"mode": "MODE_1", "text": "Wetin happen to the 20k wey I send Tunde this morning?", "received_at": "2026-11-17T09:30:15Z"}
	if len(ci) != len(want) {
		t.Errorf("canonical input has %d members, want %d: %v", len(ci), len(want), ci)
	}
	for k, v := range want {
		if ci[k] != v {
			t.Errorf("canonical input %s = %v, want %v", k, ci[k], v)
		}
	}
}

// The identity is the verified session's, whatever the text says, and the resolver may not answer for another principal or tenant (INV-12, AGENT-10).
func TestTheResolverMayNotAnswerForAnotherPrincipalOrTenantAndNothingIsSent(t *testing.T) {
	for name, mutate := range map[string]func(*interpreterport.Resolved){
		"another principal":           func(r *interpreterport.Resolved) { r.Session.PrincipalID = "usr_99999" },
		"another tenant":              func(r *interpreterport.Resolved) { r.Session.TenantID = "ten_other" },
		"an invalid session":          func(r *interpreterport.Resolved) { r.Session.PrincipalType = "End User" },
		"an actor kind off its shape": func(r *interpreterport.Resolved) { r.ActorKind = "Customer:x" },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, proposal(nil))
			mutate(&w.sess.out)
			_, err := w.port.Interpret(context.Background(), tenant, turn("hello"))
			if !errors.Is(err, interpreterport.ErrSession) || len(w.comp.reqs) != 0 {
				t.Fatalf("err %v, requests %d", err, len(w.comp.reqs))
			}
		})
	}
}

// The actor the Record writes is the resolver's kind, never a constant of this port.
func TestTheActorIsTheKindTheResolverGaveAndThePrincipalOfTheSession(t *testing.T) {
	w := newWorld(t, proposal(nil))
	w.sess.out.ActorKind = "operator"
	if _, err := w.port.Interpret(context.Background(), tenant, turn("hello")); err != nil || len(w.save.actors) != 1 || w.save.actors[0] != "operator:usr_88213" {
		t.Fatalf("err %v, actors %v", err, w.save.actors)
	}
}

func TestAResolverFailureIsReturnedUnchangedAndNothingIsSent(t *testing.T) {
	w := newWorld(t, proposal(nil))
	boom := errors.New("no such session")
	w.sess.err = boom
	if _, err := w.port.Interpret(context.Background(), tenant, turn("hello")); !errors.Is(err, boom) || len(w.comp.reqs) != 0 {
		t.Fatalf("err %v, requests %d", err, len(w.comp.reqs))
	}
}

// The text is trimmed; an empty or over-long one is a bad turn and is refused before anything is called; the bound counts characters, not bytes (canonical-input.v2: 1 to 2000).
func TestTheTextIsTrimmedAndItsBoundsAreEnforcedInCharacters(t *testing.T) {
	for name, text := range map[string]string{"empty": "", "blank": " \n\t ", "too long": strings.Repeat("a", 2001), "too long in characters": strings.Repeat("é", 2001)} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, proposal(nil))
			if _, err := w.port.Interpret(context.Background(), tenant, turn(text)); !errors.Is(err, interpreterport.ErrTurn) || w.sess.calls != 0 || len(w.comp.reqs) != 0 {
				t.Fatalf("err %v, resolver calls %d, requests %d", err, w.sess.calls, len(w.comp.reqs))
			}
		})
	}
	for name, text := range map[string]string{"2000 bytes": strings.Repeat("a", 2000), "2000 characters of two bytes": strings.Repeat("é", 2000), "padded 2000": "  " + strings.Repeat("a", 2000) + "\n"} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, proposal(nil))
			if _, err := w.port.Interpret(context.Background(), tenant, turn(text)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAnUntypedRequestIDOrCorrelationIsABadTurn(t *testing.T) {
	for name, mutate := range map[string]func(*orchestrator.Turn){
		"upper-case uuid": func(x *orchestrator.Turn) { x.RequestID = strings.ToUpper(reqID) },
		"not a uuid":      func(x *orchestrator.Turn) { x.RequestID = "req_01J9X3" },
		"empty request":   func(x *orchestrator.Turn) { x.RequestID = "" },
		"bad correlation": func(x *orchestrator.Turn) { x.CorrelationID = "c1" },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, proposal(nil))
			tn := turn("hello")
			mutate(&tn)
			if _, err := w.port.Interpret(context.Background(), tenant, tn); !errors.Is(err, interpreterport.ErrTurn) || len(w.comp.reqs) != 0 {
				t.Fatalf("err %v, requests %d", err, len(w.comp.reqs))
			}
		})
	}
}

// What the intent amounts to (A262, point 4): an ambiguity flag, a required slot that is missing or not RESOLVED means NEEDS_CLARIFICATION; an optional slot that is unresolved does not.
func TestWhatTheAdmittedIntentAmountsTo(t *testing.T) {
	cases := map[string]struct {
		mutate func(m map[string]any)
		want   orchestrator.InterpretationKind
	}{
		"an ambiguity flag":          {func(m map[string]any) { m["ambiguity_flags"] = []any{"COUNTERPARTY_MULTIPLE_MATCH"} }, orchestrator.NeedsClarification},
		"a required slot unresolved": {func(m map[string]any) { unresolve(m, "UNRESOLVED") }, orchestrator.NeedsClarification},
		"a required slot ambiguous":  {func(m map[string]any) { unresolve(m, "AMBIGUOUS") }, orchestrator.NeedsClarification},
		"a required slot absent":     {func(m map[string]any) { unresolve(m, "ABSENT") }, orchestrator.NeedsClarification},
		"a required slot missing":    {func(m map[string]any) { delete(m["slots"].(map[string]any), "question_type") }, orchestrator.NeedsClarification},
		"an optional slot unresolved": {func(m map[string]any) {
			m["slots"].(map[string]any)["amount"] = map[string]any{"type": "Quantity", "status": "UNRESOLVED", "value": 2000000, "unit": "NGN", "surface_form": "20k"}
		}, orchestrator.Admitted},
		"a required slot with no status field": {nil, orchestrator.Admitted},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, proposal(c.mutate))
			got, err := w.port.Interpret(context.Background(), tenant, turn("hello"))
			if err != nil || got.Kind != c.want || got.IntentRef != "int_01J9X3@1" {
				t.Fatalf("got %+v, err %v, want %s", got, err, c.want)
			}
		})
	}
}

// unresolve turns the required resource slot into one the gate still admits and the pack still requires resolved: no resolved_ref, another status.
func unresolve(m map[string]any, status string) {
	slot := m["slots"].(map[string]any)["subject_resource"].(map[string]any)
	delete(slot, "resolved_ref")
	slot["status"] = status
}

// A refusal at the gate is GATE_FAILED with no error and no reference (the Interpreter recorded it first); the reasons are not passed on.
func TestARefusalAtTheGateIsGateFailedWithNoErrorAndNoReference(t *testing.T) {
	w := newWorld(t, proposal(func(m map[string]any) { m["intent_node"] = "fin.transfer.send" }))
	got, err := w.port.Interpret(context.Background(), tenant, turn("hello"))
	if err != nil || got != (orchestrator.Interpretation{Kind: orchestrator.GateFailed}) {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if w.rec.rejected != 1 || len(w.save.actors) != 0 {
		t.Errorf("rejected %d, saved %d", w.rec.rejected, len(w.save.actors))
	}
}

// Every other failure stops the runner: it is returned as the Interpreter's own kind, with no interpretation and no utterance in its text (AGENT-13).
func TestAFailureThatIsNotARefusalIsReturnedAndCarriesNoUtterance(t *testing.T) {
	const text = "Wetin happen to the 20k wey I send Tunde this morning?"
	w := newWorld(t, nil)
	w.comp.err = &understanding.CompileError{Failure: understanding.FailureUnavailable}
	got, err := w.port.Interpret(context.Background(), tenant, turn(text))
	if !errors.Is(err, interpreter.ErrCompile) || got != (orchestrator.Interpretation{}) {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if strings.Contains(err.Error(), "Tunde") || strings.Contains(err.Error(), "20k") {
		t.Errorf("the error carries the utterance: %v", err)
	}
	w2 := newWorld(t, proposal(nil))
	w2.save.err = errors.New("db down")
	if _, err := w2.port.Interpret(context.Background(), tenant, turn(text)); !errors.Is(err, interpreter.ErrSave) {
		t.Errorf("err %v, want ErrSave", err)
	}
}

// An admitted intent for a node the pack does not describe cannot be classified: that is an error, never a quiet ADMITTED.
func TestAnAdmittedNodeThePackDoesNotDescribeIsAnErrorNotAnAdmission(t *testing.T) {
	raw := pack(t)
	required, err := interpreterport.RequiredFromPack(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := required.RequiredSlots("fin.payments.exception.investigate")
	if !ok || strings.Join(got, ",") != "question_type,subject_resource" {
		t.Fatalf("required %v %v, want question_type and subject_resource, sorted", got, ok)
	}
	if got, ok := required.RequiredSlots("fin.meta.out_of_scope"); !ok || len(got) != 0 {
		t.Fatalf("out of scope: %v %v", got, ok)
	}
	got[0] = "tampered"
	if again, _ := required.RequiredSlots("fin.payments.exception.investigate"); again[0] != "question_type" {
		t.Fatal("the slice handed out is the port's own: a caller changed what the next call returns")
	}
	if _, ok := required.RequiredSlots("fin.unknown"); ok {
		t.Fatal("an unknown node has a required set")
	}
	w := newWorld(t, proposal(nil))
	p, err := interpreterport.New(interpreterport.Config{Sessions: w.sess, Interpreter: stubInterpreter{w: w}, Clock: clock.NewManual(now), PackRef: packRef, Required: emptyRequired{}, Admissions: w.adm})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Interpret(context.Background(), tenant, turn("hello")); !errors.Is(err, interpreterport.ErrPack) {
		t.Fatalf("err %v, want ErrPack", err)
	}
}

type emptyRequired struct{}

func (emptyRequired) RequiredSlots(string) ([]string, bool) { return nil, false }

// stubInterpreter runs the world's real Interpreter; it exists so the test above can swap only the required-slot source.
type stubInterpreter struct{ w *world }

func (s stubInterpreter) Interpret(ctx context.Context, in interpreter.Input) (interpreter.Result, error) {
	raw, _ := os.ReadFile("../../../../../packs/fintech_v1.json")
	view, _ := understanding.NewPackView(raw)
	gate, _ := understanding.NewGate(64<<10, view)
	real, _ := interpreter.New(interpreter.Config{Compiler: s.w.comp, Gate: gate, Recorder: s.w.rec, Saver: s.w.save})
	return real.Interpret(ctx, in)
}

func TestRequiredFromPackRefusesWhatItCannotTrust(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":               `{`,
		"no nodes":               `{"intent_nodes":[]}`,
		"repeated node":          `{"intent_nodes":[{"node_id":"a.b","slots":[]},{"node_id":"a.b","slots":[]}]}`,
		"repeated slot":          `{"intent_nodes":[{"node_id":"a.b","slots":[{"name":"x","required":true},{"name":"x","required":true}]}]}`,
		"data after":             `{"intent_nodes":[{"node_id":"a.b","slots":[]}]} {}`,
		"required not a boolean": `{"intent_nodes":[{"node_id":"a.b","slots":[{"name":"x","required":"yes"}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := interpreterport.RequiredFromPack([]byte(raw)); !errors.Is(err, interpreterport.ErrPack) {
				t.Fatalf("err %v, want ErrPack", err)
			}
		})
	}
}

func TestNewRefusesAnIncompleteConfiguration(t *testing.T) {
	w := newWorld(t, proposal(nil))
	full := interpreterport.Config{Sessions: w.sess, Interpreter: stubInterpreter{w: w}, Clock: clock.NewManual(now), PackRef: packRef, Required: emptyRequired{}, Admissions: w.adm}
	for name, mutate := range map[string]func(*interpreterport.Config){
		"no sessions": func(c *interpreterport.Config) { c.Sessions = nil }, "no interpreter": func(c *interpreterport.Config) { c.Interpreter = nil },
		"no clock": func(c *interpreterport.Config) { c.Clock = nil }, "no required": func(c *interpreterport.Config) { c.Required = nil },
		"no admissions": func(c *interpreterport.Config) { c.Admissions = nil }, "no pack ref": func(c *interpreterport.Config) { c.PackRef = "" }, "bad pack ref": func(c *interpreterport.Config) { c.PackRef = "Fin 1" },
	} {
		t.Run(name, func(t *testing.T) {
			c := full
			mutate(&c)
			if _, err := interpreterport.New(c); !errors.Is(err, interpreterport.ErrNotUsable) {
				t.Fatalf("err %v, want ErrNotUsable", err)
			}
		})
	}
}

// The deadline of the caller reaches the Interpreter: the adapter adds none and removes none (a context deadline on every call).
func TestTheCallersContextReachesTheInterpreter(t *testing.T) {
	w := newWorld(t, proposal(nil))
	type key struct{}
	seen := false
	w.port = mustPort(t, w, func(ctx context.Context, in interpreter.Input) (interpreter.Result, error) {
		seen = ctx.Value(key{}) == "marker"
		return stubInterpreter{w: w}.Interpret(ctx, in)
	})
	if _, err := w.port.Interpret(context.WithValue(context.Background(), key{}, "marker"), tenant, turn("hello")); err != nil || !seen {
		t.Fatalf("err %v, seen %v", err, seen)
	}
}

type fnInterpreter func(context.Context, interpreter.Input) (interpreter.Result, error)

func (f fnInterpreter) Interpret(ctx context.Context, in interpreter.Input) (interpreter.Result, error) {
	return f(ctx, in)
}

func mustPort(t *testing.T, w *world, f func(context.Context, interpreter.Input) (interpreter.Result, error)) *interpreterport.Port {
	t.Helper()
	required, err := interpreterport.RequiredFromPack(pack(t))
	if err != nil {
		t.Fatal(err)
	}
	p, err := interpreterport.New(interpreterport.Config{Sessions: w.sess, Interpreter: fnInterpreter(f), Clock: clock.NewManual(now), PackRef: packRef, Required: required, Admissions: w.adm})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// UND-17 (noema#580; A219; REQ-INT-001, REQ-INT-002; INV-02, INV-12; AGENT-09): the intent the port gets compiled and saved carries the object_ref of the INPUT_RECEIVED the Record
// holds for the turn, read from the stored event as the session's tenant, never from the request or the model.

type capturing struct {
	w    *world
	last interpreter.Input
}

func (c *capturing) Interpret(ctx context.Context, in interpreter.Input) (interpreter.Result, error) {
	c.last = in
	return stubInterpreter{w: c.w}.Interpret(ctx, in)
}

func TestTheInterpreterIsGivenTheRecordedInputsObjectRefFromTheStoredEvent(t *testing.T) {
	w := newWorld(t, proposal(nil))
	c := &capturing{w: w}
	w.port = mustPort(t, w, c.Interpret)
	if _, err := w.port.Interpret(context.Background(), tenant, turn("hello")); err != nil {
		t.Fatal(err)
	}
	if c.last.RawRef != objectRef {
		t.Fatalf("the Interpreter was given RawRef %q, want the stored event's %q", c.last.RawRef, objectRef)
	}
	if len(w.adm.asked) != 1 || w.adm.asked[0] != "ten_demo/recev_01J9X0" {
		t.Fatalf("the Record was asked %v, want the session's tenant and the turn's InputReceivedRef", w.adm.asked)
	}
}

func TestTheSavedIntentsRawRefIsTheRecordedInputsAndNotTheModels(t *testing.T) {
	for _, model := range []string{"unset", "req_01J9X3ZZZZZZZZZZZZZZZZZZZZ"} {
		w := newWorld(t, proposal(func(m map[string]any) { m["source"].(map[string]any)["raw_ref"] = model }))
		if _, err := w.port.Interpret(context.Background(), tenant, turn("hello")); err != nil {
			t.Fatal(err)
		}
		if len(w.save.docs) != 1 || !strings.Contains(string(w.save.docs[0]), `"raw_ref":"`+objectRef+`"`) || strings.Contains(string(w.save.docs[0]), `"raw_ref":"`+model+`"`) {
			t.Fatalf("model %q: saved %s, want raw_ref %s", model, w.save.docs, objectRef)
		}
	}
}

// Fail closed: an event that is not this turn's INPUT_RECEIVED, an unreadable Record or an object_ref that is not a minted input ID stops the turn before the model service is called.
func TestAnInputTheRecordDoesNotConfirmStopsTheTurnBeforeAnythingIsCalledOrStored(t *testing.T) {
	bad := map[string]func(a *admissions){
		"lookup fails":      func(a *admissions) { a.err = errors.New("synthetic: no such event") },
		"another event":     func(a *admissions) { a.out.EventType = "INPUT_DEDUPLICATED" },
		"another tenant":    func(a *admissions) { a.out.TenantID = "ten_other" },
		"another principal": func(a *admissions) { a.out.PrincipalID = "usr_other" },
		"empty principal":   func(a *admissions) { a.out.PrincipalID = "" },
		"another corr":      func(a *admissions) { a.out.CorrelationID = "corr_other1" },
		"another request":   func(a *admissions) { a.out.RequestID = "11111111-1111-4111-8111-111111111111" },
		"no object ref":     func(a *admissions) { a.out.ObjectRef = "" },
		"unset object ref":  func(a *admissions) { a.out.ObjectRef = "unset" },
		"malformed ref":     func(a *admissions) { a.out.ObjectRef = "req_short" },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, proposal(nil))
			mutate(w.adm)
			got, err := w.port.Interpret(context.Background(), tenant, turn("hello"))
			if err == nil || got != (orchestrator.Interpretation{}) {
				t.Fatalf("got %+v, %v; want an error and no interpretation", got, err)
			}
			if len(w.comp.reqs) != 0 || len(w.save.actors) != 0 || w.rec.rejected != 0 {
				t.Fatalf("compile %d, saved %d, rejected %d: want nothing", len(w.comp.reqs), len(w.save.actors), w.rec.rejected)
			}
		})
	}
}

// Property: whatever the model wrote in raw_ref, a saved intent carries the Record's.
func TestPropertyTheSavedRawRefIsAlwaysTheRecordedOne(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		model := rapid.String().Draw(rt, "model")
		w := newWorld(t, proposal(func(m map[string]any) { m["source"].(map[string]any)["raw_ref"] = model }))
		if _, err := w.port.Interpret(context.Background(), tenant, turn("hello")); err != nil {
			rt.Fatal(err)
		}
		var doc struct {
			Source struct {
				RawRef string `json:"raw_ref"`
			} `json:"source"`
		}
		if err := json.Unmarshal(w.save.docs[0], &doc); err != nil || doc.Source.RawRef != objectRef {
			rt.Fatalf("saved raw_ref %q (%v) for model value %q, want %q", doc.Source.RawRef, err, model, objectRef)
		}
	})
}
