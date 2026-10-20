package corpus

import (
	"errors"
	"math/rand"
	"strconv"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

// REQ-RES-002: a grant for a capability that is not allowed under a freeze cannot be constructed on a frozen target.
// Permission is the proof a grant constructor must take; it can only come from FreezeGate.Permit.
func TestFreezeGateProperty(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	for i := 0; i < 2000; i++ {
		var all, allowed []string
		for k := r.Intn(12); k >= 0; k-- {
			id := "cap." + strconv.Itoa(k)
			all = append(all, id)
			if r.Intn(3) == 0 {
				allowed = append(allowed, id)
			}
		}
		gate := reconcile.NewFreezeGate(allowed)
		ok := map[string]bool{}
		for _, a := range allowed {
			ok[a] = true
		}
		for _, id := range all {
			p, err := gate.Permit(true, id)
			if ok[id] != (err == nil) || p.Valid() != ok[id] {
				t.Fatalf("frozen target, %s: allowed=%v but got valid=%v err=%v", id, ok[id], p.Valid(), err)
			}
			if !ok[id] && !errors.Is(err, reconcile.ErrFrozen) {
				t.Fatalf("wrong error %v", err)
			}
			if p, err := gate.Permit(false, id); err != nil || !p.Valid() || p.Capability() != id {
				t.Fatalf("an unfrozen target must permit %s: %v", id, err)
			}
		}
	}
	var zero reconcile.Permission
	if zero.Valid() {
		t.Fatal("the zero Permission must be invalid")
	}
	if _, err := reconcile.NewFreezeGate(nil).Permit(false, ""); err == nil {
		t.Fatal("an empty capability must not be permitted")
	}
}
