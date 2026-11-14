package outcomee2e_test

// STM-03, A204, A210: the real-stack RunWrite tests. US-01 (write): an allowed write is executed through the real Execution Controller against the real mock-case,
// verified by Outcome against the target, and only then explained. US-06 (REQ-VER-003, INV-07, AGENT-07): the target commits the create and the reply is lost (a
// FaultPlan: TIMEOUT_AFTER_COMMIT); the receipt says OUTCOME_UNKNOWN; the run verifies by key; the target received exactly one create. A resume after a crash between
// the spend and the move to EXECUTING reads the Record and never dispatches again (A204). No failure mode is mocked (AGENT-12): the fault is the target's own, loaded
// through the test-only bench client. The ports of the services that are not built (Interpreter, Evidence, Trust, Explainer, Admissions) are test doubles; Trust's
// double issues a real signed grant for a recorded decision. All data is SYNTHETIC.
//
// These tests live here and not in execution/e2e (A210) because the architecture lint refuses a package under execution/ that reaches outcome/, test files included
// (rule import-boundary), and A210 says no lint rule changes: this package already holds Execution, Outcome and the bench client, under adapters/. Raised on noema#576.

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	executorport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/executor"
	journalport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/journal"
	verifierport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/verifier"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

const rwDecisionID = "dec_us06" // the decision the trust stand-in records for the grants it issues

var (
	rwSession = orchestrator.Session{TenantID: tenant, PrincipalID: principal}
	rwTurn    = orchestrator.Turn{InvestigationID: "inv_rw0001", CorrelationID: correlation, Text: "Open a dispute case for my transfer", InputReceivedRef: "recev_RW0001",
		RequestID: "3f2b8c1e-5d4a-4b7e-9a10-2c6d8e0f1a23"}
)

// rwDoubles are the ports of the services that are not built, and a recorder that keeps the changes.
type rwDoubles struct {
	s       *stack
	mu      sync.Mutex
	changes []orchestrator.Change
	grants  []string
}

func (d *rwDoubles) Lookup(context.Context, orchestrator.Session, string) (orchestrator.Admission, error) {
	return orchestrator.Admission{EventType: "INPUT_RECEIVED", TenantID: tenant, PrincipalID: principal, CorrelationID: correlation, RequestID: rwTurn.RequestID}, nil
}
func (d *rwDoubles) Interpret(context.Context, orchestrator.Session, orchestrator.Turn) (orchestrator.Interpretation, error) {
	return orchestrator.Interpretation{Kind: orchestrator.Admitted, IntentRef: intentRef}, nil
}
func (d *rwDoubles) Read(context.Context, orchestrator.Session, string) (orchestrator.EvidenceRef, error) {
	return orchestrator.EvidenceRef{SnapshotID: "evs_RW0001"}, nil
}

// Decide plays Trust: it issues one real, signed, recorded grant and allows the write that grant is for.
func (d *rwDoubles) Decide(ctx context.Context, _ orchestrator.Session, intent string, _ orchestrator.EvidenceRef) (orchestrator.Decision, error) {
	g, err := d.s.trust.issueCreate(ctx)
	if err != nil {
		return orchestrator.Decision{}, err
	}
	d.mu.Lock()
	d.grants = append(d.grants, g)
	d.mu.Unlock()
	return orchestrator.Decision{ID: rwDecisionID, Outcome: contracts.EnumsV1DecisionOutcomeALLOW, GrantIDs: []string{g}, IntentRef: intent}, nil
}
func (d *rwDoubles) Explain(context.Context, orchestrator.Session, string) (orchestrator.Explanation, error) {
	return orchestrator.Explanation{Text: "Your dispute case has been opened."}, nil
}
func (d *rwDoubles) AppendChange(_ context.Context, _ orchestrator.Session, c orchestrator.Change) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.changes = append(d.changes, c)
	return nil
}

func (d *rwDoubles) states() []orchestrator.State {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []orchestrator.State
	for _, c := range d.changes {
		out = append(out, c.To)
	}
	return out
}

// rwRequests builds the verifier's request for a grant: the receipt the Execution Controller wrote for it and the pack's definition of the outcome. It jumps the clock
// past the settle window, as the outcome.verify delivery would after it. It is the test's stand-in for the request source the composition will own.
type rwRequests struct{ s *stack }

func (r rwRequests) For(ctx context.Context, tenantID, grantID string) (verifier.Request, error) {
	var receiptID string
	if err := r.s.admin.QueryRow(ctx, `SELECT receipt_id FROM execution_receipt WHERE tenant_id = $1 AND grant_id = $2 ORDER BY attempt DESC LIMIT 1`, tenantID, grantID).Scan(&receiptID); err != nil {
		return verifier.Request{}, err
	}
	r.s.clk.jump(time.Duration(r.s.def.SettleWindowSecs+1) * time.Second)
	req := r.s.request
	req.ReceiptID = receiptID
	req.VerificationID = "vrf_" + receiptID
	return req, nil
}

// rwRig is the real stack behind a WriteRunner.
type rwRig struct {
	s   *stack
	d   *rwDoubles
	run *orchestrator.WriteRunner
}

func newRWRig(t *testing.T) *rwRig {
	t.Helper()
	s := newStack(t, startMockCase(t))
	d := &rwDoubles{s: s}
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	run, err := orchestrator.NewWriteRunner(&orchestrator.Machine{Table: tab},
		orchestrator.Ports{Interpreter: d, Evidence: d, Trust: d, Explainer: d, Recorder: d, Admissions: d},
		orchestrator.WritePorts{
			Executor: executorport.Port{Controller: s.ctl, Timeout: time.Minute},
			Verifier: verifierport.Port{Verifier: s.v, Requests: rwRequests{s: s}},
			Journal:  journalport.Port{DB: testpg.TenantLogin(t, s.admin, s.dsn, "record_read", tenant)},
		})
	if err != nil {
		t.Fatal(err)
	}
	return &rwRig{s: s, d: d, run: run}
}

func (r *rwRig) recordCount(t *testing.T, eventType, grantID string) int {
	t.Helper()
	return r.s.count(t, `SELECT count(*) FROM record_event WHERE tenant_id = $1 AND event_type = $2 AND object_ref = $3`, tenant, eventType, grantID)
}

// US-01 write, REQ-EXE-001, REQ-VER-001, AGENT-21: the write is dispatched once, verified against the target, and only then explained.
func TestRunWriteExecutesVerifiesAndExplainsAgainstTheRealStack(t *testing.T) {
	r := newRWRig(t)
	res, err := r.run.RunWrite(bg(t), rwSession, rwTurn, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []orchestrator.State{"INTERPRETING", "EVALUATING", "PLANNED", "EXECUTING", "AWAITING_VERIFICATION", "EXPLAINING", "COMPLETED"}
	if !reflect.DeepEqual(r.d.states(), want) || res.Investigation.State != "COMPLETED" || res.Answer == "" {
		t.Fatalf("states %v, final %+v, answer %q; want %v and COMPLETED with an answer", r.d.states(), res.Investigation, res.Answer, want)
	}
	if received, committed := creates(r.s.bench.commands(t), theKey()); received != 1 || committed != 1 {
		t.Fatalf("the target received %d creates and committed %d; want 1 and 1", received, committed)
	}
	g := r.d.grants[0]
	if spent, attempted := r.recordCount(t, "GRANT_SPENT", g), r.recordCount(t, "DISPATCH_ATTEMPTED", g); spent != 1 || attempted != 1 {
		t.Errorf("the Record holds %d GRANT_SPENT and %d DISPATCH_ATTEMPTED for %s; want 1 and 1", spent, attempted, g)
	}
}

// US-06, REQ-VER-003, INV-07, AGENT-07, AGENT-08: the target commits and the reply is lost. The client gives up (the plan step's timeout is 1 s, the hold 3.5 s), the
// receipt is OUTCOME_UNKNOWN, the run verifies by key and finds the case: ACHIEVED. The target received exactly one create: nothing was dispatched twice.
func TestRunWriteAfterALostReplyVerifiesByKeyAndNeverDispatchesTwice(t *testing.T) {
	r := newRWRig(t)
	r.s.bench.loadPlan(t, planWith(t, "fp_runwrite_commit_then_drop_v1", fault("create_case", "TIMEOUT_AFTER_COMMIT", map[string]any{"delay_ms": 3500})))
	res, err := r.run.RunWrite(bg(t), rwSession, rwTurn, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Investigation.State != "COMPLETED" {
		t.Fatalf("final %s (states %v); want COMPLETED: the case was committed, so verification by key finds it", res.Investigation.State, r.d.states())
	}
	var state string
	if err := r.s.admin.QueryRow(bg(t), `SELECT execution_state FROM execution_receipt WHERE tenant_id = $1 AND grant_id = $2`, tenant, r.d.grants[0]).Scan(&state); err != nil || state != string(controller.StateOutcomeUnknown) {
		t.Fatalf("receipt state %q, %v; want OUTCOME_UNKNOWN (the reply was really lost)", state, err)
	}
	cmds := r.s.bench.commands(t)
	if received, committed := creates(cmds, theKey()); received != 1 || committed != 1 {
		t.Fatalf("the target received %d creates and committed %d; want 1 and 1", received, committed)
	}
	lost := false
	for _, c := range cmds {
		lost = lost || (c.FaultEffect != nil && *c.FaultEffect == "TIMEOUT_AFTER_COMMIT")
	}
	if !lost {
		t.Fatal("the fault trace shows no TIMEOUT_AFTER_COMMIT: the reply was not really lost")
	}
	if len(r.d.grants) != 1 {
		t.Errorf("Trust issued %d grants; a lost reply needs no new grant", len(r.d.grants))
	}
}

// A204: a crash after the spend and before EXECUTING is recorded. The Controller has dispatched (the grant is spent on the Record); the stored projection still says
// PLANNED. The resume asks the Journal, finds the grant spent, never executes again and verifies by key.
func TestResumeWriteAfterACrashBetweenTheSpendAndExecutingNeverDispatchesAgain(t *testing.T) {
	r := newRWRig(t)
	g, err := r.s.trust.issueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := r.s.ctl.Execute(bg(t), tenant, g); err != nil || out.Receipt == nil { // the process dies right after this returns
		t.Fatalf("the first dispatch: %+v, %v", out, err)
	}
	inv := orchestrator.Investigation{ID: rwTurn.InvestigationID, State: "PLANNED", Version: 3}
	res, err := r.run.ResumeWrite(bg(t), rwSession, rwTurn, inv, orchestrator.Decision{ID: rwDecisionID, Outcome: contracts.EnumsV1DecisionOutcomeALLOW, GrantIDs: []string{g}, IntentRef: intentRef}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Investigation.State != "COMPLETED" || res.Investigation.Version <= 3 {
		t.Fatalf("final %+v; want COMPLETED, continuing from version 3", res.Investigation)
	}
	if received, committed := creates(r.s.bench.commands(t), theKey()); received != 1 || committed != 1 {
		t.Fatalf("the target received %d creates and committed %d; want 1 and 1", received, committed)
	}
	if got := r.recordCount(t, "DISPATCH_ATTEMPTED", g); got != 1 {
		t.Errorf("the Record holds %d DISPATCH_ATTEMPTED for %s; want 1", got, g)
	}
}

// Security Reviewer, item 4 on noema#576 (INV-03, AGENT-08): two resumes of the same investigation at once. Both read the Journal, find nothing spent and reach Execute;
// only the database's unique spend stops the second (the Controller reports a replay, or the serialisable spend does not commit). The target received one create, the
// Record holds one GRANT_SPENT, and at most one resume completes; the other stops with an error and writes nothing.
func TestTwoResumesAtOnceDispatchTheGrantOnce(t *testing.T) {
	r := newRWRig(t)
	g, err := r.s.trust.issueCreate(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	inv := orchestrator.Investigation{ID: rwTurn.InvestigationID, State: "PLANNED", Version: 3}
	dec := orchestrator.Decision{ID: rwDecisionID, Outcome: contracts.EnumsV1DecisionOutcomeALLOW, GrantIDs: []string{g}, IntentRef: intentRef}
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]orchestrator.Result, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = r.run.ResumeWrite(bg(t), rwSession, rwTurn, inv, dec, 1)
		}()
	}
	close(start)
	wg.Wait()
	completed := 0
	for i := range results {
		if errs[i] == nil && results[i].Investigation.State == "COMPLETED" {
			completed++
		}
	}
	if completed < 1 {
		t.Fatalf("no resume completed: %v, %v", errs[0], errs[1])
	}
	if received, committed := creates(r.s.bench.commands(t), theKey()); received != 1 || committed != 1 {
		t.Fatalf("the target received %d creates and committed %d; want 1 and 1", received, committed)
	}
	if spent := r.recordCount(t, "GRANT_SPENT", g); spent != 1 {
		t.Errorf("the Record holds %d GRANT_SPENT for %s; want 1", spent, g)
	}
}
