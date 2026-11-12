package controller_test

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// S11 and the receipt contract (execution-receipt.v1: logical_action is a pack ID; definition of done 3c, A131: removing either change was caught by no test): the two
// revalidation events carry the policy version the grant was issued under, and the receipt of a write names the capability of the grant as its logical action.
func TestRevalidationEventsCarryTheGrantsPolicyVersionAndTheReceiptNamesTheCapability(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutate   func(*World)
		wantType string
	}{
		{"revalidation passes", func(*World) {}, "REVALIDATION_PASSED"},
		{"revalidation fails on a changed fact", func(w *World) { w.Mutate(func(w *World) { w.RevalChanged = []string{"ownership"} }) }, "REVALIDATION_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, c, g := setup(t)
			var seen []controller.Event
			w.FailAppend = func(e controller.Event) error { seen = append(seen, e); return nil } // an observer: it never refuses
			tc.mutate(w)
			if _, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, e := range seen {
				if e.Type != tc.wantType {
					continue
				}
				found = true
				if e.PolicyVersion == "" || e.PolicyVersion != g.Grant.PolicyVersion {
					t.Errorf("%s: policy version %q, want the grant's %q", e.Type, e.PolicyVersion, g.Grant.PolicyVersion)
				}
			}
			if !found {
				t.Fatalf("no %s event among %v", tc.wantType, seen)
			}
			if tc.wantType != "REVALIDATION_PASSED" {
				return
			}
			receipts := w.Snapshot().Receipts
			if len(receipts) != 1 {
				t.Fatalf("%d receipts, want 1", len(receipts))
			}
			for _, r := range receipts {
				if r.LogicalAction != g.Grant.CapabilityID || r.LogicalAction == "" {
					t.Errorf("receipt logical action %q, want the grant's capability %q", r.LogicalAction, g.Grant.CapabilityID)
				}
			}
		})
	}
}
