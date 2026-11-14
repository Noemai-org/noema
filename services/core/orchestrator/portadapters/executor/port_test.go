package executorport_test

// STM-03, A204, REQ-EXE-001, INV-03, INV-07, AGENT-07, AGENT-08: the Executor port over the Execution Controller. What the Controller reports becomes the runner's
// receipt class; anything that is not a dispatch or a receipt is an error, never a guess. Identifiers are SYNTHETIC.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	executorport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/executor"
)

type ctl struct {
	out         controller.Outcome
	err         error
	tenant      string
	grant       string
	hadDeadline bool
}

func (c *ctl) Execute(ctx context.Context, tenantID, grantID string) (controller.Outcome, error) {
	_, c.hadDeadline = ctx.Deadline()
	c.tenant, c.grant = tenantID, grantID
	return c.out, c.err
}

var sess = orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_01"}

func port(c *ctl) executorport.Port { return executorport.Port{Controller: c, Timeout: time.Minute} }

func TestTheReceiptStateBecomesTheRunnersReceiptClass(t *testing.T) {
	for state, want := range map[controller.ExecutionState]orchestrator.ReceiptClass{
		controller.StateAcknowledged:         orchestrator.ReceiptApplied,
		controller.StateDispatched:           orchestrator.ReceiptApplied,
		controller.StateRejectedByTarget:     orchestrator.ReceiptApplied,
		controller.StateRejectedStaleVersion: orchestrator.ReceiptApplied,
		controller.StateFailedTerminal:       orchestrator.ReceiptApplied,
		controller.StateOutcomeUnknown:       orchestrator.ReceiptUnknown, // a timeout, a 5xx or silence (INV-07, AGENT-07)
	} {
		c := &ctl{out: controller.Outcome{Receipt: &controller.Receipt{GrantID: "grt_AA11", State: state}}}
		got, err := port(c).Execute(context.Background(), sess, "grt_AA11")
		if err != nil || got.Class != want || got.GrantID != "grt_AA11" {
			t.Errorf("%s: got %+v, %v; want class %s", state, got, err, want)
		}
		if c.tenant != "ten_aa" || c.grant != "grt_AA11" || !c.hadDeadline {
			t.Errorf("%s: the Controller was called with tenant %q, grant %q, deadline %v", state, c.tenant, c.grant, c.hadDeadline)
		}
	}
}

func TestAVoidedGrantIsAVoidedReceiptAndNothingElseIsGuessed(t *testing.T) {
	c := &ctl{out: controller.Outcome{Void: controller.VoidReason("SIGNATURE_INVALID")}}
	if got, err := port(c).Execute(context.Background(), sess, "grt_AA11"); err != nil || got.Class != orchestrator.ReceiptVoided {
		t.Fatalf("got %+v, %v; want a voided receipt", got, err)
	}
	for name, out := range map[string]controller.Outcome{
		"replayed":      {Replayed: true},
		"raced":         {Raced: true},
		"budget spent":  {BudgetSpent: true},
		"nothing set":   {},
		"unknown state": {Receipt: &controller.Receipt{State: "SUCCEEDED"}}, // Execution never writes SUCCEEDED
	} {
		got, err := port(&ctl{out: out}).Execute(context.Background(), sess, "grt_AA11")
		if err == nil || got.Class != "" {
			t.Errorf("%s: got %+v, %v; want an error and no class", name, got, err)
		}
	}
}

func TestAControllerErrorIsReturnedAndNoSessionOrGrantIsRefusedFirst(t *testing.T) {
	boom := errors.New("synthetic controller failure")
	if _, err := port(&ctl{err: boom}).Execute(context.Background(), sess, "grt_AA11"); !errors.Is(err, boom) {
		t.Errorf("err %v, want the Controller's error", err)
	}
	c := &ctl{}
	for name, call := range map[string]func() error{
		"no tenant": func() error {
			_, e := port(c).Execute(context.Background(), orchestrator.Session{PrincipalID: "usr_01"}, "grt_AA11")
			return e
		},
		"no principal": func() error {
			_, e := port(c).Execute(context.Background(), orchestrator.Session{TenantID: "ten_aa"}, "grt_AA11")
			return e
		},
		"no grant": func() error { _, e := port(c).Execute(context.Background(), sess, ""); return e },
		"no controller": func() error {
			_, e := executorport.Port{Timeout: time.Minute}.Execute(context.Background(), sess, "grt_AA11")
			return e
		},
		"no timeout": func() error {
			_, e := executorport.Port{Controller: c}.Execute(context.Background(), sess, "grt_AA11")
			return e
		},
	} {
		if err := call(); !errors.Is(err, executorport.ErrNotUsable) {
			t.Errorf("%s: err %v, want ErrNotUsable", name, err)
		}
	}
	if c.grant != "" {
		t.Errorf("the Controller was called with %q although the call was refused", c.grant)
	}
}
