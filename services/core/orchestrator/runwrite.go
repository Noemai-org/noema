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
	w   WritePorts
	cfg WriteConfig
}

// NewWriteRunner refuses a Runner that does not build and any missing write port.
func NewWriteRunner(m *Machine, p Ports, w WritePorts, cfg WriteConfig) (*WriteRunner, error) {
	r, err := NewRunner(m, p)
	if err != nil {
		return nil, err
	}
	if w.Executor == nil || w.Verifier == nil || w.Journal == nil || w.Bindings == nil {
		return nil, fmt.Errorf("%w: a write port is missing", ErrNotWired)
	}
	if cfg.MaxDispatchesPerKey < 1 {
		return nil, fmt.Errorf("%w: the dispatch limit per key must be at least 1", ErrNotWired)
	}
	return &WriteRunner{Runner: r, w: w, cfg: cfg}, nil
}

// RunWrite runs a turn whose decision may write. It takes the same path as RunRead to EVALUATING; an ALLOW that writes then walks PLANNED, EXECUTING and
// AWAITING_VERIFICATION. A verdict of ACHIEVED explains and completes; PENDING_EXTERNAL waits for the outcome; UNKNOWN and COMPENSATED are recorded as RECOVERING and the run stops
// with ErrOutcomeUnknown or ErrCompensated (a new grant could apply the same write twice, INV-07, A224); any other verdict goes to RECOVERING and back to EVALUATING, where Trust decides
// again on a fresh evidence snapshot and issues a new grant (INV-17, AGENT-08). Before Trust is asked again the runner counts the dispatches of the logical key from the Record (the Journal) and stops with
// ErrRecoveryBudgetSpent at the configured limit, PERD's 2 per key (A216); a limit it was not given is no limit at all, so there is none by default. A grant is never dispatched
// twice. Every move is recorded before the run goes on (PERD 32.4, EC-E16).
func (w *WriteRunner) RunWrite(ctx context.Context, s Session, t Turn) (Result, error) {
	if err := w.admit(ctx, s, t); err != nil {
		return Result{}, err
	}
	u := &wrun{run: run{ctx: ctx, r: w.Runner, s: s, inv: Investigation{ID: t.InvestigationID, State: "RECEIVED"}}, w: w}
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

// admit checks the session, the turn and the recorded INPUT_RECEIVED it names, as RunRead does before it records or runs anything (A161). The checks repeat
// RunRead's; folding them into one function is a refactor for its own PR (debt).
func (w *WriteRunner) admit(ctx context.Context, s Session, t Turn) error {
	if s.TenantID == "" || s.PrincipalID == "" {
		return ErrNoSession
	}
	if t.Text == "" || !plainID(t.InvestigationID) || !plainID(t.CorrelationID) || !requestID.MatchString(t.RequestID) {
		return ErrBadTurn
	}
	if !recordEventID.MatchString(t.InputReceivedRef) {
		return ErrNoInputReceived
	}
	a, err := w.p.Admissions.Lookup(ctx, s, t.InputReceivedRef)
	if err != nil {
		return fmt.Errorf("%w: lookup: %w", ErrNoInputReceived, err)
	}
	if a.EventType != inputReceived || a.TenantID != s.TenantID || a.PrincipalID != s.PrincipalID || a.CorrelationID != t.CorrelationID || a.RequestID != t.RequestID {
		return ErrNoInputReceived
	}
	return nil
}

// ResumeWrite continues an investigation from its stored projection after a crash (A204). inv is what the store holds (state and version), d is the recorded
// decision. The recovery count is derived from the Record, so a resume cannot reset it. From PLANNED it asks the Journal: a grant that is not spent is executed, and a grant the
// Record shows as spent is never spent or dispatched again; the run verifies it by key. From EXECUTING and AWAITING_VERIFICATION it verifies by key. Any other
// state is ErrNotResumable. A Journal that cannot be read stops the run before anything is dispatched.
func (w *WriteRunner) ResumeWrite(ctx context.Context, s Session, t Turn, inv Investigation, d Decision) (Result, error) {
	// The caller's inputs are not trusted: the turn must be this tenant's recorded input, the stored projection this turn's investigation, and the decision must name
	// one plain grant and the intent it was made on. Bindings then proves the decision, grant and intent against what Trust recorded.
	if err := w.admit(ctx, s, t); err != nil {
		return Result{}, err
	}
	if inv.ID != t.InvestigationID {
		return Result{}, ErrBadTurn
	}
	if len(d.GrantIDs) != 1 || !plainID(d.GrantIDs[0]) {
		return Result{}, ErrNoGrant
	}
	if d.IntentRef == "" {
		return Result{}, ErrBadTurn
	}
	// The decision and the grant are the caller's: before anything is read, moved or dispatched, the Trust side proves they are this turn's (Codex P1 on noema#576).
	if err := w.w.Bindings.Bound(ctx, s, Binding{CorrelationID: t.CorrelationID, DecisionID: d.ID, GrantID: d.GrantIDs[0], IntentRef: d.IntentRef}); err != nil {
		return Result{Investigation: inv}, fmt.Errorf("orchestrator: bindings: %w", err)
	}
	u := &wrun{run: run{ctx: ctx, r: w.Runner, s: s, inv: inv}, w: w}
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
	w         *WriteRunner
	lastGrant string // the grant of the round that just ended; the recovery count is derived from the Record by it
	voids     int    // grants voided before they were spent: they leave no GRANT_SPENT on the Record, so this run counts them
}

// evaluate is EVALUATING: read evidence, ask Trust, take the arrow. Each return here is a fresh evaluation on a fresh snapshot.
func (u *wrun) evaluate(intentRef string) (Result, error) {
	exhausted, err := u.budgetSpent()
	if err != nil {
		return u.result(""), err
	}
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
	if exhausted {
		// Trust allowed another write although the key's dispatches have reached the limit: PERD REQ-RCV-003 makes ESCALATE the result there. The runner does not
		// execute it and does not decide in Trust's place; the grant that was issued is never spent (a defect of the decision, reported by this error).
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
	u.lastGrant = d.GrantIDs[0]
	rc, err := u.w.w.Executor.Execute(u.ctx, u.s, d.GrantIDs[0])
	if err != nil {
		return u.result(""), fmt.Errorf("orchestrator: execute: %w", err)
	}
	if rc.GrantID != d.GrantIDs[0] {
		return u.result(""), fmt.Errorf("%w: a receipt for grant %q, not %q", ErrBadPortAnswer, rc.GrantID, d.GrantIDs[0])
	}
	switch rc.Class {
	case ReceiptVoided:
		// The grant was voided before it was spent: it was never dispatched. A new decision follows (PLANNED to EVALUATING).
		if err := u.move("EVALUATING"); err != nil {
			return u.result(""), err
		}
		u.voids++
		return u.evaluate(d.IntentRef)
	case ReceiptBudgetSpent:
		// The database refused the dispatch because the key's budget is used up (A153, A216): nothing was dispatched, so there is nothing to verify and no reason to ask Trust
		// again. The run stops for a person; the grant is never spent and expires by TTL. No arrow to a hand-over exists from PLANNED, so none is invented (A216).
		return u.result(""), ErrRecoveryBudgetSpent
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
	u.lastGrant = d.GrantIDs[0]
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
	case contracts.EnumsV1VerdictUNKNOWN, contracts.EnumsV1VerdictCOMPENSATED:
		// UNKNOWN: the check by key could not tell whether the write happened. COMPENSATED (A224, relayed): the write happened and was then undone. In both a new grant
		// could apply the write a second time, so the run is recorded as RECOVERING and stops here.
		if err := u.move("RECOVERING"); err != nil {
			return u.result(""), err
		}
		if v == contracts.EnumsV1VerdictCOMPENSATED {
			return u.result(""), ErrCompensated
		}
		return u.result(""), ErrOutcomeUnknown
	case contracts.EnumsV1VerdictNOTACHIEVED, contracts.EnumsV1VerdictPARTIALLYACHIEVED:
		if err := u.move("RECOVERING"); err != nil {
			return u.result(""), err
		}
		if err := u.move("EVALUATING"); err != nil {
			return u.result(""), err
		}
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

// budgetSpent is the recovery budget of PERD REQ-RCV-003 and ruling A216: at most cfg.MaxDispatchesPerKey dispatches per logical key. The count is derived from the
// Record (GRANT_SPENT events of the last grant's idempotency key, through the Journal), so a crash and a resume cannot reset it. When it is spent the run still asks
// Trust: REQ-RCV-003 makes ESCALATE the result there, taken by the existing arrows (EVALUATING to ESCALATING by that decision, then ESCALATED by the handoff), and
// the runner never decides it. It only refuses to execute any other allowed write.
func (u *wrun) budgetSpent() (bool, error) {
	if u.voids >= u.w.cfg.MaxDispatchesPerKey {
		return true, nil
	}
	if u.lastGrant == "" {
		return false, nil
	}
	p, err := u.w.w.Journal.Progress(u.ctx, u.s, u.inv.ID, u.lastGrant)
	if err != nil {
		return false, fmt.Errorf("orchestrator: journal: %w", err)
	}
	return p.KeyDispatches >= u.w.cfg.MaxDispatchesPerKey, nil
}
