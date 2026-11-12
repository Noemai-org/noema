package orchestrator

import (
	"context"
	"fmt"
	"regexp"
	"unicode"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// inputReceived is the Record event type the input gateway writes when it admits a turn (PERD 28.2, entry of RECEIVED).
const inputReceived = "INPUT_RECEIVED"

// requestID is the pattern of a request ID (common.v1 RequestId) and recordEventID that of a Record event ID (common.v1 RecordEventId). \A and \z, not ^ and $, so
// text before or a trailing newline does not match (A138).
var requestID = regexp.MustCompile(`\Areq_[0-9A-Za-z]{2,32}\z`)

var recordEventID = regexp.MustCompile(`\Arecev_[0-9A-Za-z]{2,32}\z`)

// Runner walks one investigation through the PERD 28.2 table (STM-01). It never sets a state itself: every move is a Command applied by the Machine, so an arrow that is
// not in the table is refused. Each applied move is appended to the Record before the run goes on (PERD 32.4, EC-E16); if the append fails the run stops, and nothing
// after it runs and no answer is returned. It decides, executes and verifies nothing: the Ports do.
type Runner struct {
	m *Machine
	p Ports
}

// NewRunner refuses a missing machine, a machine with no table, or any missing port.
func NewRunner(m *Machine, p Ports) (*Runner, error) {
	switch {
	case m == nil || m.Table == nil:
		return nil, fmt.Errorf("%w: no machine or table", ErrNotWired)
	case p.Interpreter == nil || p.Evidence == nil || p.Trust == nil || p.Explainer == nil || p.Recorder == nil || p.Admissions == nil:
		return nil, fmt.Errorf("%w: a port is missing", ErrNotWired)
	}
	return &Runner{m: m, p: p}, nil
}

// Result is where a run ended: the investigation as it is, the changes it made (each already recorded), and the answer, which is set only after COMPLETED was recorded.
type Result struct {
	Investigation Investigation
	Changes       []Change
	Answer        string
}

type run struct {
	ctx     context.Context
	r       *Runner
	s       Session
	inv     Investigation
	changes []Change
}

// move applies one command and records it before the run continues. The investigation advances only when the append succeeded.
func (u *run) move(to State) error {
	next, ch, err := u.r.m.Apply(u.inv, Command{To: to, ExpectedVersion: u.inv.Version})
	if err != nil {
		return err
	}
	if err := u.r.p.Recorder.AppendChange(u.ctx, u.s, ch); err != nil {
		return fmt.Errorf("%w: moving to %s: %w", ErrRecordFailed, to, err)
	}
	u.inv, u.changes = next, append(u.changes, ch)
	return nil
}

func (u *run) result(answer string) Result {
	return Result{Investigation: u.inv, Changes: append([]Change(nil), u.changes...), Answer: answer}
}

// RunRead runs the read-only path of US-01: RECEIVED, INTERPRETING, EVALUATING, EXPLAINING, COMPLETED. Any other interpretation or decision outcome takes its table
// arrow and the run ends there (a clarification, a confirmation, a wait, a rejection, an escalation and an unknown are other runs' work). An ALLOW that is not a read
// stops the run in EVALUATING with ErrNotARead: it executes nothing. The session must be the authenticated caller's (INV-12). The turn must name the recorded INPUT_RECEIVED event the input gateway wrote (A161); without it the run is refused before anything is recorded or run.
func (r *Runner) RunRead(ctx context.Context, s Session, t Turn) (Result, error) {
	if s.TenantID == "" || s.PrincipalID == "" {
		return Result{}, ErrNoSession
	}
	if t.Text == "" || !plainID(t.InvestigationID) || !plainID(t.CorrelationID) || !requestID.MatchString(t.RequestID) {
		return Result{}, ErrBadTurn
	}
	if !recordEventID.MatchString(t.InputReceivedRef) {
		return Result{}, ErrNoInputReceived
	}
	// The shape proves nothing: the Record must confirm the event, and that it is this turn's input (tenant, principal, correlation and request ID), before anything runs.
	a, err := r.p.Admissions.Lookup(ctx, s, t.InputReceivedRef)
	if err != nil {
		return Result{}, fmt.Errorf("%w: lookup: %w", ErrNoInputReceived, err)
	}
	// The session's tenant and principal and the turn's correlation and request ID are non-empty here, so an event with an empty field cannot match.
	if a.EventType != inputReceived || a.TenantID != s.TenantID || a.PrincipalID != s.PrincipalID || a.CorrelationID != t.CorrelationID || a.RequestID != t.RequestID {
		return Result{}, ErrNoInputReceived
	}
	u := &run{ctx: ctx, r: r, s: s, inv: Investigation{ID: t.InvestigationID, State: "RECEIVED"}}
	if err := u.move("INTERPRETING"); err != nil {
		return u.result(""), err
	}
	in, err := r.p.Interpreter.Interpret(ctx, s, t)
	if err != nil {
		return u.result(""), fmt.Errorf("orchestrator: interpret: %w", err)
	}
	switch in.Kind {
	case GateFailed:
		return u.finish("REJECTED_AT_GATE")
	case NeedsClarification:
		return u.finish("AWAITING_CLARIFICATION")
	case Admitted:
	default:
		return u.result(""), fmt.Errorf("%w: interpretation kind %q", ErrBadPortAnswer, in.Kind)
	}
	if err := u.move("EVALUATING"); err != nil {
		return u.result(""), err
	}
	ev, err := r.p.Evidence.Read(ctx, s, in.IntentRef)
	if err != nil {
		return u.result(""), fmt.Errorf("orchestrator: evidence: %w", err)
	}
	d, err := r.p.Trust.Decide(ctx, s, in.IntentRef, ev)
	if err != nil {
		return u.result(""), fmt.Errorf("orchestrator: decide: %w", err)
	}
	if to, ok := decisionArrow[d.Outcome]; ok {
		return u.finish(to)
	}
	if d.Outcome != contracts.EnumsV1DecisionOutcomeALLOW {
		return u.result(""), fmt.Errorf("%w: decision outcome %q", ErrBadPortAnswer, d.Outcome)
	}
	if !d.Read {
		return u.result(""), ErrNotARead
	}
	if err := u.move("EXPLAINING"); err != nil {
		return u.result(""), err
	}
	ex, err := r.p.Explainer.Explain(ctx, s, d.ID)
	if err != nil {
		return u.result(""), fmt.Errorf("orchestrator: explain: %w", err)
	}
	if ex.Blocked {
		return u.finish("EXPLAINING_BLOCKED")
	}
	if err := u.move("COMPLETED"); err != nil {
		return u.result(""), err
	}
	return u.result(ex.Text), nil
}

// finish takes the one arrow that ends a run in a non-completing state.
func (u *run) finish(to State) (Result, error) {
	if err := u.move(to); err != nil {
		return u.result(""), err
	}
	return u.result(""), nil
}

// decisionArrow maps every outcome of the contract enum except ALLOW to the state the table takes from EVALUATING (PERD 28.2).
var decisionArrow = map[contracts.EnumsV1DecisionOutcome]State{
	contracts.EnumsV1DecisionOutcomeCLARIFY:  "AWAITING_CLARIFICATION",
	contracts.EnumsV1DecisionOutcomeCONFIRM:  "AWAITING_CONFIRMATION",
	contracts.EnumsV1DecisionOutcomeWAIT:     "WAITING",
	contracts.EnumsV1DecisionOutcomeESCALATE: "ESCALATING",
	contracts.EnumsV1DecisionOutcomeREJECT:   "REJECTED",
	contracts.EnumsV1DecisionOutcomeUNKNOWN:  "BLOCKED_UNKNOWN",
}

// plainID is true for a non-empty identifier with no control character or space.
func plainID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
