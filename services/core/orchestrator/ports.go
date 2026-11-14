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
// RequestID is the client's request ID of this turn (a lower-case UUID, canonical-input.v2 request_id); the admitted event must carry the same one, so an older input of the same actor and
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
//
// GrantIDs are the Decision Grants Trust issued with an ALLOW that writes (A204: grants are issued with the decision; no planner calls Trust again in the POC). A
// read has none. IntentRef is the compiled intent the decision was made on, so a run that resumes can evaluate again without the interpretation.
type Decision struct {
	ID        string
	Outcome   contracts.EnumsV1DecisionOutcome
	Read      bool
	GrantIDs  []string
	IntentRef string
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

// ReceiptClass is what the Execution Controller reports for one grant. A timeout, a 5xx or silence is ReceiptUnknown (INV-07, AGENT-07): the write may or may not
// have happened, so the runner verifies by key and never dispatches again. A 200 is not an outcome (AGENT-21): ReceiptApplied only says a receipt exists.
type ReceiptClass string

const (
	ReceiptApplied ReceiptClass = "APPLIED"
	ReceiptVoided  ReceiptClass = "VOIDED"
	ReceiptUnknown ReceiptClass = "UNKNOWN"
)

// Receipt is the Controller's answer for one grant.
type Receipt struct {
	GrantID string
	Class   ReceiptClass
}

// Executor spends one grant and dispatches its write through the Execution Controller (the only place that does). It returns a receipt for every grant it was asked to
// run, a lost reply included. An error means the Controller could not say what happened: the runner stops, and a resume reads the Record.
type Executor interface {
	Execute(ctx context.Context, s Session, grantID string) (Receipt, error)
}

// Verifier gives the Outcome Verdict for a grant's write, verified against the target by the grant's key (AGENT-21). It is the only source of a verdict.
type Verifier interface {
	Verify(ctx context.Context, s Session, decisionID, grantID string) (contracts.EnumsV1Verdict, error)
}

// Progress is what the Record holds for one grant: whether GRANT_SPENT and DISPATCH_ATTEMPTED were written.
type Progress struct{ GrantSpent, DispatchAttempted bool }

// Journal reads Progress from the Record as the session's tenant (A204: the Record is the truth). A grant that is spent is never spent again.
type Journal interface {
	Progress(ctx context.Context, s Session, investigationID, grantID string) (Progress, error)
}

// WritePorts are the services only the write path needs. NewWriteRunner refuses any missing.
type WritePorts struct {
	Executor Executor
	Verifier Verifier
	Journal  Journal
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
	// ErrRecoveryBudgetSpent is returned when Trust allows a further write after the recovery budget is spent: the run stops in EVALUATING and executes nothing more.
	ErrRecoveryBudgetSpent = errors.New("orchestrator: the recovery budget is spent; another write is not executed")
	// ErrOutcomeUnknown is returned when the Verifier could not tell whether the write happened. The run is recorded as RECOVERING and stops: nothing is executed
	// again and no new decision is asked for, because a new grant could apply the same write twice (INV-07, AGENT-07, AGENT-08).
	ErrOutcomeUnknown = errors.New("orchestrator: the outcome of the write is unknown; the run stops and nothing is dispatched again")
	// ErrNoGrant is returned for an allowed write that does not carry exactly one grant (the POC has no multi-step plan).
	ErrNoGrant = errors.New("orchestrator: the allowed write does not carry exactly one grant")
	// ErrNotResumable is returned for a stored state the write path cannot resume from.
	ErrNotResumable = errors.New("orchestrator: the investigation is not in a state the write path resumes from")
	// ErrNotARead is returned when Trust allows something that is not a read: this run only reads, so it executes nothing (INV-03).
	ErrNotARead = errors.New("orchestrator: the allowed decision is not a read; a read-only run does not execute")
)
