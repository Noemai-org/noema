package recovery_test

// STM-05 RecoverInvestigations (REQ-STM-001, REQ-RCV-003; INV-03, INV-07, INV-12, INV-30; AGENT-07, AGENT-08, AGENT-10; ruling A204 and A377 item 15, interim, relayed, not
// yet committed). The doubles here are plain stand-ins for services built elsewhere (the store, Trust, the Controller, the verifier, the Record journal). A process that
// dies is a crash point in a double (the call returns errCrash after what it did is done), never a mocked target fault. All identifiers are SYNTHETIC.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
)

const (
	tenantA    = "ten_AA11"
	tenantB    = "ten_BB22"
	principal  = "prn_AA11"
	invID      = "inv_AA11"
	corrID     = "corr_AA11"
	recevID    = "recev_AA11"
	reqID      = "11111111-2222-3333-4444-555555555555"
	decisionID = "dec_AA11"
	grantID    = "grt_AA11"
	intentRef  = "int_AA11"
	authnRef   = "ses_0123456789abcdef0123456789abcdef" // SYNTHETIC
)

var errCrash = errors.New("synthetic crash point: the process died here")

// row is the stored investigation plus what Trust recorded for it.
type row struct {
	tenant, principal, corr, recev string
	role, authn                    string // the stored principal_role and authn_ref; both empty is a legacy row (NULL columns)
	inv                            orchestrator.Investigation
	decision                       orchestrator.Decision
}

// world is every port of the orchestrator over one in-memory store. It counts what each port was asked and keeps the session each was given.
type world struct {
	mu sync.Mutex

	rows     map[string]*row // by investigation ID
	spent    map[string]bool // grants the Record shows as spent
	recordEr error           // what AppendChange answers while set

	crashExec, crashVerify bool // crash points: the process dies after the spend, or when asked to verify
	verdicts               []contracts.EnumsV1Verdict
	verifyGate             chan struct{} // when set, Verify waits on it
	verifyEntered          chan struct{}

	nDecide, nExecute, nVerify int
	sessions                   map[string][]orchestrator.Session
	turns                      []orchestrator.Turn
	readerCalls                []readerCall
	batchIgnored               bool     // the reader returns more than the limit it was given
	sessionErr                 error    // what SessionOf answers while set
	sessionTenants             []string // the tenant of every SessionOf call
	staleRead                  bool     // the reader returns a version older than the row's: another process moved the row after the list was read
}

type readerCall struct {
	tenant string
	idle   int64
	limit  int
}

func newWorld() *world {
	w := &world{rows: map[string]*row{}, spent: map[string]bool{}, sessions: map[string][]orchestrator.Session{}, verdicts: []contracts.EnumsV1Verdict{contracts.EnumsV1VerdictACHIEVED}}
	w.rows[invID] = &row{tenant: tenantA, principal: principal, role: "customer", authn: authnRef, corr: corrID, recev: recevID, inv: orchestrator.Investigation{ID: invID, State: "RECEIVED"}}
	return w
}

func (w *world) note(port string, s orchestrator.Session) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sessions[port] = append(w.sessions[port], s)
}

func (w *world) Interpret(_ context.Context, s orchestrator.Session, t orchestrator.Turn) (orchestrator.Interpretation, error) {
	w.note("interpreter", s)
	return orchestrator.Interpretation{Kind: orchestrator.Admitted, IntentRef: intentRef}, nil
}

func (w *world) Read(_ context.Context, s orchestrator.Session, _ string) (orchestrator.EvidenceRef, error) {
	w.note("evidence", s)
	return orchestrator.EvidenceRef{SnapshotID: "evs_AA11"}, nil
}

func (w *world) Decide(_ context.Context, s orchestrator.Session, _ string, _ orchestrator.EvidenceRef) (orchestrator.Decision, error) {
	w.note("trust", s)
	d := orchestrator.Decision{ID: decisionID, Outcome: contracts.EnumsV1DecisionOutcomeALLOW, GrantIDs: []string{grantID}, IntentRef: intentRef}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nDecide++
	w.rows[invID].decision = d
	return d, nil
}

func (w *world) Explain(_ context.Context, s orchestrator.Session, _ string) (orchestrator.Explanation, error) {
	w.note("explainer", s)
	return orchestrator.Explanation{Text: "Your complaint has been escalated."}, nil
}

func (w *world) AppendChange(_ context.Context, s orchestrator.Session, c orchestrator.Change) error {
	w.note("recorder", s)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.recordEr != nil {
		return w.recordEr
	}
	r, ok := w.rows[c.ID]
	if !ok || r.tenant != s.TenantID || r.inv.Version != c.FromVersion || r.inv.State != c.From {
		return orchestrator.ErrStaleVersion
	}
	r.inv.State, r.inv.Version = c.To, c.ToVersion
	return nil
}

func (w *world) Lookup(_ context.Context, s orchestrator.Session, ref string) (orchestrator.Admission, error) {
	w.note("admissions", s)
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, r := range w.rows {
		if r.recev == ref && r.tenant == s.TenantID {
			return orchestrator.Admission{EventType: "INPUT_RECEIVED", TenantID: r.tenant, PrincipalID: r.principal, CorrelationID: r.corr, RequestID: reqID}, nil
		}
	}
	return orchestrator.Admission{}, errors.New("no such event")
}

func (w *world) Execute(_ context.Context, s orchestrator.Session, g string) (orchestrator.Receipt, error) {
	w.note("executor", s)
	w.mu.Lock()
	w.nExecute++
	w.spent[g] = true
	crash := w.crashExec
	w.mu.Unlock()
	if crash {
		return orchestrator.Receipt{}, errCrash
	}
	return orchestrator.Receipt{GrantID: g, Class: orchestrator.ReceiptApplied}, nil
}

func (w *world) Verify(_ context.Context, s orchestrator.Session, _, _ string) (contracts.EnumsV1Verdict, error) {
	w.note("verifier", s)
	w.mu.Lock()
	n := w.nVerify
	w.nVerify++
	crash, gate, entered := w.crashVerify, w.verifyGate, w.verifyEntered
	if n > 0 { // only the first ask waits: a second ask is the defect the single-flight test looks for, and must not hang the test
		gate, entered = nil, nil
	}
	vs := w.verdicts
	w.mu.Unlock()
	if crash {
		return "", errCrash
	}
	if entered != nil {
		entered <- struct{}{}
	}
	if gate != nil {
		<-gate
	}
	if n >= len(vs) {
		n = len(vs) - 1
	}
	return vs[n], nil
}

func (w *world) Progress(_ context.Context, s orchestrator.Session, _, g string) (orchestrator.Progress, error) {
	w.note("journal", s)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.spent[g] {
		return orchestrator.Progress{GrantSpent: true, DispatchAttempted: true, KeyDispatches: 1}, nil
	}
	return orchestrator.Progress{}, nil
}

func (w *world) Bound(_ context.Context, s orchestrator.Session, _ orchestrator.Binding) error {
	w.note("bindings", s)
	return nil
}

// SessionOf is the stored-session port over the store (investigations.Store.SessionOf): the tenant is the caller's session's, another tenant's row is not found, and a row
// with no role or no authn reference is ErrNoRecordedSession.
func (w *world) SessionOf(_ context.Context, s orchestrator.Session, id string) (investigations.RecordedSession, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sessionTenants = append(w.sessionTenants, s.TenantID)
	if w.sessionErr != nil {
		return investigations.RecordedSession{}, w.sessionErr
	}
	r, ok := w.rows[id]
	if !ok || r.tenant != s.TenantID {
		return investigations.RecordedSession{}, investigations.ErrNoInvestigation
	}
	if r.role == "" || r.authn == "" {
		return investigations.RecordedSession{}, investigations.ErrNoRecordedSession
	}
	return investigations.RecordedSession{PrincipalID: r.principal, PrincipalRole: r.role, AuthnRef: r.authn}, nil
}

// Idle is the reader port over the store: every investigation of the tenant that is not in a state without exits. The double ignores the idle age (the database's
// clock decides it in production) and records what it was asked.
func (w *world) Idle(_ context.Context, tenant string, idleMS int64, limit int) ([]recovery.Candidate, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.readerCalls = append(w.readerCalls, readerCall{tenant, idleMS, limit})
	tab, err := orchestrator.LoadTable()
	if err != nil {
		return nil, err
	}
	var out []recovery.Candidate
	for _, r := range w.rows {
		if len(tab.Exits(r.inv.State)) == 0 || (r.tenant != tenant && !w.batchIgnored) {
			continue
		}
		inv := r.inv
		if w.staleRead {
			inv.Version--
		}
		out = append(out, recovery.Candidate{Investigation: inv, TenantID: r.tenant, CorrelationID: r.corr, InputReceivedRef: r.recev, PrincipalRef: r.principal, Decision: r.decision})
	}
	return out, nil
}

// runner builds a write runner whose executor is x (the one the recovery process is given).
func (w *world) runner(t *testing.T, x orchestrator.Executor) *orchestrator.WriteRunner {
	t.Helper()
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := orchestrator.NewWriteRunner(&orchestrator.Machine{Table: tab},
		orchestrator.Ports{Interpreter: w, Evidence: w, Trust: w, Explainer: w, Recorder: w, Admissions: w},
		orchestrator.WritePorts{Executor: x, Verifier: w, Journal: w, Bindings: w}, orchestrator.WriteConfig{MaxDispatchesPerKey: orchestrator.PERDMaxDispatchesPerKey})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// firstProcess runs the turn as the process that later dies.
func (w *world) firstProcess(t *testing.T) error {
	t.Helper()
	_, err := w.runner(t, w).RunWrite(tctx(), orchestrator.Session{TenantID: tenantA, PrincipalID: principal, Role: "customer"},
		orchestrator.Turn{InvestigationID: invID, CorrelationID: corrID, Text: "escalate my complaint", InputReceivedRef: recevID, RequestID: reqID})
	return err
}

func (w *world) state() orchestrator.State {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rows[invID].inv.State
}

func (w *world) counts() (decide, execute, verify int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.nDecide, w.nExecute, w.nVerify
}

// activity builds the recovery activity over a recovery runner whose executor is x.
func (w *world) activity(t *testing.T, x orchestrator.Executor) *recovery.Activity {
	t.Helper()
	a, err := recovery.New(recovery.Config{IdleMS: 60000, Batch: 10, RunTimeoutMS: 30000}, w, w, w, w, w.runner(t, x))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// tctx is a context with a deadline: the activity refuses one without (the stores and ports of this repository all do).
func tctx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	_ = cancel // released by the deadline; a test context lives for one test
	return ctx
}
