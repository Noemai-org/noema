package orchestrator

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// The ports of the G1 slice (STM-01). The orchestrator decides, executes and verifies nothing: it asks these services and records what happens. cmd/core's composition
// root (compose.go, EDG-01 slice 3c) is the only place that plugs implementations in. The ports live here, not in internal/contracts, because that directory holds only
// generated types; a port carries no more than opaque references and enums that the contracts already define.

// Session is the authenticated caller: the tenant and principal of the verified token, never of a request body (INV-12, AGENT-10). The HTTP edge builds it from the
// session middleware's result; the orchestrator only passes it on.
type Session struct{ TenantID, PrincipalID string }

// Turn is one admitted customer turn. The input gateway has bounded and validated the text; the orchestrator takes it as given and only requires that it is present.
// InputReceivedRef is the Record event ID of the INPUT_RECEIVED event the input gateway wrote for this turn (A161): the orchestrator never writes that event, and it
// refuses a turn that does not name one.
// RequestID is the client's request ID of this turn (common.v1 RequestId); the admitted event must carry the same one, so an older input of the same actor and
// conversation cannot be paired with different text.
type Turn struct{ InvestigationID, CorrelationID, Text, InputReceivedRef, RequestID string }

// InterpretationKind is what the model service's compiled intent amounts to after the schema gate.
type InterpretationKind string

const (
	Admitted           InterpretationKind = "ADMITTED"
	NeedsClarification InterpretationKind = "NEEDS_CLARIFICATION"
	GateFailed         InterpretationKind = "GATE_FAILED"
)

// Interpretation is the model service's answer, after the schema gate. IntentRef is an opaque reference to the compiled intent; its content never enters the
// orchestrator (model output is untrusted data, INV-02).
type Interpretation struct {
	Kind      InterpretationKind
	IntentRef string
}

// EvidenceRef points at the evidence snapshot the Evidence Layer built for the intent. Unavailable says a source could not be read: Trust, not the orchestrator,
// decides what that means.
type EvidenceRef struct {
	SnapshotID  string
	Unavailable bool
}

// Decision is Trust's answer: the recorded decision, its outcome from the contract enum, and whether what it allows is a read.
type Decision struct {
	ID      string
	Outcome contracts.EnumsV1DecisionOutcome
	Read    bool
}

// Explanation is the rendered answer, or a block by the validator (REQ-EXP-002).
type Explanation struct {
	Text    string
	Blocked bool
}

// Interpreter turns a turn into an interpretation, through the model service.
type Interpreter interface {
	Interpret(ctx context.Context, s Session, t Turn) (Interpretation, error)
}

// Evidence reads what the decision needs, through the Evidence Layer.
type Evidence interface {
	Read(ctx context.Context, s Session, intentRef string) (EvidenceRef, error)
}

// Trust makes the decision (the only place that does).
type Trust interface {
	Decide(ctx context.Context, s Session, intentRef string, e EvidenceRef) (Decision, error)
}

// Explainer renders the answer for a recorded decision.
type Explainer interface {
	Explain(ctx context.Context, s Session, decisionID string) (Explanation, error)
}

// Recorder appends one INVESTIGATION_STATE_CHANGED event to the Record (PERD 32.2). The runner calls it before it goes on, so if it fails nothing else happens.
type Recorder interface {
	AppendChange(ctx context.Context, s Session, c Change) error
}

// Admission is what the Record holds for one event the input gateway wrote: its type and the identity it names. The port fills it from the stored event, reading as the
// session's tenant; the runner compares every field itself.
type Admission struct{ EventType, TenantID, PrincipalID, CorrelationID, RequestID string }

// Admissions finds the recorded event a turn's InputReceivedRef names (A161). It reads the Record as the session's tenant (row-level security), so an event of another
// tenant is not found. An event that does not exist is an error, not an empty Admission.
type Admissions interface {
	Lookup(ctx context.Context, s Session, ref string) (Admission, error)
}

// Ports is every service the runner needs. NewRunner refuses a Ports with any of them missing.
type Ports struct {
	Interpreter Interpreter
	Evidence    Evidence
	Trust       Trust
	Explainer   Explainer
	Recorder    Recorder
	Admissions  Admissions
}

// Errors of the runner. Callers compare with errors.Is.
var (
	// ErrNotWired is returned by NewRunner for a missing machine, table or port: a runner that cannot do everything does nothing.
	ErrNotWired = errors.New("orchestrator: the runner is not fully wired")
	// ErrNoSession is returned for a session without a tenant or a principal.
	ErrNoSession = errors.New("orchestrator: the session has no tenant or no principal")
	// ErrBadTurn is returned for a turn without an investigation ID, a correlation ID or text, or with a control character in an ID.
	ErrBadTurn = errors.New("orchestrator: the turn is not valid")
	// ErrNoInputReceived is returned for a turn whose InputReceivedRef is not the ID of a recorded INPUT_RECEIVED event of this tenant, principal and correlation (A161):
	// nothing is recorded or run.
	ErrNoInputReceived = errors.New("orchestrator: the turn does not name its recorded INPUT_RECEIVED event")
	// ErrRecordFailed wraps a failed append of a state change: the run stopped before it, and nothing after it ran (the Record comes first).
	ErrRecordFailed = errors.New("orchestrator: the Record append failed")
	// ErrBadPortAnswer is returned for an answer outside what the port may give (an unknown kind or outcome); it is refused, never guessed.
	ErrBadPortAnswer = errors.New("orchestrator: a port gave an answer outside its contract")
	// ErrNotARead is returned when Trust allows something that is not a read: this run only reads, so it executes nothing (INV-03).
	ErrNotARead = errors.New("orchestrator: the allowed decision is not a read; a read-only run does not execute")
)
