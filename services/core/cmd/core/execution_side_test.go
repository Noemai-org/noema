package main

// LANE-5 slice 2 (A547, A550, A666, interim; REQ-EXE-001, REQ-EXE-006, REQ-SEC-004; INV-03, INV-07, INV-12, INV-17, AGENT-08, AGENT-16): the Execution side per tenant. The composition
// builds one Execution Controller for each tenant over that tenant's own execution login, the receipt ring and the seams other lanes own, and one tenant-routing Executor. A part that is
// missing is a named gap and builds no Executor: a Controller that cannot do everything dispatches nothing. Identifiers, keys and logins are SYNTHETIC.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

type sideStub struct{}

func (sideStub) VerifyWith(string, []byte, string) bool { return false }
func (sideStub) Dispatch(context.Context, controller.DispatchRequest) controller.TransportResult {
	return controller.TransportResult{}
}
func (sideStub) Revalidate(context.Context, controller.Grant, controller.PlanStep) (controller.Revalidation, error) {
	return controller.Revalidation{}, nil
}
func (sideStub) LatestIntentHash(context.Context, controller.Grant) (string, error) { return "", nil }
func (sideStub) Plan(context.Context, controller.Grant) (controller.PlanStep, error) {
	return controller.PlanStep{}, nil
}
func (sideStub) Verify(context.Context, orchestrator.Session, string, string) (contracts.EnumsV1Verdict, error) {
	return "", nil
}
func (sideStub) Bound(context.Context, orchestrator.Session, orchestrator.Binding) error { return nil }
func (sideStub) Progress(context.Context, orchestrator.Session, string, string) (orchestrator.Progress, error) {
	return orchestrator.Progress{}, nil
}

func sideSeams() executionSeams {
	clk := clock.NewManual(time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC))
	st := sideStub{}
	return executionSeams{Clock: clk, IDs: ids.New(clk, nil), Keys: st, Dispatcher: st, Revalidator: st, Intents: st, Plans: st, Verifier: st, Bindings: st}
}

func sideEnv(d execDirs, extra envMap) Env {
	e := envMap{EnvExecutionDSNDir: d.exec, EnvOutcomeDSNDir: d.outcome, EnvExecutionKeyDir: d.keys, EnvExecutionKeyMaxActive: "2",
		EnvExecutionRollbackTimeout: "5000", EnvExecutionExecuteTimeout: "30000", ledger.EnvSerializationAttempts: "3"}
	for k, v := range extra {
		e[k] = v
	}
	return Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
}

func sideParts(t *testing.T, tenants ...string) (execDirs, *executionParts) {
	t.Helper()
	d := newExecDirs(t, tenants...)
	parts, closer, err := buildExecution(t.Context(), execEnv(d, "2"))
	if err != nil || parts == nil {
		t.Fatalf("slice 1: %v", err)
	}
	t.Cleanup(closer)
	return d, parts
}

func TestACompleteExecutionSideIsOneControllerForEachTenantOnThatTenantsOwnLogin(t *testing.T) { // REQ-SEC-004, INV-12
	d, parts := sideParts(t, "ten_aa", "ten_bb")
	side, err := buildExecutionSide(sideEnv(d, nil), parts, sideSeams())
	if err != nil || side == nil || len(side.Gaps) != 0 || side.Executor == nil || len(side.Controllers) != 2 {
		t.Fatalf("side %+v, err %v; want two controllers, an Executor and no gap", side, err)
	}
	for _, tenant := range []string{"ten_aa", "ten_bb"} {
		ctl := side.Controllers[tenant]
		if ctl == nil {
			t.Fatalf("no controller for %s", tenant)
		}
		l, ok := ctl.Ledger.(*ledger.Ledger)
		if !ok || l.Pool != parts.ExecutionPools[tenant] || l.RollbackTimeout != 5*time.Second || l.SerializationAttempts != 3 || l.Rec == nil || l.Rec.Signers["core-execution"] == nil {
			t.Errorf("%s: ledger %+v is not over this tenant's pool, its settings and the core-execution signer", tenant, l)
		}
		if g, ok := ctl.Grants.(*ledger.Ledger); !ok || g != l {
			t.Errorf("%s: grants are not read through the same ledger", tenant)
		}
		b, ok := ctl.Bind.(*ledger.StoreBinder)
		if !ok || b.Pool != parts.ExecutionPools[tenant] || b.Ledger != l {
			t.Errorf("%s: the binder is not over this tenant's pool and ledger", tenant)
		}
		if ctl.Receipts != controller.ReceiptSigner(parts.Receipts) || ctl.Keys == nil || ctl.Out == nil || ctl.Reval == nil || ctl.Clock == nil || ctl.IDs == nil {
			t.Errorf("%s: the controller lacks a part", tenant)
		}
		if ctl.Boundary != nil {
			t.Errorf("%s: the crash-harness hook is set in production", tenant)
		}
	}
	if side.Controllers["ten_aa"] == side.Controllers["ten_bb"] {
		t.Error("two tenants share one controller")
	}
}

func TestAMissingPartBuildsNoExecutorAndIsOneFixedGapLine(t *testing.T) { // a Controller that cannot do everything dispatches nothing
	d, parts := sideParts(t, "ten_aa")
	for name, c := range map[string]struct {
		mut func(*executionSeams)
		env envMap
		gap string
	}{
		"clock":               {func(s *executionSeams) { s.Clock = nil }, nil, "execution: clock missing"},
		"ids":                 {func(s *executionSeams) { s.IDs = nil }, nil, "execution: ids missing"},
		"grant keys":          {func(s *executionSeams) { s.Keys = nil }, nil, "execution: grant keys missing"},
		"dispatcher":          {func(s *executionSeams) { s.Dispatcher = nil }, nil, "execution: dispatcher missing"},
		"revalidator":         {func(s *executionSeams) { s.Revalidator = nil }, nil, "execution: revalidator missing"},
		"intent source":       {func(s *executionSeams) { s.Intents = nil }, nil, "execution: intent source missing"},
		"plan source":         {func(s *executionSeams) { s.Plans = nil }, nil, "execution: plan source missing"},
		"rollback timeout":    {func(*executionSeams) {}, envMap{EnvExecutionRollbackTimeout: ""}, "execution: " + EnvExecutionRollbackTimeout + " missing"},
		"execute timeout":     {func(*executionSeams) {}, envMap{EnvExecutionExecuteTimeout: ""}, "execution: " + EnvExecutionExecuteTimeout + " missing"},
		"serialization tries": {func(*executionSeams) {}, envMap{ledger.EnvSerializationAttempts: ""}, "execution: " + ledger.EnvSerializationAttempts + " missing"},
	} {
		s := sideSeams()
		c.mut(&s)
		side, err := buildExecutionSide(sideEnv(d, c.env), parts, s)
		if err != nil || side == nil || side.Executor != nil || len(side.Controllers) != 0 {
			t.Errorf("%s: side %+v, err %v; want no Executor and no controller", name, side, err)
			continue
		}
		if len(side.Gaps) != 1 || side.Gaps[0] != c.gap {
			t.Errorf("%s: gaps %q, want exactly %q", name, side.Gaps, c.gap)
		}
	}
}

func TestWithNoExecutionPartsTheSideIsOneGapLineAndNoError(t *testing.T) {
	side, err := buildExecutionSide(Env{Getenv: envMap{}.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, nil, sideSeams())
	if err != nil || side == nil || side.Executor != nil || len(side.Gaps) != 1 || side.Gaps[0] != "execution: not configured" {
		t.Fatalf("side %+v, err %v; want the one gap 'execution: not configured'", side, err)
	}
}

func TestAnInvalidExecutionSettingIsAnErrorThatNamesItAndNeverTheValue(t *testing.T) { // AGENT-13, AGENT-16
	d, parts := sideParts(t, "ten_aa")
	for _, name := range []string{EnvExecutionRollbackTimeout, EnvExecutionExecuteTimeout, ledger.EnvSerializationAttempts} {
		for _, bad := range []string{"0", "-5", "abc", "1.5", "007", " 5", "99999999999999999999"} {
			side, err := buildExecutionSide(sideEnv(d, envMap{name: bad}), parts, sideSeams())
			if err == nil || side != nil || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), bad) {
				t.Errorf("%s=%q: side %v, err %v; want an error that names the setting and not the value", name, bad, side, err)
			}
		}
	}
}

func TestTheExecutorSendsAGrantOnlyToTheSessionsOwnTenantAndRefusesOthers(t *testing.T) { // INV-12, INV-03
	d, parts := sideParts(t, "ten_aa", "ten_bb")
	side, err := buildExecutionSide(sideEnv(d, nil), parts, sideSeams())
	if err != nil || side.Executor == nil {
		t.Fatal(err)
	}
	for name, s := range map[string]orchestrator.Session{
		"an unknown tenant": {TenantID: "ten_zz", PrincipalID: "usr_1"},
		"an empty tenant":   {PrincipalID: "usr_1"},
		"no principal":      {TenantID: "ten_aa"},
	} {
		rec, err := side.Executor.Execute(t.Context(), s, "grt_0001")
		if err == nil || rec != (orchestrator.Receipt{}) {
			t.Errorf("%s: receipt %+v, err %v; want a refusal and no receipt", name, rec, err)
		}
	}
	// an execute call with no deadline never reaches the ledger: it is refused by the controller's own rule
	if _, err := side.Controllers["ten_aa"].Execute(context.Background(), "ten_aa", "grt_0001"); err == nil || errors.Is(err, context.Canceled) {
		t.Errorf("execute with no deadline: %v; want a refusal", err)
	}
}

func TestWritePortsNameEachPartStillMissing(t *testing.T) {
	d, parts := sideParts(t, "ten_aa")
	s := sideSeams()
	s.Verifier, s.Bindings = nil, nil
	side, err := buildExecutionSide(sideEnv(d, nil), parts, s)
	if err != nil {
		t.Fatal(err)
	}
	wp, gaps := side.WritePorts(nil)
	want := []string{"execution: verifier not built", "execution: bindings not built", "execution: journal not built"}
	if strings.Join(gaps, "|") != strings.Join(want, "|") || wp != (orchestrator.WritePorts{}) {
		t.Fatalf("ports %+v, gaps %q; want no ports and the gaps %q", wp, gaps, want)
	}
	full, err := buildExecutionSide(sideEnv(d, nil), parts, sideSeams())
	if err != nil {
		t.Fatal(err)
	}
	wp, gaps = full.WritePorts(sideStub{})
	if len(gaps) != 0 || wp.Executor == nil || wp.Verifier == nil || wp.Bindings == nil || wp.Journal == nil {
		t.Fatalf("ports %+v, gaps %q; want every port and no gap", wp, gaps)
	}
	if a, b := wp.Executor, orchestrator.Executor(full.Executor); a != b {
		t.Error("the write ports hold another Executor than the side's")
	}
}

type callRecorder struct{ calls *[]string }

func (c callRecorder) Execute(_ context.Context, s orchestrator.Session, grantID string) (orchestrator.Receipt, error) {
	*c.calls = append(*c.calls, s.TenantID+"/"+grantID)
	return orchestrator.Receipt{GrantID: grantID, Class: orchestrator.ReceiptApplied}, nil
}

// Added with the green, not seen red: the router calls the Executor of the session's own tenant and no other.
func TestTheRouterCallsOnlyTheExecutorOfTheSessionsTenant(t *testing.T) { // INV-12, INV-03
	var a, b []string
	x := &tenantExecutor{byTenant: map[string]orchestrator.Executor{"ten_aa": callRecorder{&a}, "ten_bb": callRecorder{&b}}}
	if _, err := x.Execute(t.Context(), orchestrator.Session{TenantID: "ten_bb", PrincipalID: "usr_1"}, "grt_0001"); err != nil {
		t.Fatal(err)
	}
	if len(a) != 0 || len(b) != 1 || b[0] != "ten_bb/grt_0001" {
		t.Fatalf("tenant A saw %v, tenant B saw %v; want only B to be called", a, b)
	}
	if _, err := x.Execute(t.Context(), orchestrator.Session{TenantID: "ten_cc", PrincipalID: "usr_1"}, "grt_0002"); !errors.Is(err, ErrNoExecutionForTenant) || len(a)+len(b) != 1 {
		t.Fatalf("an unknown tenant: err %v, calls %v %v; want ErrNoExecutionForTenant and no call", err, a, b)
	}
	var nilX *tenantExecutor
	if _, err := nilX.Execute(t.Context(), orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_1"}, "grt_0003"); !errors.Is(err, ErrNoExecutionForTenant) {
		t.Fatalf("a nil router: %v", err)
	}
}
