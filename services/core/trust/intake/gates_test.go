package intake_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// REQ-TRU-002, REQ-TRU-003, REQ-NFR-002, INV-16: T1 to T6 of PERD 23.3 refuse before anything about a target is read, each with the one outcome and the one core reason
// code the step owns, and every other way a step can go (an error, a panic) ends in UNKNOWN, never ALLOW. Steps T7 to T15 are not built in this PR, so a request that passes
// T1 to T6 ends in UNKNOWN INTERNAL_ERROR at T7: the intake cannot allow anything yet.

const (
	tenant    = "ten_synth01"
	principal = "usr_synth01"
	node      = "fin.payments.exception.investigate"
	intentRef = "int_synth0001@2"
)

// spy records every call, by port and method, in order; its answers are set per test, and a fault is injected by name.
type spy struct {
	calls []string
	// the answers
	intent      intake.AdmittedIntent
	latest      int
	getErr      error
	verifyErr   error
	needsWrite  bool
	enabled     bool
	capability  intake.Capability
	capPresent  bool
	inScope     bool
	roleAllowed bool
	// faults: the method named here returns an error or panics
	failAt  string
	panicAt string
}

func newSpy() *spy {
	return &spy{
		intent: intake.AdmittedIntent{TenantID: tenant, IntentID: "int_synth0001", Version: 2, IntentNode: node, IntentHash: "sha256:" + hex64(), CorrelationID: "corr_synth0001",
			PrincipalID: principal, View: decide.IntentView{Version: 2}},
		latest: 2, enabled: true, capability: intake.Capability{ID: "fin.cap.lookup_transaction", Registered: true}, capPresent: true, inScope: true, roleAllowed: true,
	}
}

func hex64() string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = "0123456789abcdef"[i%16]
	}
	return string(b)
}

var errFault = errors.New("injected fault")

func (s *spy) hit(name string) error {
	s.calls = append(s.calls, name)
	if s.panicAt == name {
		panic("injected panic in " + name)
	}
	if s.failAt == name {
		return errFault
	}
	return nil
}

func (s *spy) Get(_ context.Context, _, _ string, _ int) (intake.AdmittedIntent, error) {
	if err := s.hit("Intents.Get"); err != nil {
		return intake.AdmittedIntent{}, err
	}
	return s.intent, s.getErr
}
func (s *spy) LatestVersion(context.Context, string, string) (int, error) {
	return s.latest, s.hit("Intents.LatestVersion")
}
func (s *spy) Verify(context.Context, intake.AdmittedIntent) error {
	if err := s.hit("Intents.Verify"); err != nil {
		return err
	}
	return s.verifyErr
}
func (s *spy) NeedsWrite(context.Context, intake.AdmittedIntent) (bool, error) {
	return s.needsWrite, s.hit("Writes.NeedsWrite")
}
func (s *spy) Enabled(context.Context, string) (bool, error) {
	return s.enabled, s.hit("Writes.Enabled")
}
func (s *spy) Requested(context.Context, intake.AdmittedIntent) (intake.Capability, bool, error) {
	return s.capability, s.capPresent, s.hit("Capabilities.Requested")
}
func (s *spy) InScope(context.Context, string, string) (bool, error) {
	return s.inScope, s.hit("Scope.InScope")
}
func (s *spy) RoleAllowed(context.Context, string, string) (bool, error) {
	return s.roleAllowed, s.hit("Authority.RoleAllowed")
}

// later stands for the ports of T10 to T12, which these tests never reach: T7 is not built, so the evaluation ends there. If one is called, the step fails closed.
type later struct{}

func (later) Reconcile(context.Context, string, string) (intake.Reconciliation, error) {
	return intake.Reconciliation{}, errors.New("not reached")
}
func (later) Evaluate(context.Context, intake.PolicyQuery) (intake.PolicyEvaluation, error) {
	return intake.PolicyEvaluation{}, errors.New("not reached")
}
func (later) Applicable(context.Context, intake.ClockQuery) (intake.ClockSet, error) {
	return intake.ClockSet{}, errors.New("not reached")
}
func (later) Route(context.Context, intake.ClockQuery, []clocks.Status) (intake.ClockRouting, error) {
	return intake.ClockRouting{}, errors.New("not reached")
}

func (s *spy) service() *intake.Service {
	return &intake.Service{
		Ports:       intake.Ports{Intents: s, Writes: s, Capabilities: s, Scope: s, Authority: s, Reconciler: later{}, Policies: later{}, Clocks: later{}},
		Clock:       clock.NewManual(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)), // SYNTHETIC
		PackVersion: "1.0.0",
	}
}

func request() intake.Request {
	return intake.Request{Session: intake.Session{TenantID: tenant, PrincipalID: principal, Role: "customer"}, CorrelationID: "corr_synth0001", IntentRef: intentRef}
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func eval(t *testing.T, s *spy, r intake.Request) intake.Result {
	t.Helper()
	res, err := s.service().Evaluate(ctx(t), r)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return res
}

func reasons(d decide.Decision) []string {
	var out []string
	for _, r := range d.Reasons() {
		out = append(out, r.String())
	}
	return out
}

func last(res intake.Result) decide.TraceEntry { return res.Trace[len(res.Trace)-1] }

// want asserts the one outcome, the one reason code, and that the evaluation ended at the step with a terminal result.
func want(t *testing.T, name string, res intake.Result, outcome decide.Outcome, reason string, step decide.GateStep) {
	t.Helper()
	if got := res.Decision.Outcome(); got != outcome {
		t.Errorf("%s: outcome %s, want %s", name, got, outcome)
	}
	if got := reasons(res.Decision); !reflect.DeepEqual(got, []string{reason}) {
		t.Errorf("%s: reasons %v, want [%s]", name, got, reason)
	}
	if len(res.Trace) == 0 || last(res).Step != step || last(res).Result != decide.StepTerminal {
		t.Errorf("%s: trace %v, want it to end at %v terminal", name, res.Trace, step)
	}
}

func TestEveryGateStepRefusesWithItsOwnOutcomeAndReasonCode(t *testing.T) {
	for _, c := range []struct {
		name    string
		set     func(s *spy, r *intake.Request)
		outcome decide.Outcome
		reason  string
		step    decide.GateStep
	}{
		{"T1: the intent does not exist", func(s *spy, r *intake.Request) { s.getErr = intake.ErrNotFound }, decide.Reject, "INTENT_INVALID", decide.T1AdmittedIntent},
		{"T1: the intent is another tenant's", func(s *spy, r *intake.Request) { s.intent.TenantID = "ten_synth02" }, decide.Reject, "INTENT_INVALID", decide.T1AdmittedIntent},
		{"T1: a newer version exists", func(s *spy, r *intake.Request) { s.latest = 3 }, decide.Reject, "INTENT_INVALID", decide.T1AdmittedIntent},
		{"T1: the stored intent no longer verifies", func(s *spy, r *intake.Request) { s.verifyErr = intake.ErrInvalid }, decide.Reject, "INTENT_INVALID", decide.T1AdmittedIntent},
		{"T1: the stored version is not the one asked for", func(s *spy, r *intake.Request) { s.intent.Version = 1 }, decide.Reject, "INTENT_INVALID", decide.T1AdmittedIntent},
		{"T1: a malformed reference", func(s *spy, r *intake.Request) { r.IntentRef = "int_synth0001" }, decide.Reject, "INTENT_INVALID", decide.T1AdmittedIntent},
		{"T1: a reference with version zero", func(s *spy, r *intake.Request) { r.IntentRef = "int_synth0001@0" }, decide.Reject, "INTENT_INVALID", decide.T1AdmittedIntent},
		{"T2: no tenant in the session", func(s *spy, r *intake.Request) { r.Session.TenantID = "" }, decide.Reject, "UNAUTHENTICATED", decide.T2Session},
		{"T2: no principal in the session", func(s *spy, r *intake.Request) { r.Session.PrincipalID = "" }, decide.Reject, "UNAUTHENTICATED", decide.T2Session},
		{"T2: the intent names another principal", func(s *spy, r *intake.Request) { s.intent.PrincipalID = "usr_synth02" }, decide.Reject, "UNAUTHENTICATED", decide.T2Session},
		{"T3: a write and writes are disabled", func(s *spy, r *intake.Request) { s.needsWrite, s.enabled = true, false }, decide.Unknown, "WRITES_DISABLED", decide.T3WritesEnabled},
		{"T4: the capability is prohibited", func(s *spy, r *intake.Request) { s.capability.Prohibited = true }, decide.Reject, "PROHIBITED_CAPABILITY", decide.T4Capability},
		{"T4: the capability is not registered", func(s *spy, r *intake.Request) { s.capability.Registered = false }, decide.Reject, "PROHIBITED_CAPABILITY", decide.T4Capability},
		{"T5: the intent node is out of scope", func(s *spy, r *intake.Request) { s.inScope = false }, decide.Reject, "OUT_OF_SCOPE", decide.T5Scope},
		{"T6: the role is not allowed", func(s *spy, r *intake.Request) { s.roleAllowed = false }, decide.Reject, "NOT_AUTHORIZED", decide.T6TechnicalAuthority},
		{"T6: the session has no role", func(s *spy, r *intake.Request) { r.Session.Role = "" }, decide.Reject, "NOT_AUTHORIZED", decide.T6TechnicalAuthority},
	} {
		s, r := newSpy(), request()
		c.set(s, &r)
		res := eval(t, s, r)
		want(t, c.name, res, c.outcome, c.reason, c.step)
		if res.Decision.Outcome() == decide.Reject && c.step >= decide.T6TechnicalAuthority && res.Decision.Authority().Technical != decide.Fail {
			t.Errorf("%s: the technical layer is %v, want FAIL", c.name, res.Decision.Authority().Technical)
		}
	}
}

// T3: reads continue when writes are disabled: the switch refuses only a request that leads to a write.
func TestAReadContinuesWhenWritesAreDisabled(t *testing.T) {
	s := newSpy()
	s.needsWrite, s.enabled = false, false
	res := eval(t, s, request())
	if last(res).Step < decide.T4Capability {
		t.Fatalf("the evaluation stopped at %v with writes disabled for a read: %v", last(res).Step, res.Trace)
	}
}

// With every port answering well, T1 to T6 pass and the evaluation ends at T7, which is not built: UNKNOWN INTERNAL_ERROR, never ALLOW.
func TestARequestThatPassesT1ToT6EndsUnknownAtT7BecauseNoLaterStepIsBuilt(t *testing.T) {
	res := eval(t, newSpy(), request())
	if res.Decision.Outcome() != decide.Unknown || !reflect.DeepEqual(reasons(res.Decision), []string{"INTERNAL_ERROR"}) {
		t.Fatalf("decision %s %v, want UNKNOWN INTERNAL_ERROR", res.Decision.Outcome(), reasons(res.Decision))
	}
	if len(res.Trace) != 7 || last(res).Step != decide.T7TargetResolution || last(res).Result != decide.StepFailed {
		t.Fatalf("trace %v, want T1 to T6 passed and T7 failed", res.Trace)
	}
	for i := 0; i < 6; i++ {
		if res.Trace[i].Result != decide.StepPassed {
			t.Errorf("step %d: %v, want PASSED", i+1, res.Trace[i])
		}
	}
}

// REQ-TRU-002 "refuse before read": a refusal at T2 to T6 stops the evaluation before the later steps' ports are called, and nothing about a target is read at all in T1 to T6
// (the intake has no port that could).
func TestARefusalStopsBeforeTheLaterPortsAreCalled(t *testing.T) {
	for _, c := range []struct {
		name     string
		set      func(s *spy, r *intake.Request)
		notCalls []string
	}{
		{"T2", func(s *spy, r *intake.Request) { r.Session.PrincipalID = "" }, []string{"Writes.NeedsWrite", "Writes.Enabled", "Capabilities.Requested", "Scope.InScope", "Authority.RoleAllowed"}},
		{"T4", func(s *spy, r *intake.Request) { s.capability.Prohibited = true }, []string{"Scope.InScope", "Authority.RoleAllowed"}},
		{"T5", func(s *spy, r *intake.Request) { s.inScope = false }, []string{"Authority.RoleAllowed"}},
	} {
		s, r := newSpy(), request()
		c.set(s, &r)
		eval(t, s, r)
		for _, n := range c.notCalls {
			for _, called := range s.calls {
				if called == n {
					t.Errorf("%s: %s was called after the refusal; calls %v", c.name, n, s.calls)
				}
			}
		}
	}
}

// Fault injection (TRU-02, REQ-NFR-002, INV-16): at every port, an error and a panic end the evaluation in UNKNOWN, never ALLOW; and when the fault is at a port, the
// evaluation never reports a step after it as passed.
func TestAnErrorOrAPanicAtAnyPortIsUnknownNeverAllow(t *testing.T) {
	for _, method := range []string{"Intents.Get", "Intents.LatestVersion", "Intents.Verify", "Writes.NeedsWrite", "Writes.Enabled", "Capabilities.Requested", "Scope.InScope", "Authority.RoleAllowed"} {
		for _, mode := range []string{"error", "panic"} {
			s := newSpy()
			s.needsWrite = true // so that T3 reads the switch
			if mode == "error" {
				s.failAt = method
			} else {
				s.panicAt = method
			}
			res := eval(t, s, request())
			if res.Decision.Outcome() == decide.Allow {
				t.Fatalf("%s %s: ALLOW", method, mode)
			}
			if res.Decision.Outcome() != decide.Unknown || !reflect.DeepEqual(reasons(res.Decision), []string{"INTERNAL_ERROR"}) {
				t.Errorf("%s %s: %s %v, want UNKNOWN INTERNAL_ERROR", method, mode, res.Decision.Outcome(), reasons(res.Decision))
			}
		}
	}
}

func TestAPortThatIsMissingOrAContextWithNoDeadlineIsAnErrorNotADecision(t *testing.T) {
	s := newSpy()
	svc := s.service()
	svc.Ports.Scope = nil
	if _, err := svc.Evaluate(ctx(t), request()); err == nil {
		t.Error("a nil port was accepted")
	}
	if _, err := s.service().Evaluate(context.Background(), request()); !errors.Is(err, intake.ErrNoDeadline) {
		t.Errorf("no deadline: %v, want ErrNoDeadline", err)
	}
	var zero intake.Service
	if _, err := zero.Evaluate(ctx(t), request()); err == nil {
		t.Error("the zero Service ran")
	}
}

// INV-21: the same request and the same answers give the same decision and the same trace.
func TestTheSameInputsGiveTheSameDecisionAndTrace(t *testing.T) {
	a, b := eval(t, newSpy(), request()), eval(t, newSpy(), request())
	if !reflect.DeepEqual(a.Trace, b.Trace) || a.Decision.Outcome() != b.Decision.Outcome() || !reflect.DeepEqual(reasons(a.Decision), reasons(b.Decision)) {
		t.Fatalf("two runs differ: %v %v / %v %v", a.Decision.Outcome(), a.Trace, b.Decision.Outcome(), b.Trace)
	}
}
