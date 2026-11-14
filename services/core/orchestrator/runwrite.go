package orchestrator

import (
	"context"
	"fmt"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// WriteRunner is the Runner plus the write path of US-01 and US-06 (STM-01, A204). The state of an investigation is a projection and the Record is the truth: the
// runner records EXECUTING after Execute returns, asks the Journal what the Record holds for a grant (GRANT_SPENT, DISPATCH_ATTEMPTED), and ResumeWrite continues
// from the stored projection, never from zero. It decides, executes and verifies nothing itself: Trust, the Executor and the Verifier do.
type WriteRunner struct {
	*Runner
	w WritePorts
}

// NewWriteRunner refuses a Runner that does not build and any missing write port.
func NewWriteRunner(m *Machine, p Ports, w WritePorts) (*WriteRunner, error) {
	r, err := NewRunner(m, p)
	if err != nil {
		return nil, err
	}
	if w.Executor == nil || w.Verifier == nil || w.Journal == nil {
		return nil, fmt.Errorf("%w: a write port is missing", ErrNotWired)
	}
	return &WriteRunner{Runner: r, w: w}, nil
}

// RunWrite runs a turn whose decision may write. It takes the same path as RunRead to EVALUATING; an ALLOW that writes then walks PLANNED, EXECUTING and
// AWAITING_VERIFICATION. A verdict of ACHIEVED explains and completes; PENDING_EXTERNAL waits for the outcome; any other verdict goes to RECOVERING and back to
// EVALUATING, where Trust decides again on a fresh evidence snapshot and issues a new grant (INV-17, AGENT-08). maxRecoveries is the number of such returns the
// run may take; it has no default (AGENT-16), and when it is spent a further allowed write is refused with ErrRecoveryBudgetSpent. A grant is never dispatched
// twice. Every move is recorded before the run goes on (PERD 32.4, EC-E16).
func (w *WriteRunner) RunWrite(ctx context.Context, s Session, t Turn, maxRecoveries int) (Result, error) {
	if maxRecoveries < 0 {
		return Result{}, ErrBadTurn
	}
	// The checks below repeat RunRead's; folding them into one function is a refactor for its own PR (debt).
	if s.TenantID == "" || s.PrincipalID == "" {
		return Result{}, ErrNoSession
	}
	if t.Text == "" || !plainID(t.InvestigationID) || !plainID(t.CorrelationID) || !requestID.MatchString(t.RequestID) {
		return Result{}, ErrBadTurn
	}
	if !recordEventID.MatchString(t.InputReceivedRef) {
		return Result{}, ErrNoInputReceived
	}
	a, err := w.p.Admissions.Lookup(ctx, s, t.InputReceivedRef)
	if err != nil {
		return Result{}, fmt.Errorf("%w: lookup: %w", ErrNoInputReceived, err)
	}
	if a.EventType != inputReceived || a.TenantID != s.TenantID || a.PrincipalID != s.PrincipalID || a.CorrelationID != t.CorrelationID || a.RequestID != t.RequestID {
		return Result{}, ErrNoInputReceived
	}
	u := &wrun{run: run{ctx: ctx, r: w.Runner, s: s, inv: Investigation{ID: t.InvestigationID, State: "RECEIVED"}}, w: w, budget: maxRecoveries}
	if err := u.move("INTERPRETING"); err != nil {
		return u.result(""), err
	}
	in, err := w.p.Interpreter.Interpret(ctx, s, t)
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
	return u.evaluate(in.IntentRef)
}

// ResumeWrite continues an investigation from its stored projection after a crash (A204). inv is what the store holds (state and version), d is the recorded
// decision, and maxRecoveries is the number of recoveries still allowed. From PLANNED it asks the Journal: a grant that is not spent is executed, and a grant the
// Record shows as spent is never spent or dispatched again; the run verifies it by key. From EXECUTING and AWAITING_VERIFICATION it verifies by key. Any other
// state is ErrNotResumable. A Journal that cannot be read stops the run before anything is dispatched.
func (w *WriteRunner) ResumeWrite(ctx context.Context, s Session, t Turn, inv Investigation, d Decision, maxRecoveries int) (Result, error) {
	if maxRecoveries < 0 || !plainID(t.InvestigationID) || inv.ID != t.InvestigationID {
		return Result{}, ErrBadTurn
	}
	if s.TenantID == "" || s.PrincipalID == "" {
		return Result{}, ErrNoSession
	}
	if len(d.GrantIDs) != 1 {
		return Result{}, ErrNoGrant
	}
	u := &wrun{run: run{ctx: ctx, r: w.Runner, s: s, inv: inv}, w: w, budget: maxRecoveries}
	switch inv.State {
	case "PLANNED":
		p, err := w.w.Journal.Progress(ctx, s, inv.ID, d.GrantIDs[0])
		if err != nil {
			return u.result(""), fmt.Errorf("orchestrator: journal: %w", err)
		}
		if !p.GrantSpent && !p.DispatchAttempted {
			return u.execute(d)
		}
		if err := u.move("EXECUTING"); err != nil {
			return u.result(""), err
		}
		return u.awaitVerification(d)
	case "EXECUTING":
		return u.awaitVerification(d)
	case "AWAITING_VERIFICATION":
		return u.verify(d)
	}
	return u.result(""), fmt.Errorf("%w: %s", ErrNotResumable, inv.State)
}

// wrun is a run with the write path's budget.
type wrun struct {
	run
	w          *WriteRunner
	budget     int
	recoveries int
}

// evaluate is EVALUATING: read evidence, ask Trust, take the arrow. Each return here is a fresh evaluation on a fresh snapshot.
func (u *wrun) evaluate(intentRef string) (Result, error) {
	ev, err := u.r.p.Evidence.Read(u.ctx, u.s, intentRef)
	if err != nil {
		return u.result(""), fmt.Errorf("orchestrator: evidence: %w", err)
	}
	d, err := u.r.p.Trust.Decide(u.ctx, u.s, intentRef, ev)
	if err != nil {
		return u.result(""), fmt.Errorf("orchestrator: decide: %w", err)
	}
	if d.IntentRef == "" {
		d.IntentRef = intentRef
	}
	if to, ok := decisionArrow[d.Outcome]; ok {
		return u.finish(to)
	}
	if d.Outcome != contracts.EnumsV1DecisionOutcomeALLOW {
		return u.result(""), fmt.Errorf("%w: decision outcome %q", ErrBadPortAnswer, d.Outcome)
	}
	if d.Read {
		if err := u.move("EXPLAINING"); err != nil {
			return u.result(""), err
		}
		return u.explain(d)
	}
	if len(d.GrantIDs) != 1 {
		return u.result(""), ErrNoGrant
	}
	if u.recoveries > u.budget {
		return u.result(""), ErrRecoveryBudgetSpent
	}
	if err := u.move("PLANNED"); err != nil {
		return u.result(""), err
	}
	return u.execute(d)
}

// execute spends and dispatches the decision's one grant. EXECUTING is recorded after Execute returns, so a crash before it leaves PLANNED and the Journal says
// what happened. An error from the Executor stops the run: the write may have been dispatched, and only the Record can say.
func (u *wrun) execute(d Decision) (Result, error) {
	rc, err := u.w.w.Executor.Execute(u.ctx, u.s, d.GrantIDs[0])
	if err != nil {
		return u.result(""), fmt.Errorf("orchestrator: execute: %w", err)
	}
	switch rc.Class {
	case ReceiptVoided:
		// The grant was voided before it was spent: it was never dispatched. A new decision follows (PLANNED to EVALUATING).
		if err := u.move("EVALUATING"); err != nil {
			return u.result(""), err
		}
		u.recoveries++
		return u.evaluate(d.IntentRef)
	case ReceiptApplied, ReceiptUnknown:
	default:
		return u.result(""), fmt.Errorf("%w: receipt class %q", ErrBadPortAnswer, rc.Class)
	}
	if err := u.move("EXECUTING"); err != nil {
		return u.result(""), err
	}
	return u.awaitVerification(d)
}

// awaitVerification takes EXECUTING to AWAITING_VERIFICATION (any receipt, a lost reply included) and verifies.
func (u *wrun) awaitVerification(d Decision) (Result, error) {
	if err := u.move("AWAITING_VERIFICATION"); err != nil {
		return u.result(""), err
	}
	return u.verify(d)
}

// verify asks the Verifier for the verdict by the grant's key. It never executes.
func (u *wrun) verify(d Decision) (Result, error) {
	v, err := u.w.w.Verifier.Verify(u.ctx, u.s, d.ID, d.GrantIDs[0])
	if err != nil {
		return u.result(""), fmt.Errorf("orchestrator: verify: %w", err)
	}
	switch v {
	case contracts.EnumsV1VerdictACHIEVED:
		if err := u.move("EXPLAINING"); err != nil {
			return u.result(""), err
		}
		return u.explain(d)
	case contracts.EnumsV1VerdictPENDINGEXTERNAL:
		return u.finish("AWAITING_EXTERNAL_OUTCOME")
	case contracts.EnumsV1VerdictNOTACHIEVED, contracts.EnumsV1VerdictPARTIALLYACHIEVED, contracts.EnumsV1VerdictUNKNOWN, contracts.EnumsV1VerdictCOMPENSATED:
		if err := u.move("RECOVERING"); err != nil {
			return u.result(""), err
		}
		if err := u.move("EVALUATING"); err != nil {
			return u.result(""), err
		}
		u.recoveries++
		return u.evaluate(d.IntentRef)
	}
	return u.result(""), fmt.Errorf("%w: verdict %q", ErrBadPortAnswer, v)
}

// explain is EXPLAINING: ask for the answer and complete, or end in EXPLAINING_BLOCKED when the validator blocks it.
func (u *wrun) explain(d Decision) (Result, error) {
	ex, err := u.r.p.Explainer.Explain(u.ctx, u.s, d.ID)
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
