// Package recovery is the RecoverInvestigations activity (STM-05; REQ-STM-001, REQ-RCV-003; INV-03, INV-07, INV-12, INV-30; A204, A377 item 15, interim, relayed).
// An investigation that stopped part-way, because the process died, is found by the age of its last move and resumed from the Record: a grant the Record shows as spent
// or attempted is verified by its key and never dispatched again; an unknown or compensated outcome, and anything the write path cannot resume, stops for a person.
//
// It does no I/O of its own: rows come through the Reader port (Postgres, in the tenant's own login) and everything else through the orchestrator's ports, so a workflow
// that calls it as an activity does no I/O in workflow code (INV-30).
//
// May import: the standard library, orchestrator, orchestrator/investigations (its stored-session types only) and edge/session (the role mapping). Must never: touch Temporal or the database, dispatch a grant, take a value from a request, or decide an outcome
// (the Verifier does).
package recovery

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
)

// Outcome is what one pass did with one investigation.
type Outcome string

const (
	// Resumed: ResumeWrite ran to the end of its path (completed, awaiting an external outcome, or ended in a state the machine allows).
	Resumed Outcome = "RESUMED"
	// NeedsPerson: nothing more may be done by a machine (unknown or compensated outcome, a grant not spent, a state the write path does not resume, a spent budget).
	NeedsPerson Outcome = "NEEDS_PERSON"
	// Skipped: another pass, in this process or another, has the investigation or moved it first.
	Skipped Outcome = "SKIPPED"
	// Failed: a port could not answer; the next pass tries again from the Record.
	Failed Outcome = "FAILED"
	// Refused: the row is not this tenant's or is malformed; no port was called for it.
	Refused Outcome = "REFUSED"
	// NoRecordedSession: the row holds no principal role and authn reference (a row from before migration 0036). It is never resumed and never given a role (A490).
	NoRecordedSession Outcome = "NO_RECORDED_SESSION"
)

// Item is the result for one investigation; Report is one pass for one tenant.
type Item struct {
	InvestigationID string
	Outcome         Outcome
}

// Report lists what one pass did, in the order it did it. NoRecordedSession and Refused count the rows that were not resumed for those reasons, so they stay visible (A490).
type Report struct {
	Items                      []Item
	NoRecordedSession, Refused int
}

// TotalNoRecordedSession is the number of rows without a recorded session over a startup pass's reports.
func TotalNoRecordedSession(reports map[string]Report) int {
	n := 0
	for _, r := range reports {
		n += r.NoRecordedSession
	}
	return n
}

// Candidate is what the Reader returns for one idle, non-terminal investigation: the stored row and the decision Trust recorded for it. None of it comes from a request.
type Candidate struct {
	Investigation                                           orchestrator.Investigation
	TenantID, CorrelationID, InputReceivedRef, PrincipalRef string
	Decision                                                orchestrator.Decision
}

// Reader lists a tenant's non-terminal investigations whose last move is older than idleMS, at most limit of them. The database's clock decides idleness (the
// activity reads no clock), and the tenant is the login's, so a row of another tenant is not returned; the activity checks that again.
type Reader interface {
	Idle(ctx context.Context, tenantID string, idleMS int64, limit int) ([]Candidate, error)
}

// SessionReader is the store's SessionOf (investigations.Store): the principal, role and authn reference the row holds, for the caller's tenant.
type SessionReader interface {
	SessionOf(ctx context.Context, sess orchestrator.Session, investigationID string) (investigations.RecordedSession, error)
}

// Resumer is the write runner's ResumeWrite.
type Resumer interface {
	ResumeWrite(ctx context.Context, s orchestrator.Session, t orchestrator.Turn, inv orchestrator.Investigation, d orchestrator.Decision) (orchestrator.Result, error)
}

// Activity is RecoverInvestigations. Build it with New.
type Activity struct {
	cfg      Config
	reader   Reader
	sessions SessionReader
	journal  orchestrator.Journal
	admit    orchestrator.Admissions
	resumer  Resumer
	mu       sync.Mutex
	inflight map[string]struct{} // tenant and correlation of every investigation a pass is working on
}

// New refuses a missing port or a Config with a zero field. The Resumer's executor should be NoDispatch: recovery never dispatches.
func New(cfg Config, r Reader, ss SessionReader, j orchestrator.Journal, a orchestrator.Admissions, x Resumer) (*Activity, error) {
	if r == nil || ss == nil || j == nil || a == nil || x == nil {
		return nil, errors.New("recovery: the activity is not fully wired")
	}
	if cfg.IdleMS < 1 || cfg.Batch < 1 || cfg.check() != nil {
		return nil, fmt.Errorf("recovery: the idle age, the batch and the run timeout must be set, and %s must exceed %s", EnvIdleMS, EnvRunTimeoutMS)
	}
	return &Activity{cfg: cfg, reader: r, sessions: ss, journal: j, admit: a, resumer: x, inflight: map[string]struct{}{}}, nil
}

var tenantRe = regexp.MustCompile(`\Aten_[0-9A-Za-z]{2,32}\z`)

// Recover resumes the idle investigations of one tenant, at most Config.Batch of them. The tenant is the activity's argument (the worker's, from the schedule), never a
// request's. An error means the list could not be read; what happened to each investigation is in the Report.
func (a *Activity) Recover(ctx context.Context, tenantID string) (Report, error) {
	if _, ok := ctx.Deadline(); !ok {
		return Report{}, errors.New("recovery: the context needs a deadline")
	}
	if !tenantRe.MatchString(tenantID) {
		return Report{}, errors.New("recovery: the tenant is outside its pattern")
	}
	cands, err := a.reader.Idle(ctx, tenantID, a.cfg.IdleMS, a.cfg.Batch)
	if err != nil {
		return Report{}, fmt.Errorf("recovery: list: %w", err)
	}
	if len(cands) > a.cfg.Batch {
		cands = cands[:a.cfg.Batch]
	}
	var rep Report
	for _, c := range cands {
		o := a.one(ctx, tenantID, c)
		rep.Items = append(rep.Items, Item{InvestigationID: c.Investigation.ID, Outcome: o})
		switch o {
		case NoRecordedSession:
			rep.NoRecordedSession++
		case Refused:
			rep.Refused++
		}
	}
	return rep, nil
}

// StartupPass runs Recover for each tenant, once, when the worker starts (G1). A tenant whose list cannot be read ends the pass with its error: the others it did were
// recorded in the map.
func (a *Activity) StartupPass(ctx context.Context, tenantIDs []string) (map[string]Report, error) {
	out := make(map[string]Report, len(tenantIDs))
	for _, t := range tenantIDs {
		rep, err := a.Recover(ctx, t)
		if err != nil {
			return out, fmt.Errorf("tenant %s: %w", t, err)
		}
		out[t] = rep
	}
	return out, nil
}

func (a *Activity) claim(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.inflight[key]; ok {
		return false
	}
	a.inflight[key] = struct{}{}
	return true
}

func (a *Activity) release(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.inflight, key)
}

// one resumes one candidate. Every value it hands a port is the stored row's, a constant, or a Record answer.
func (a *Activity) one(ctx context.Context, tenantID string, c Candidate) Outcome {
	if c.TenantID != tenantID {
		return Refused
	}
	// The stored session is read for the pass's tenant; the caller's role never selects the row. A row with none is never resumed and never given a role (A490).
	rs, err := a.sessions.SessionOf(ctx, orchestrator.Session{TenantID: c.TenantID, PrincipalID: c.PrincipalRef}, c.Investigation.ID)
	switch {
	case errors.Is(err, investigations.ErrNoRecordedSession):
		return NoRecordedSession
	case errors.Is(err, investigations.ErrNoInvestigation):
		return Refused
	case err != nil:
		return Failed
	}
	s, err := NewRecoverySession(c, rs)
	if err != nil {
		return Refused
	}
	if !a.claim(c.TenantID + "/" + c.CorrelationID) {
		return Skipped
	}
	defer a.release(c.TenantID + "/" + c.CorrelationID)
	// The request ID is the Record's: the INPUT_RECEIVED event the row names says which request this investigation began with.
	adm, err := a.admit.Lookup(ctx, s, c.InputReceivedRef)
	if err != nil {
		return Failed
	}
	t := orchestrator.Turn{InvestigationID: c.Investigation.ID, CorrelationID: c.CorrelationID, Text: recoveryText, InputReceivedRef: c.InputReceivedRef, RequestID: adm.RequestID}
	if c.Investigation.State == "PLANNED" && len(c.Decision.GrantIDs) == 1 {
		// Recovery never spends a grant (INV-03, AGENT-08): at PLANNED, ResumeWrite would execute one the Record does not show as spent, so it is not called for it.
		p, err := a.journal.Progress(ctx, s, c.Investigation.ID, c.Decision.GrantIDs[0])
		if err != nil {
			return Failed
		}
		if !p.GrantSpent && !p.DispatchAttempted {
			return NeedsPerson
		}
	}
	_, err = a.resumer.ResumeWrite(ctx, s, t, c.Investigation, c.Decision)
	switch {
	case err == nil:
		return Resumed
	case errors.Is(err, orchestrator.ErrStaleVersion):
		return Skipped
	case errors.Is(err, orchestrator.ErrOutcomeUnknown), errors.Is(err, orchestrator.ErrCompensated), errors.Is(err, orchestrator.ErrNotResumable),
		errors.Is(err, orchestrator.ErrRecoveryBudgetSpent), errors.Is(err, orchestrator.ErrNotBound), errors.Is(err, orchestrator.ErrNoGrant), errors.Is(err, ErrNoDispatch):
		return NeedsPerson
	}
	return Failed
}
