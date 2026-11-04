package verifier_test

import (
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

// REQ-VER-004, INV-28: a pending condition that holds gives PENDING_EXTERNAL with a watch that has a deadline; one that
// does not gives the plain verdict; a watch that cannot be kept is refused.
func TestPendingExternalHasAWatchWithADeadline(t *testing.T) {
	pend := func(w *world) {
		closed := theCase()
		closed.Status = "CLOSED"
		w.read.byKey, w.read.byTx = []step{complete(closed)}, []step{complete(closed)}
		now := w.clk.Now()
		w.req.Definition.Pending = &verifier.Pending{Predicate: "exists(case where case.status == 'CLOSED')", Triggers: []string{"fin.evt.settlement.updated"},
			NextCheckAt: now.Add(5 * time.Minute), DeadlineAt: now.Add(72 * time.Hour)}
	}
	w := newWorld(t, pend)
	res, wr := w.decided(t)
	want(t, res, verifier.PendingExternal, verifier.ReasonPendingCondition)
	if wr.Watch == nil || wr.Watch.WatchID == "" || !wr.Watch.DeadlineAt.After(wr.Watch.NextCheckAt) || wr.RecoveryRequestID == "" {
		t.Fatalf("PENDING_EXTERNAL without a usable watch or recovery request: %+v", wr)
	}
	// P2 (Codex): the pending condition is about the case under our key, as the success predicate is. Another case that
	// matches it must not hold a watch open for this receipt.
	t.Run("another case matches the pending condition but the key's case does not", func(t *testing.T) {
		ours := theCase()
		ours.CreatedByIntent = "int_other"
		other := theCase()
		other.CaseID, other.IdempotencyKey, other.Status, other.CreatedByIntent = "case_2", "idk_v1_other", "CLOSED", "int_other"
		w := newWorld(t, pend, func(w *world) { w.read.byKey, w.read.byTx = []step{complete(ours)}, []step{complete(ours, other)} })
		res, wr := w.decided(t)
		want(t, res, verifier.NotAchieved, verifier.ReasonPredicateFalse)
		if wr.Watch != nil {
			t.Fatal("a watch was stored for an effect that is not this receipt's")
		}
	})
	// Codex: times are stored to the microsecond. A time less than one microsecond ahead truncates to now, which is not ahead.
	for name, mut := range map[string]func(p *verifier.Pending, now time.Time){
		"a first check less than a microsecond ahead": func(p *verifier.Pending, now time.Time) { p.NextCheckAt = now.Add(500 * time.Nanosecond) },
		"a deadline less than a microsecond ahead": func(p *verifier.Pending, now time.Time) {
			p.NextCheckAt, p.DeadlineAt = now.Add(500*time.Nanosecond), now.Add(500*time.Nanosecond)
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, pend)
			mut(w.req.Definition.Pending, w.clk.Now())
			res, wr := w.decided(t)
			want(t, res, verifier.Unknown, verifier.ReasonWatchUnusable)
			if wr.Watch != nil {
				t.Fatal("a watch that is due at the instant it is stored was accepted")
			}
		})
	}
	for name, mut := range map[string]func(*verifier.Pending){
		"a deadline already past":      func(p *verifier.Pending) { p.DeadlineAt = t0 },
		"a first check after deadline": func(p *verifier.Pending) { p.NextCheckAt = p.DeadlineAt.Add(time.Second) },
		"a first check in the past":    func(p *verifier.Pending) { p.NextCheckAt = t0 },
		"no deadline":                  func(p *verifier.Pending) { p.DeadlineAt = time.Time{} },
		// outcome-verdict.v1: watch.triggers are unique PackIds. A value the contract refuses must not be signed.
		"a duplicate trigger":         func(p *verifier.Pending) { p.Triggers = []string{"fin.evt.a", "fin.evt.a"} },
		"a trigger that is no PackId": func(p *verifier.Pending) { p.Triggers = []string{"Not A Pack Id"} },
		"an empty trigger":            func(p *verifier.Pending) { p.Triggers = []string{""} },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, pend, func(w *world) { mut(w.req.Definition.Pending) })
			res, wr := w.decided(t)
			want(t, res, verifier.Unknown, verifier.ReasonWatchUnusable)
			if wr.Watch != nil {
				t.Fatal("a watch without a usable deadline was stored")
			}
		})
	}
}
