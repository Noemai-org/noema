package controller_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

func setup(t *testing.T) (*World, *controller.Controller, controller.SignedGrant) {
	t.Helper()
	w := NewWorld([]byte("synthetic-simulation-key-not-a-secret"))
	now := w.Clock.Now()
	g := w.IssueGrant(1, now.Add(-time.Minute), now.Add(10*time.Minute))
	w.Store(g)
	c := &controller.Controller{Clock: w.Clock, Keys: w.Keys, ReceiptKeyID: KeyID, Grants: w, Bind: w, Reval: w, Ledger: w, Out: w, IDs: w}
	return w, c, g
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

// PERD 27.1: a clean run goes through every step, in order, and ends with a signed receipt that never says succeeded.
func TestACleanRunWritesOneEffectAndOneReceipt(t *testing.T) {
	w, c, g := setup(t)
	out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err != nil || out.Receipt == nil || out.Receipt.State != controller.StateAcknowledged || out.Receipt.ExternalAck == nil {
		t.Fatalf("%+v %v", out, err)
	}
	s := w.Snapshot()
	var types []string
	for _, e := range s.Record {
		types = append(types, e.Type)
	}
	want := []string{"REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT"}
	if len(types) != len(want) {
		t.Fatalf("record events %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("record events %v, want %v", types, want)
		}
	}
	for _, n := range s.Effects {
		if n != 1 {
			t.Fatalf("effects %v", s.Effects)
		}
	}
	if out.Receipt.Sig.Value == "" {
		t.Fatal("the receipt must be signed")
	}
}

// REQ-EXE-001: each void reason of E1 to E7, with no dispatch and a RecoveryRequest.
func TestEachCheckVoidsTheGrantWithItsReason(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(w *World, g *controller.SignedGrant)
		want   controller.VoidReason
	}{
		{"E1 tampered", func(w *World, g *controller.SignedGrant) { g.Grant.PlanStep = 9; w.Store(*g) }, controller.VoidSignatureInvalid},
		{"E1 unknown key", func(w *World, g *controller.SignedGrant) { g.Sig.KeyID = "retired"; w.Store(*g) }, controller.VoidSignatureInvalid},
		{"E2 expired", func(w *World, _ *controller.SignedGrant) { w.Clock.Advance(11 * time.Minute) }, controller.VoidExpired},
		{"E2 not yet valid", func(w *World, g *controller.SignedGrant) {
			*g = w.IssueGrant(2, w.Clock.Now().Add(time.Minute), w.Clock.Now().Add(5*time.Minute))
			w.Store(*g)
		}, controller.VoidExpired},
		{"E3 intent superseded", func(w *World, _ *controller.SignedGrant) {
			w.Mutate(func(w *World) {
				w.IntentHash = "sha256:" + string(make([]byte, 0)) + "0000000000000000000000000000000000000000000000000000000000000000"
			})
		}, controller.VoidIntentSuperseded},
		{"E4 policy superseded", func(w *World, _ *controller.SignedGrant) {
			w.Mutate(func(w *World) { w.DecisionAllowed = false })
		}, controller.VoidPolicySuperseded},
		{"E4 the grant's policy has a newer active version", func(w *World, _ *controller.SignedGrant) {
			w.Mutate(func(w *World) { w.ActivePolicies["sim.pol"] = "1.1" })
		}, controller.VoidPolicySuperseded},
		{"E4 the grant's policy has an older active version (rolled back)", func(w *World, _ *controller.SignedGrant) {
			w.Mutate(func(w *World) { w.ActivePolicies["sim.pol"] = "0.9" })
		}, controller.VoidPolicySuperseded},
		{"E4 the active version only extends the grant's version as text (1.0.1 against 1.0)", func(w *World, _ *controller.SignedGrant) {
			w.Mutate(func(w *World) { w.ActivePolicies["sim.pol"] = "1.0.1" })
		}, controller.VoidPolicySuperseded},
		{"E4 the grant's policy has no active version (retired)", func(w *World, _ *controller.SignedGrant) {
			w.Mutate(func(w *World) { delete(w.ActivePolicies, "sim.pol") })
		}, controller.VoidPolicySuperseded},
		{"E6 kill switch", func(w *World, _ *controller.SignedGrant) { w.Mutate(func(w *World) { w.KillSwitch = true }) }, controller.VoidWritesDisabled},
		{"E7 target version moved", func(w *World, _ *controller.SignedGrant) {
			w.Mutate(func(w *World) { w.TargetVersion = "v_2" })
		}, controller.VoidStateChanged},
		{"E7 ownership changed", func(w *World, _ *controller.SignedGrant) {
			w.Mutate(func(w *World) { w.RevalChanged = []string{"ownership"} })
		}, controller.VoidStateChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, c, g := setup(t)
			tc.mutate(w, &g)
			out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
			if err != nil || out.Void != tc.want {
				t.Fatalf("void %q err %v, want %q", out.Void, err, tc.want)
			}
			s := w.Snapshot()
			if len(s.Dispatches) != 0 || len(s.Effects) != 0 || len(s.Spent) != 0 {
				t.Fatalf("a void grant must not spend or dispatch: %+v", s)
			}
			if len(w.Recovery) != 1 {
				t.Fatalf("a void grant raises one RecoveryRequest: %v", w.Recovery)
			}
			again, _ := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
			if again.Receipt != nil || len(w.Snapshot().Dispatches) != 0 {
				t.Fatalf("a void grant never dispatches later: %+v", again)
			}
		})
	}
}

// REQ-EXE-001: 100 concurrent spends of one grant produce exactly one dispatch (real goroutines, run with -race).
func TestHundredConcurrentSpendsProduceOneDispatch(t *testing.T) {
	w, c, g := setup(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	replays, receipts := 0, 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Errorf("%v", err)
			}
			if out.Replayed {
				replays++
			}
			if out.Receipt != nil {
				receipts++
			}
		}()
	}
	wg.Wait()
	s := w.Snapshot()
	if len(s.Dispatches) != 1 || receipts != 1 || replays != 99 {
		t.Fatalf("dispatches %d receipts %d replays %d", len(s.Dispatches), receipts, replays)
	}
}

// REQ-EXE-008: the switch is read again inside the spend, so flipping it between E6 and E8 still stops the write.
func TestTheKillSwitchIsReadAgainInsideTheSpend(t *testing.T) {
	w, c, g := setup(t)
	c.Boundary = func(p controller.Point) error {
		if p == controller.PointAfterE7 {
			w.Mutate(func(w *World) { w.KillSwitch = true })
		}
		return nil
	}
	out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err != nil || out.Void != controller.VoidWritesDisabled || len(w.Snapshot().Dispatches) != 0 {
		t.Fatalf("%+v %v", out, err)
	}
}

// Fail closed: a context with no deadline is refused before anything is read.
func TestAContextWithoutADeadlineIsRefused(t *testing.T) {
	_, c, g := setup(t)
	if _, err := c.Execute(context.Background(), TenantID, g.Grant.GrantID); err == nil {
		t.Fatal("must refuse")
	}
}

// An entropy failure while a receipt ID is minted (INV-07, INV-03): the controller refuses before the grant is spent and
// before anything is dispatched. It neither panics nor leaves a half-written run. The grant stays unspent, so a later
// presentation after the entropy source recovers can still run.
func TestAFailedIDSourceRefusesBeforeSpendAndDispatch(t *testing.T) {
	w, c, g := setup(t)
	w.IDErr = errors.New("injected: the entropy source is unavailable")
	out, err := func() (out controller.Outcome, err error) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("the controller panicked on an entropy failure: %v", r)
			}
		}()
		return c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	}()
	if err == nil || out.Receipt != nil || out.Void != "" {
		t.Fatalf("outcome %+v, error %v; want a refusal with an error", out, err)
	}
	if !errors.Is(err, controller.ErrIDSource) {
		t.Fatalf("error %v is not ErrIDSource", err)
	}
	s := w.Snapshot()
	for _, e := range s.Record {
		if e.Type == "GRANT_SPENT" || e.Type == "DISPATCH_ATTEMPTED" || e.Type == "RECEIPT" {
			t.Fatalf("record event %s written although no receipt ID could be minted", e.Type)
		}
	}
	for _, n := range s.Effects {
		if n != 0 {
			t.Fatalf("effects %v: a write was dispatched", s.Effects)
		}
	}
	w.IDErr = nil
	out, err = c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err != nil || out.Receipt == nil {
		t.Fatalf("after the source recovered, the unspent grant did not run: %+v %v", out, err)
	}
}

// Ruling of 5 Nov 2026: a grant binds the exact policy_id and version it was decided under. Another policy's version, however high, is
// no business of this grant: versions are compared only within the grant's own policy_id, never across policy IDs.
func TestAnotherPolicysVersionNeverVoidsAGrant(t *testing.T) {
	w, c, g := setup(t)
	w.Mutate(func(w *World) { w.ActivePolicies["sim.other"] = "99.0" })
	out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err != nil || out.Void != "" {
		t.Fatalf("a different policy's version voided the grant: %+v %v", out, err)
	}
}

// A grant whose policy_version is not policy_id@version cannot be compared and is refused (fail closed), with a recovery request.
func TestAGrantWithAMalformedPolicyVersionIsVoided(t *testing.T) {
	for _, bad := range []string{"", "sim.pol", "@1.0", "sim.pol@", "sim.pol@1.0@2"} {
		w, c, _ := setup(t)
		g := w.IssueGrantWithPolicy(1, w.Clock.Now().Add(-time.Minute), w.Clock.Now().Add(5*time.Minute), bad)
		w.Store(g)
		out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
		if err != nil || out.Void != controller.VoidPolicySuperseded {
			t.Errorf("%q: void %q err %v", bad, out.Void, err)
		}
	}
}

// Codex P2 on noema#163: a policy_id@version that is malformed in the same way on both sides (the grant and the binder) is still not
// comparable. The grant contract requires a lower-case dotted policy ID and numeric dotted version components; anything else fails closed.
func TestAMalformedPolicyVersionThatMatchesTheBindersIsStillVoided(t *testing.T) {
	for _, bad := range []struct{ id, ver string }{
		{"sim.pol", "not-a-version"}, {"sim.pol", "1"}, {"sim.pol", "1.x"}, {"sim.pol", "1.0.0.0"}, {"sim.pol", "1.0-rc1"}, {"sim.pol", " 1.0"},
		{"Sim.Pol", "1.0"}, {"sim..pol", "1.0"}, {"sim.pol.", "1.0"}, {"1sim", "1.0"}, {"sim pol", "1.0"},
	} {
		w, c, _ := setup(t)
		w.Mutate(func(w *World) { w.ActivePolicies[bad.id] = bad.ver })
		g := w.IssueGrantWithPolicy(1, w.Clock.Now().Add(-time.Minute), w.Clock.Now().Add(5*time.Minute), bad.id+"@"+bad.ver)
		w.Store(g)
		out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
		if err != nil || out.Void != controller.VoidPolicySuperseded || len(w.Snapshot().Dispatches) != 0 {
			t.Errorf("%s@%s: void %q err %v dispatches %d; want a void with no dispatch", bad.id, bad.ver, out.Void, err, len(w.Snapshot().Dispatches))
		}
	}
	// and the well-formed shapes the contract allows still run
	for _, good := range []string{"1.0", "3.10", "1.2.3", "10.0.1"} {
		w, c, _ := setup(t)
		w.Mutate(func(w *World) { w.ActivePolicies["sim.pol"] = good })
		g := w.IssueGrantWithPolicy(1, w.Clock.Now().Add(-time.Minute), w.Clock.Now().Add(5*time.Minute), "sim.pol@"+good)
		w.Store(g)
		if out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID); err != nil || out.Void != "" {
			t.Errorf("sim.pol@%s: void %q err %v; a well-formed matching version must run", good, out.Void, err)
		}
	}
}
