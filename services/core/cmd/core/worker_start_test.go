package main

// TRU-10 (REQ-TRU-010, REQ-EXE-006; INV-12, INV-30, AGENT-08, AGENT-16; A539, interim, relayed, not yet committed): the start site of the escalation workflow. The disposition decision
// of the recorded ESCALATE becomes the workflow input; the tenant is the verified session's; the workflow id is one per tenant and correlation, so a second start finds the first. The
// client is a recording stand-in for the Temporal client: it fails nothing and decides nothing. Every value is SYNTHETIC.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/workflows"
)

type escStartedRun struct {
	client.WorkflowRun
	id, run string
}

func (r escStartedRun) GetID() string    { return r.id }
func (r escStartedRun) GetRunID() string { return r.run }

type escStartCall struct {
	opts client.StartWorkflowOptions
	name any
	args []any
}

type escStartRecorder struct {
	calls []escStartCall
	err   error
}

func (s *escStartRecorder) ExecuteWorkflow(_ context.Context, o client.StartWorkflowOptions, wf interface{}, args ...interface{}) (client.WorkflowRun, error) {
	s.calls = append(s.calls, escStartCall{opts: o, name: wf, args: args})
	if s.err != nil {
		return nil, s.err
	}
	return escStartedRun{id: o.ID, run: "run_1"}, nil
}

func escStartCfg() *WorkerConfig {
	return &WorkerConfig{Address: "temporal:7233", Namespace: "noema", TaskQueue: "noema-core", ActivityTimeout: 30 * time.Second}
}

func escStartReq() EscalationStart {
	return EscalationStart{Session: orchestrator.Session{TenantID: "ten_a", PrincipalID: "prn_1", Role: "customer", AuthnRef: "ses_1"}, CorrelationID: "corr_1",
		DispositionDecisionID: "dec_disposition1", CreateGrantID: "grt_create", PlannedQueue: "disputes_l2"}
}

func escStartCtx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func newEscStarter(t *testing.T, rec *escStartRecorder) *EscalationStarter {
	t.Helper()
	s, err := NewEscalationStarter(rec, escStartCfg())
	if err != nil {
		t.Fatalf("NewEscalationStarter: %v", err)
	}
	return s
}

// The recorded ESCALATE's decision ID is the workflow's start input, with the tenant of the verified session, the configured activity timeout and the configured task queue.
func TestTheEscalationStartsWithTheDispositionAsInputUnderTheSessionsTenant(t *testing.T) {
	rec := &escStartRecorder{}
	got, err := newEscStarter(t, rec).Start(escStartCtx(t), escStartReq())
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("the client was called %d times, want once", len(rec.calls))
	}
	c := rec.calls[0]
	if c.name != workflows.EscalateWorkflowName || c.opts.TaskQueue != "noema-core" {
		t.Errorf("started %v on %q, want %s on noema-core", c.name, c.opts.TaskQueue, workflows.EscalateWorkflowName)
	}
	want := workflows.EscalateInput{TenantID: "ten_a", CorrelationID: "corr_1", CreateGrantID: "grt_create", PlannedQueue: "disputes_l2", DispositionDecisionID: "dec_disposition1",
		ActivityTimeout: 30 * time.Second}
	if len(c.args) != 1 || c.args[0] != want {
		t.Errorf("input %+v, want %+v", c.args, want)
	}
	if got.WorkflowID != c.opts.ID || got.WorkflowID == "" || got.RunID != "run_1" {
		t.Errorf("started %+v, want the workflow id of the options and run_1", got)
	}
}

// AGENT-08, INV-30: one workflow id per tenant and correlation, the same every time (a second start finds the first), and none shared by two tenants or two correlations. The start
// does not make a second run while the first is open: the SDK then returns the run that exists. A duplicate of a closed run is rejected too (see the test below).
func TestTheWorkflowIDIsOnePerTenantAndCorrelation(t *testing.T) {
	rec := &escStartRecorder{}
	s := newEscStarter(t, rec)
	a, err := s.Start(escStartCtx(t), escStartReq())
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Start(escStartCtx(t), escStartReq())
	if err != nil || a.WorkflowID != b.WorkflowID {
		t.Fatalf("second start: %+v, %v; want the same id %q", b, err, a.WorkflowID)
	}
	other := escStartReq()
	other.CorrelationID = "corr_2"
	c, _ := s.Start(escStartCtx(t), other)
	otherTenant := escStartReq()
	otherTenant.Session.TenantID = "ten_b"
	d, _ := s.Start(escStartCtx(t), otherTenant)
	seen := map[string]bool{a.WorkflowID: true}
	for _, id := range []string{c.WorkflowID, d.WorkflowID} {
		if id == "" || seen[id] {
			t.Errorf("the workflow id %q is empty or shared with another start", id)
		}
		seen[id] = true
	}
	for _, c := range rec.calls {
		if c.opts.WorkflowExecutionErrorWhenAlreadyStarted {
			t.Error("the start asks the server to fail on a duplicate: a repeat start would be an error, not the first run")
		}
	}
}

// INV-12, AGENT-16: what the start cannot trust is refused before the client is called: no verified tenant or principal, an incomplete case, no deadline.
func TestAnIncompleteStartIsRefusedWithoutCallingTemporal(t *testing.T) {
	cuts := map[string]func(*EscalationStart){
		"tenant":      func(r *EscalationStart) { r.Session.TenantID = "" },
		"principal":   func(r *EscalationStart) { r.Session.PrincipalID = "" },
		"correlation": func(r *EscalationStart) { r.CorrelationID = "" },
		"disposition": func(r *EscalationStart) { r.DispositionDecisionID = "" },
		"create":      func(r *EscalationStart) { r.CreateGrantID = "" },
		"queue":       func(r *EscalationStart) { r.PlannedQueue = "" },
	}
	for name, cut := range cuts {
		rec := &escStartRecorder{}
		r := escStartReq()
		cut(&r)
		got, err := newEscStarter(t, rec).Start(escStartCtx(t), r)
		if err == nil || got != (StartedEscalation{}) || len(rec.calls) != 0 {
			t.Errorf("no %s: %+v, %v (client called %d times); want a refusal before the client", name, got, err, len(rec.calls))
		}
	}
	rec := &escStartRecorder{}
	if _, err := newEscStarter(t, rec).Start(context.Background(), escStartReq()); err == nil || len(rec.calls) != 0 {
		t.Errorf("no deadline: %v (client called %d times); want a refusal before the client", err, len(rec.calls))
	}
}

// A start that Temporal refuses is an error with no result; its text never carries the server's words (an address or a login must not reach a log).
func TestAFailedStartIsAnErrorAndNamesNoWorkflow(t *testing.T) {
	rec := &escStartRecorder{err: errors.New("synthetic dial tcp 10.0.0.1:7233 refused")}
	got, err := newEscStarter(t, rec).Start(escStartCtx(t), escStartReq())
	if err == nil || got != (StartedEscalation{}) {
		t.Fatalf("%+v, %v; want an error and no workflow", got, err)
	}
	if strings.Contains(err.Error(), "10.0.0.1") {
		t.Errorf("the error carries the server's words: %v", err)
	}
	if !errors.Is(err, rec.err) {
		t.Errorf("the cause is not reachable with errors.Is: %v", err)
	}
}

// No client or no configuration builds no starter: a half-built start would run nothing or the wrong queue.
func TestAStarterNeedsAClientAndTheWorkerConfiguration(t *testing.T) {
	if _, err := NewEscalationStarter(nil, escStartCfg()); err == nil {
		t.Error("no client: want an error")
	}
	if _, err := NewEscalationStarter(&escStartRecorder{}, nil); err == nil {
		t.Error("no configuration: want an error")
	}
	if _, err := NewEscalationStarter(&escStartRecorder{}, &WorkerConfig{TaskQueue: "noema-core"}); err == nil {
		t.Error("no activity timeout: want an error")
	}
	if _, err := NewEscalationStarter(&escStartRecorder{}, &WorkerConfig{ActivityTimeout: time.Second}); err == nil {
		t.Error("no task queue: want an error")
	}
}

// AGENT-08, INV-30 (A539; the user's go-ahead of 25 Nov, interim, not yet committed; C1 and SR note 1 on noema#945): a repeat start after the first run has closed must not run the case again.
// The start asks the server to reject a duplicate of a closed run, so the SDK returns the run that exists and no second EscalateCase begins; a retry needs a new grant, not a new run.
func TestTheStartRejectsADuplicateOfAClosedRunSoTheCaseIsNeverRunTwice(t *testing.T) {
	rec := &escStartRecorder{}
	if _, err := newEscStarter(t, rec).Start(escStartCtx(t), escStartReq()); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("the client was called %d times, want once", len(rec.calls))
	}
	if got := rec.calls[0].opts.WorkflowIDReusePolicy; got != enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE {
		t.Errorf("the workflow id reuse policy is %v, want REJECT_DUPLICATE", got)
	}
}
