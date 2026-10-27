package workflows_test

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"

	"github.com/Noemai-org/noema/services/core/workflows"
)

const (
	tenantID = "ten_aa"
	timeout  = 30 * time.Second
)

// script is a set of ports with scripted answers; it records every call so a test can say what did not happen.
type script struct {
	mu        sync.Mutex
	calls     []string
	runs      map[string]workflows.GrantRun // by grant ID
	runErr    map[string]error
	view      workflows.CaseView
	viewErrs  []error // one per read attempt; nil entries succeed
	issueID   string
	issueErr  error
	issueReqs []workflows.RouteGrantRequest
}

func (s *script) note(c string) {
	s.mu.Lock()
	s.calls = append(s.calls, c)
	s.mu.Unlock()
}

func (s *script) count(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (s *script) ExecuteGrant(_ context.Context, _, grantID string) (workflows.GrantRun, error) {
	s.note("execute:" + grantID)
	if err := s.runErr[grantID]; err != nil {
		return workflows.GrantRun{}, err
	}
	return s.runs[grantID], nil
}

func (s *script) ReadCaseByKey(_ context.Context, _, key string) (workflows.CaseView, error) {
	s.note("read:" + key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.viewErrs) > 0 {
		err := s.viewErrs[0]
		s.viewErrs = s.viewErrs[1:]
		if err != nil {
			return workflows.CaseView{}, err
		}
	}
	return s.view, nil
}

func (s *script) IssueRouteGrant(_ context.Context, req workflows.RouteGrantRequest) (string, error) {
	s.note("issue")
	s.mu.Lock()
	s.issueReqs = append(s.issueReqs, req)
	s.mu.Unlock()
	return s.issueID, s.issueErr
}

func acked(grant, receipt string) workflows.GrantRun {
	return workflows.GrantRun{GrantID: grant, ReceiptID: receipt, State: "ACKNOWLEDGED", TransportClass: "ACK_2XX", RawCode: "201", ExternalAck: "CMP-1", IdempotencyKey: "idk_v1_k1", Attempt: 1}
}

func newScript() *script {
	return &script{
		runs: map[string]workflows.GrantRun{"grt_create": acked("grt_create", "rcp_create"), "grt_route": acked("grt_route", "rcp_route")},
		view: workflows.CaseView{CaseID: "case_1", Reference: "CMP-1", Status: "OPEN", OwnerQueue: "PAYMENTS_OPERATIONS", VersionToken: "v1"}, issueID: "grt_route",
		runErr: map[string]error{},
	}
}

func run(t *testing.T, s *script, in workflows.EscalateInput) (workflows.EscalateResult, error) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(workflows.EscalateCase, workflowOptions())
	env.RegisterActivity(&workflows.EscalateActivities{Executor: s, Reader: s, Issuer: s})
	env.ExecuteWorkflow(workflows.EscalateWorkflowName, in)
	if !env.IsWorkflowCompleted() {
		t.Fatal("the workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		return workflows.EscalateResult{}, err
	}
	var res workflows.EscalateResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatal(err)
	}
	return res, nil
}

func input() workflows.EscalateInput {
	return workflows.EscalateInput{TenantID: tenantID, CorrelationID: "corr_abc123", CreateGrantID: "grt_create", PlannedQueue: "DISPUTES", ActivityTimeout: timeout}
}

// Graph fin.graph.escalate: create, read back by key, route on the token read back. Both receipts acknowledged: the
// case awaits verification, because Execution cannot say it succeeded.
func TestEscalateRunsCreateReadBackAndRouteInOrder(t *testing.T) {
	s := newScript()
	res, err := run(t, s, input())
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != workflows.StatusAwaitingVerification || res.Reason != "" {
		t.Fatalf("result %+v", res)
	}
	want := []string{"execute:grt_create", "read:idk_v1_k1", "issue", "execute:grt_route"}
	if strings.Join(s.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls %v, want %v", s.calls, want)
	}
	req := s.issueReqs[0]
	if req.CaseID != "case_1" || req.VersionToken != "v1" || req.PlannedQueue != "DISPUTES" || req.CreateReceiptID != "rcp_create" || req.TenantID != tenantID {
		t.Fatalf("the route grant was asked for with %+v", req)
	}
	if res.ReadBack == nil || res.ReadBack.VersionToken != "v1" || res.Route == nil || res.RouteGrantID != "grt_route" {
		t.Fatalf("result %+v", res)
	}
}

// REQ-EXE-006, REQ-RCV-002: any write that is not acknowledged stops the case. Nothing further is written, and the
// workflow does not retry it.
func TestAWriteThatIsNotAcknowledgedStopsTheCase(t *testing.T) {
	unknown := workflows.GrantRun{GrantID: "grt_create", ReceiptID: "rcp_create", State: "OUTCOME_UNKNOWN", TransportClass: "TIMEOUT", IdempotencyKey: "idk_v1_k1", Attempt: 1}
	rejected := workflows.GrantRun{GrantID: "grt_create", ReceiptID: "rcp_create", State: "REJECTED_BY_TARGET", TransportClass: "REJECTED_4XX", IdempotencyKey: "idk_v1_k1", Attempt: 1}
	for name, c := range map[string]struct {
		create workflows.GrantRun
		reason string
	}{
		"timeout":  {unknown, workflows.ReasonCreateNotAcknowledged},
		"rejected": {rejected, workflows.ReasonCreateNotAcknowledged},
		"void":     {workflows.GrantRun{GrantID: "grt_create", Void: "EXPIRED"}, workflows.ReasonCreateVoid},
		"replay":   {workflows.GrantRun{GrantID: "grt_create", Replayed: true}, workflows.ReasonCreateReplay},
	} {
		s := newScript()
		s.runs["grt_create"] = c.create
		res, err := run(t, s, input())
		if err != nil || res.Status != workflows.StatusStopped || res.Reason != c.reason {
			t.Errorf("%s: %+v, %v; want STOPPED %s", name, res, err, c.reason)
		}
		if s.count("execute:") != 1 || s.count("read:") != 0 || s.count("issue") != 0 {
			t.Errorf("%s: calls %v; want only the one create", name, s.calls)
		}
	}
	// The same for the route.
	for name, c := range map[string]struct {
		route  workflows.GrantRun
		reason string
	}{
		"stale version": {workflows.GrantRun{GrantID: "grt_route", ReceiptID: "rcp_route", State: "REJECTED_STALE_VERSION"}, workflows.ReasonRouteNotAcknowledged},
		"void":          {workflows.GrantRun{GrantID: "grt_route", Void: "STATE_CHANGED"}, workflows.ReasonRouteVoid},
		"replay":        {workflows.GrantRun{GrantID: "grt_route", Replayed: true}, workflows.ReasonRouteReplay},
	} {
		s := newScript()
		s.runs["grt_route"] = c.route
		res, err := run(t, s, input())
		if err != nil || res.Status != workflows.StatusStopped || res.Reason != c.reason || res.Route == nil {
			t.Errorf("route %s: %+v, %v; want STOPPED %s", name, res, err, c.reason)
		}
	}
}

// D8: a write activity runs once. If ExecuteGrant returns an error, the workflow fails and the grant is not executed again.
func TestExecuteGrantIsNeverRetriedByTemporal(t *testing.T) {
	s := newScript()
	s.runErr["grt_create"] = errors.New("the ledger could not be reached")
	if _, err := run(t, s, input()); err == nil {
		t.Fatal("the workflow succeeded although ExecuteGrant failed")
	}
	if n := s.count("execute:grt_create"); n != 1 {
		t.Fatalf("ExecuteGrant ran %d times, want 1", n)
	}
}

// D8: a read activity retries with bounded backoff. Two failures and a success is one case read; three failures stop the case.
func TestReadBackRetriesWithinItsBoundAndNeverWritesAgain(t *testing.T) {
	s := newScript()
	s.viewErrs = []error{errors.New("unavailable"), errors.New("unavailable"), nil}
	res, err := run(t, s, input())
	if err != nil || res.Status != workflows.StatusAwaitingVerification {
		t.Fatalf("%+v, %v", res, err)
	}
	if s.count("read:") != 3 || s.count("execute:grt_create") != 1 {
		t.Fatalf("calls %v; want 3 reads and 1 create", s.calls)
	}

	s = newScript()
	s.viewErrs = []error{errors.New("unavailable"), errors.New("unavailable"), errors.New("unavailable"), nil}
	res, err = run(t, s, input())
	if err != nil || res.Status != workflows.StatusStopped || res.Reason != workflows.ReasonCaseNotReadable {
		t.Fatalf("%+v, %v; want STOPPED %s", res, err, workflows.ReasonCaseNotReadable)
	}
	if s.count("read:") != 3 || s.count("issue") != 0 || s.count("execute:") != 1 {
		t.Fatalf("calls %v; want 3 reads, no grant asked for, one write", s.calls)
	}
}

// A refused route grant is a stop, not a retry: Trust decides, the workflow does not ask twice.
func TestARefusedRouteGrantStopsTheCaseWithoutAskingTwice(t *testing.T) {
	s := newScript()
	s.issueErr = errors.New("Trust refused")
	res, err := run(t, s, input())
	if err != nil || res.Status != workflows.StatusStopped || res.Reason != workflows.ReasonRouteGrantRefused {
		t.Fatalf("%+v, %v", res, err)
	}
	if s.count("issue") != 1 || s.count("execute:grt_route") != 0 {
		t.Fatalf("calls %v", s.calls)
	}
}

// The workflow refuses to start without what it needs; it never picks a timeout of its own (REQ-OPS-003).
func TestEscalateRefusesIncompleteInput(t *testing.T) {
	for name, mutate := range map[string]func(*workflows.EscalateInput){
		"no timeout": func(i *workflows.EscalateInput) { i.ActivityTimeout = 0 },
		"no tenant":  func(i *workflows.EscalateInput) { i.TenantID = "" },
		"no grant":   func(i *workflows.EscalateInput) { i.CreateGrantID = "" },
		"no queue":   func(i *workflows.EscalateInput) { i.PlannedQueue = "" },
	} {
		in := input()
		mutate(&in)
		if _, err := run(t, newScript(), in); err == nil {
			t.Errorf("%s: the workflow started", name)
		}
	}
}

// The literal MaximumAttempts in options.go is the one in the Activities table (the scanner needs a literal).
func TestReadOptionsMatchTheDeclaredPolicy(t *testing.T) {
	a, ok := workflows.PolicyFor("ReadBackCase")
	if !ok || a.Kind != workflows.Read || a.Policy.MaximumAttempts != 3 || len(a.Policy.BackoffSeconds) != 3 {
		t.Fatalf("ReadBackCase policy %+v", a)
	}
	src, err := os.ReadFile("options.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "MaximumAttempts:    "+strconv.Itoa(a.Policy.MaximumAttempts)) {
		t.Fatal("options.go does not carry the MaximumAttempts of the Activities table")
	}
}

// INV-30: workflow code does no I/O and reads no clock. The workflow file may import only the Temporal workflow
// package and a handful of pure packages.
func TestWorkflowCodeImportsNothingThatDoesIO(t *testing.T) {
	allowed := map[string]bool{"errors": true, "fmt": true, "time": true, "go.temporal.io/sdk/workflow": true}
	for _, file := range []string{"escalate.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if !allowed[path] {
				t.Errorf("%s imports %q: workflow code does no I/O", file, path)
			}
		}
	}
	src, _ := os.ReadFile("escalate.go")
	for _, banned := range []string{"time.Now", "time.Sleep", "go func", "context.Background", "rand."} {
		if strings.Contains(string(src), banned) {
			t.Errorf("escalate.go uses %s: not deterministic", banned)
		}
	}
}
