package controller_test

import (
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// TST-01 (REQ-TST-002): the invariants of the Execution Controller as property tests, one statement each, in the words of the
// invariant-to-test map (PERD 45.2). rapid draws the defect, the network fault, the number of presentations and the crash point;
// the world is the deterministic simulation of D7 (everything SYNTHETIC). They run with `go test`, so CI runs them on every merge.
// INV-05 and INV-18 are proved where their code is built (EXE-01 and PLN-01).

// A defect a grant can have. prepare builds the grant and may break it; world changes the world before the grant is presented.
type defect struct {
	name     string
	valid    bool // the grant is valid, so a dispatch is allowed
	unstored bool // the grant is never stored, so the ledger cannot find it
	prepare  func(w *World, now time.Time) controller.SignedGrant
	world    func(w *World)
}

func fresh(w *World, now time.Time) controller.SignedGrant {
	return w.IssueGrant(1, now.Add(-time.Minute), now.Add(10*time.Minute))
}

// resigned breaks a field of a grant that is then correctly signed again: the signature is valid and the grant still does not match
// the call.
func resigned(edit func(g *controller.Grant)) func(*World, time.Time) controller.SignedGrant {
	return func(w *World, now time.Time) controller.SignedGrant {
		g := fresh(w, now)
		edit(&g.Grant)
		return w.Resign(g)
	}
}

var defects = []defect{
	{name: "none", valid: true, prepare: fresh},
	{name: "a field changed after signing", prepare: func(w *World, now time.Time) controller.SignedGrant {
		g := fresh(w, now)
		g.Grant.PlanStep = 2
		return g
	}},
	{name: "a signature of zeros", prepare: func(w *World, now time.Time) controller.SignedGrant {
		g := fresh(w, now)
		g.Sig.Value = strings.Repeat("0", len(g.Sig.Value))
		return g
	}},
	{name: "a key the ledger does not know", prepare: func(w *World, now time.Time) controller.SignedGrant {
		g := fresh(w, now)
		g.Sig.KeyID = "sim-key-9"
		return g
	}},
	{name: "expired", prepare: func(w *World, now time.Time) controller.SignedGrant {
		return w.IssueGrant(1, now.Add(-20*time.Minute), now.Add(-10*time.Minute))
	}},
	{name: "not yet valid", prepare: func(w *World, now time.Time) controller.SignedGrant {
		return w.IssueGrant(1, now.Add(5*time.Minute), now.Add(15*time.Minute))
	}},
	{name: "parameters that do not match, validly signed", prepare: resigned(func(g *controller.Grant) { g.ParamsHash = hashOf("other-params") })},
	{name: "another tenant's grant, validly signed", prepare: resigned(func(g *controller.Grant) { g.TenantID = "ten_other" })},
	{name: "a capability that does not match, validly signed", prepare: resigned(func(g *controller.Grant) { g.CapabilityID = "sim.cap.other" })},
	{name: "a plan that does not match, validly signed", prepare: resigned(func(g *controller.Grant) { g.PlanHash = hashOf("plan-2") })},
	{name: "a grant the ledger never stored", unstored: true, prepare: fresh},
	{name: "the kill switch on", prepare: fresh, world: func(w *World) { w.KillSwitch = true }},
	{name: "the decision no longer ALLOW", prepare: fresh, world: func(w *World) { w.DecisionAllowed = false }},
	{name: "the intent superseded", prepare: fresh, world: func(w *World) { w.IntentHash = hashOf("intent-2") }},
	{name: "the target version moved", prepare: fresh, world: func(w *World) { w.TargetVersion = "v_2" }},
}

func present(w *World, grantID string, times int) {
	for i := 0; i < times; i++ {
		ctx, cancel := deadline()
		_, _ = controllerFor(w, nil).Execute(ctx, TenantID, grantID)
		cancel()
	}
}

// INV-03: no dispatch without a valid, unexpired, unused, matching grant. Whatever is wrong with the grant, nothing reaches the
// target; and a grant that is right reaches it once, however often it is presented and whatever the network does.
//
// "Matching" means the plan, the capability, the parameters and the tenant. The target is not a separate defect: the plan has no
// target field, the grant's target_ref is under the signature, and the dispatch is built from that same grant, so a validly signed
// grant for another target is simply another grant. A forged or altered target is the "field changed after signing" defect.
func TestINV03NoDispatchWithoutAValidUnexpiredUnusedMatchingGrant(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := rapid.SampledFrom(defects).Draw(t, "defect")
		fault := rapid.SampledFrom(Modes()).Draw(t, "fault")
		times := rapid.IntRange(1, 4).Draw(t, "presentations")
		w := NewWorld(secret)
		w.SetFaults(func() FaultMode { return fault })
		g := d.prepare(w, w.Clock.Now())
		if !d.unstored {
			w.Store(g)
		}
		if d.world != nil {
			d.world(w)
		}
		present(w, g.Grant.GrantID, times)
		s := w.Snapshot()
		if !d.valid {
			if len(s.Dispatches) != 0 || len(s.Spent) != 0 {
				t.Fatalf("%q: %d dispatches and %d spent grants; a grant with this defect must reach nothing", d.name, len(s.Dispatches), len(s.Spent))
			}
			return
		}
		if len(s.Dispatches) != 1 {
			t.Fatalf("a valid grant presented %d times under fault %d was dispatched %d times, want exactly 1", times, fault, len(s.Dispatches))
		}
		if id := s.Dispatches[0].GrantID; !s.Spent[id] {
			t.Fatalf("grant %s was dispatched without being spent", id)
		}
	})
}

// INV-07: a timeout, a 5xx or silence never maps to success or failure. Under every network fault the receipt says what was seen,
// and anything that is not a clear answer is OUTCOME_UNKNOWN. After a crash the sweeper writes OUTCOME_UNKNOWN too.
func TestINV07ATimeoutAFiveXXOrSilenceNeverMapsToSuccessOrFailure(t *testing.T) {
	want := map[FaultMode]controller.ExecutionState{
		FaultNone: controller.StateAcknowledged, FaultRejected: controller.StateRejectedByTarget,
		FaultLostResponse: controller.StateOutcomeUnknown, FaultDroppedRequest: controller.StateOutcomeUnknown,
		FaultServerErrBefore: controller.StateOutcomeUnknown, FaultServerErrAfter: controller.StateOutcomeUnknown, FaultConnReset: controller.StateOutcomeUnknown,
	}
	rapid.Check(t, func(t *rapid.T) {
		fault := rapid.SampledFrom(Modes()).Draw(t, "fault")
		w := NewWorld(secret)
		w.SetFaults(func() FaultMode { return fault })
		g := fresh(w, w.Clock.Now())
		w.Store(g)
		present(w, g.Grant.GrantID, 1)
		s := w.Snapshot()
		if len(s.Receipts) != 1 {
			t.Fatalf("fault %d: %d receipts, want exactly 1", fault, len(s.Receipts))
		}
		for _, r := range s.Receipts {
			if r.State != want[fault] {
				t.Fatalf("fault %d gave state %s, want %s", fault, r.State, want[fault])
			}
			if r.State == "SUCCEEDED" || r.State == "FAILED" {
				t.Fatalf("fault %d gave %s: a timeout, a 5xx or silence is never success or failure", fault, r.State)
			}
		}
	})
	rapid.Check(t, func(t *rapid.T) {
		point := rapid.SampledFrom(controller.Points()).Draw(t, "point")
		fault := rapid.SampledFrom(Modes()).Draw(t, "fault")
		for id, r := range RunCrash(point, fault).Final.Receipts {
			if r.TransportResult == "SWEEPER_NO_RECEIPT" && r.State != controller.StateOutcomeUnknown {
				t.Fatalf("crash at %s, fault %d: the sweeper wrote %s for %s, want OUTCOME_UNKNOWN", PointName(point), fault, r.State, id)
			}
			if r.State == "SUCCEEDED" || r.State == "FAILED" {
				t.Fatalf("crash at %s, fault %d: receipt %s is %s", PointName(point), fault, id, r.State)
			}
		}
	})
}

// INV-08: the key is committed before dispatch. For every dispatch, under every crash point and fault, the idempotency record and
// DISPATCH_ATTEMPTED were committed first, the key the target saw is the key computed from the call, and it is the same for every
// attempt of the logical action.
func TestINV08EveryWriteCarriesAKeyCommittedBeforeTheCall(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		point := rapid.SampledFrom(controller.Points()).Draw(t, "point")
		fault := rapid.SampledFrom(Modes()).Draw(t, "fault")
		r := RunCrash(point, fault)
		key := keyOf(r.Run.World)
		for _, d := range r.Final.Dispatches {
			if !d.RecordedFirst || d.Key == "" {
				t.Fatalf("crash at %s, fault %d: grant %s was dispatched before its key was committed (key %q)", PointName(point), fault, d.GrantID, d.Key)
			}
			if d.Key != key {
				t.Fatalf("crash at %s, fault %d: the target saw key %q, want %q", PointName(point), fault, d.Key, key)
			}
			committed := false
			for _, e := range r.Final.Record {
				if e.Type == "DISPATCH_ATTEMPTED" && e.GrantID == d.GrantID && e.Seq <= d.AtSeq {
					committed = true
				}
			}
			if !committed {
				t.Fatalf("crash at %s, fault %d: no DISPATCH_ATTEMPTED for %s precedes its dispatch", PointName(point), fault, d.GrantID)
			}
		}
	})
}
