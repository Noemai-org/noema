package controller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// TST-01 gap 2 (INV-03, INV-17, INV-05, INV-18, REQ-EXE-003, REQ-EXE-008, ruling A153): the checks of E3 to E7 and the dispatch budget of E8 under
// a read that cannot be made, a plan step that names an unregistered, prohibited or adapter-less capability, a grant that is not single-use or has no correlation
// ID, and a spent dispatch budget. Whatever goes wrong there, nothing is spent and nothing reaches the target. Everything here is SYNTHETIC.

var errRead = errors.New("synthetic: the read could not be made")

// reads wraps the world's Binder and Revalidator so that one read fails or one plan step is edited.
type reads struct {
	*World
	intentErr, decisionErr, policyErr, planErr, writesErr, revalErr error
	editPlan                                                        func(*controller.PlanStep)
}

func (r *reads) LatestIntentHash(ctx context.Context, g controller.Grant) (string, error) {
	if r.intentErr != nil {
		return "", r.intentErr
	}
	return r.World.LatestIntentHash(ctx, g)
}

func (r *reads) DecisionStillAllowed(ctx context.Context, g controller.Grant) (bool, error) {
	if r.decisionErr != nil {
		return false, r.decisionErr
	}
	return r.World.DecisionStillAllowed(ctx, g)
}

func (r *reads) ActivePolicyVersion(ctx context.Context, g controller.Grant) (string, error) {
	if r.policyErr != nil {
		return "", r.policyErr
	}
	return r.World.ActivePolicyVersion(ctx, g)
}

func (r *reads) Plan(ctx context.Context, g controller.Grant) (controller.PlanStep, error) {
	if r.planErr != nil {
		return controller.PlanStep{}, r.planErr
	}
	s, err := r.World.Plan(ctx, g)
	if r.editPlan != nil {
		r.editPlan(&s)
	}
	return s, err
}

func (r *reads) WritesEnabled(ctx context.Context, tenantID string) (bool, error) {
	if r.writesErr != nil {
		return false, r.writesErr
	}
	return r.World.WritesEnabled(ctx, tenantID)
}

func (r *reads) Revalidate(ctx context.Context, g controller.Grant, s controller.PlanStep) (controller.Revalidation, error) {
	if r.revalErr != nil {
		return controller.Revalidation{}, r.revalErr
	}
	return r.World.Revalidate(ctx, g, s)
}

// budgetSpent is a Ledger whose database refuses the spend because the key's dispatch budget is used up (migration 0023, NS002): nothing is written.
type budgetSpent struct{ controller.Ledger }

func (budgetSpent) Spend(context.Context, controller.SpendCommand) (controller.SpendResult, error) {
	return controller.SpendResult{BudgetSpent: true}, nil
}

type unsafeRead struct {
	name     string
	prepare  func(w *World, now time.Time) controller.SignedGrant
	set      func(r *reads)
	wantVoid controller.VoidReason // "" when the protocol must stop with an error and no void
}

var unsafeReads = []unsafeRead{
	{name: "E7 the revalidation read cannot be made", set: func(r *reads) { r.revalErr = errRead }, wantVoid: controller.VoidRevalidationFailed},
	{name: "E7 a revalidated fact changed", set: func(r *reads) { r.World.RevalChanged = []string{"ownership"} }, wantVoid: controller.VoidStateChanged},
	{name: "E6 the kill switch cannot be read", set: func(r *reads) { r.writesErr = errRead }, wantVoid: controller.VoidWritesDisabled},
	{name: "E5 the capability is registered, has an adapter and is prohibited", set: func(r *reads) { r.editPlan = func(s *controller.PlanStep) { s.Prohibited = true } }, wantVoid: controller.VoidBindingMismatch},
	{name: "E5 the capability is not registered", set: func(r *reads) { r.editPlan = func(s *controller.PlanStep) { s.Registered = false } }, wantVoid: controller.VoidBindingMismatch},
	{name: "E5 the capability has no adapter", set: func(r *reads) { r.editPlan = func(s *controller.PlanStep) { s.AdapterExists = false } }, wantVoid: controller.VoidBindingMismatch},
	{name: "E5 the plan cannot be read", set: func(r *reads) { r.planErr = errRead }},
	{name: "E4 the active policy cannot be read", set: func(r *reads) { r.policyErr = errRead }},
	{name: "E4 the decision cannot be read", set: func(r *reads) { r.decisionErr = errRead }},
	{name: "E3 the latest intent cannot be read", set: func(r *reads) { r.intentErr = errRead }},
	{name: "E2 a grant that is not single-use, validly signed", prepare: func(w *World, now time.Time) controller.SignedGrant {
		return resigned(func(g *controller.Grant) { g.SingleUse = false })(w, now)
	}, wantVoid: controller.VoidBindingMismatch},
	{name: "E2 a grant with no correlation ID, validly signed", prepare: func(w *World, now time.Time) controller.SignedGrant {
		return resigned(func(g *controller.Grant) { g.CorrelationID = "" })(w, now)
	}, wantVoid: controller.VoidBindingMismatch},
}

func checkINV03Reads(t failer, u unsafeRead, fault FaultMode, times int) {
	w := NewWorld(secret)
	w.SetFaults(func() FaultMode { return fault })
	prepare := u.prepare
	if prepare == nil {
		prepare = fresh
	}
	g := prepare(w, w.Clock.Now())
	w.Store(g)
	r := &reads{World: w}
	if u.set != nil {
		u.set(r)
	}
	var lastErr error
	var last controller.Outcome
	for i := 0; i < times; i++ {
		c := controllerFor(w, nil)
		c.Bind, c.Reval = r, r
		ctx, cancel := deadline()
		last, lastErr = c.Execute(ctx, TenantID, g.Grant.GrantID)
		cancel()
		if u.wantVoid == "" && lastErr == nil {
			t.Fatalf("%q: Execute returned no error; a read that cannot be made must stop the protocol", u.name)
		}
	}
	s := w.Snapshot()
	if len(s.Dispatches) != 0 || len(s.Spent) != 0 || len(s.Receipts) != 0 {
		t.Fatalf("%q under fault %d: %d dispatches, %d spent grants, %d receipts; this must reach nothing", u.name, fault, len(s.Dispatches), len(s.Spent), len(s.Receipts))
	}
	if u.wantVoid != "" && s.Voids[g.Grant.GrantID] != u.wantVoid {
		t.Fatalf("%q: the grant is void as %q (last outcome %+v, error %v), want %q", u.name, s.Voids[g.Grant.GrantID], last, lastErr, u.wantVoid)
	}
}

func TestINV03NoDispatchWhenAReadFailsOrThePlanIsNotCallable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		checkINV03Reads(t, rapid.SampledFrom(unsafeReads).Draw(t, "case"), rapid.SampledFrom(Modes()).Draw(t, "fault"), rapid.IntRange(1, 3).Draw(t, "presentations"))
	})
	for _, u := range unsafeReads {
		for _, fault := range Modes() {
			for _, times := range []int{1, 3} {
				checkINV03Reads(t, u, fault, times)
			}
		}
	}
}

// Ruling A153: the database refuses the spend of a key whose dispatch budget is used up. Nothing is spent, nothing is dispatched, no receipt exists, the grant is not
// voided and the controller says so, however often the grant is presented.
func TestINV03NoDispatchWhenTheDispatchBudgetIsSpent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		w := NewWorld(secret)
		fault := rapid.SampledFrom(Modes()).Draw(t, "fault")
		times := rapid.IntRange(1, 4).Draw(t, "presentations")
		w.SetFaults(func() FaultMode { return fault })
		g := fresh(w, w.Clock.Now())
		w.Store(g)
		for i := 0; i < times; i++ {
			c := controllerFor(w, nil)
			c.Ledger = budgetSpent{w}
			ctx, cancel := deadline()
			out, err := c.Execute(ctx, TenantID, g.Grant.GrantID)
			cancel()
			if err != nil || !out.BudgetSpent || out.Receipt != nil || out.Void != "" || out.Replayed {
				t.Fatalf("presentation %d: %+v, %v; want BudgetSpent and nothing else", i, out, err)
			}
		}
		s := w.Snapshot()
		if len(s.Dispatches) != 0 || len(s.Spent) != 0 || len(s.Receipts) != 0 || len(s.Voids) != 0 {
			t.Fatalf("a spent dispatch budget still reached the target: %d dispatches, %d spent, %d receipts, %d voids", len(s.Dispatches), len(s.Spent), len(s.Receipts), len(s.Voids))
		}
	})
}
