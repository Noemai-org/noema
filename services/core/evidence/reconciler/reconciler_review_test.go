package reconciler_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/reconciler"
)

// REQ-CTX-005, INV-21, INV-23 (Security Reviewer's note 2 and EL's P2 on noema#663): the reconciliation id names what was classified. The classification also depends on the clock
// reading it was computed at, and on each item's read time, effective time, freshness limit and source, so each of them changes the id; the same inputs give the same id.
func TestTheReconciliationIDFollowsEveryInputOfTheClassification(t *testing.T) {
	base := classify(t, build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))).ReconciliationID
	if again := classify(t, build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))).ReconciliationID; again != base {
		t.Fatalf("the same inputs gave two ids: %s and %s", base, again)
	}
	cases := map[string]func(*world){
		"the clock one second later":       func(w *world) { w.clk.Advance(time.Second) },
		"the clock past the expiry":        func(w *world) { w.clk.Advance(10 * time.Minute) },
		"an earlier read time":             func(w *world) { w.items[0].ReadAt = w.items[0].ReadAt.Add(-time.Second) },
		"an earlier effective time":        func(w *world) { w.items[1].EffectiveAt = w.items[1].EffectiveAt.Add(-time.Second) },
		"a longer freshness limit":         func(w *world) { w.items[2].EffectiveFreshnessSeconds++ },
		"a shorter freshness limit":        func(w *world) { w.items[0].EffectiveFreshnessSeconds-- },
		"two sources swapped between rows": func(w *world) { w.items[1].Source, w.items[2].Source = w.items[2].Source, w.items[1].Source },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			w := build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))
			mutate(w)
			got, err := w.rec.Classify(ctx(t), tenant, w.items)
			if err != nil {
				t.Fatal(err)
			}
			if got.ReconciliationID == base {
				t.Errorf("%s gave the same reconciliation id as the base (%s)", name, base)
			}
		})
	}
}

// A read time after the clock's reading would push the item's expiry out and keep it fresh longer than its limit allows: it is refused whole, like any item that cannot be used. A read at
// exactly the reading is a read made now and is used.
func TestAReadTimeAfterTheClockIsRefusedAndOneAtTheClockIsUsed(t *testing.T) {
	w := build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))
	w.items[0].ReadAt = now.Add(time.Nanosecond)
	if _, err := w.rec.Classify(ctx(t), tenant, w.items); !errors.Is(err, reconciler.ErrItem) {
		t.Fatalf("a read after the clock: err %v, want ErrItem", err)
	}
	w = build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))
	w.items[0].ReadAt = now
	if _, err := w.rec.Classify(ctx(t), tenant, w.items); err != nil {
		t.Fatalf("a read at the clock was refused: %v", err)
	}
}
