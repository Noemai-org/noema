package controller_test

import (
	"math/rand/v2"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// REQ-EXE-005, REQ-EXE-007: the table of E10, and a property over generated transport results.
func TestClassifyTable(t *testing.T) {
	cases := []struct {
		in   controller.TransportResult
		want controller.ExecutionState
	}{
		{controller.TransportResult{Class: controller.TransportAck2xx, ExternalAck: "CMP-1"}, controller.StateAcknowledged},
		{controller.TransportResult{Class: controller.TransportAck2xx}, controller.StateOutcomeUnknown},
		{controller.TransportResult{Class: controller.TransportDuplicate409, ExternalAck: "CMP-1"}, controller.StateAcknowledged},
		{controller.TransportResult{Class: controller.TransportVersionConflict}, controller.StateRejectedStaleVersion},
		{controller.TransportResult{Class: controller.TransportRejected4xx}, controller.StateRejectedByTarget},
		// a create refused because the subject already has a case is a business rejection, not a stale version (D6)
		{controller.TransportResult{Class: controller.TransportDuplicateCreate, RawCode: "412:PRECONDITION_FAILED"}, controller.StateRejectedByTarget},
		{controller.TransportResult{Class: controller.TransportDefect}, controller.StateFailedTerminal},
		{controller.TransportResult{Class: controller.TransportTimeout}, controller.StateOutcomeUnknown},
		{controller.TransportResult{Class: controller.TransportConnectionReset}, controller.StateOutcomeUnknown},
		{controller.TransportResult{Class: controller.TransportServer5xx}, controller.StateOutcomeUnknown},
		{controller.TransportResult{Class: controller.TransportNoResponse}, controller.StateOutcomeUnknown},
		{controller.TransportResult{Class: "SOMETHING_NEW"}, controller.StateOutcomeUnknown},
		{controller.TransportResult{}, controller.StateOutcomeUnknown},
	}
	for _, c := range cases {
		if got := controller.Classify(c.in); got != c.want {
			t.Errorf("%+v: %s, want %s", c.in, got, c.want)
		}
	}
}

func TestClassifyProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	classes := []controller.TransportClass{controller.TransportAck2xx, controller.TransportVersionConflict, controller.TransportDuplicate409,
		controller.TransportDuplicateCreate, controller.TransportRejected4xx, controller.TransportTimeout, controller.TransportConnectionReset, controller.TransportServer5xx,
		controller.TransportNoResponse, controller.TransportDefect, "X", ""}
	silence := map[controller.TransportClass]bool{controller.TransportTimeout: true, controller.TransportConnectionReset: true,
		controller.TransportServer5xx: true, controller.TransportNoResponse: true}
	valid := map[controller.ExecutionState]bool{controller.StateAcknowledged: true, controller.StateOutcomeUnknown: true, controller.StateRejectedByTarget: true,
		controller.StateRejectedStaleVersion: true, controller.StateFailedTerminal: true}
	for i := 0; i < 20000; i++ {
		r := controller.TransportResult{Class: classes[rng.IntN(len(classes))], RawCode: []string{"", "200", "500"}[rng.IntN(3)]}
		if rng.IntN(2) == 0 {
			r.ExternalAck = "CMP-9"
		}
		got := controller.Classify(r)
		if !valid[got] {
			t.Fatalf("%+v gave %q, which Execution may not write after a dispatch", r, got)
		}
		if silence[r.Class] && got != controller.StateOutcomeUnknown {
			t.Fatalf("silence %+v must be OUTCOME_UNKNOWN, got %s", r, got)
		}
		if got == controller.StateAcknowledged && r.ExternalAck == "" {
			t.Fatalf("an ack needs a reference: %+v", r)
		}
	}
}

// REQ-EXE-002: the key is stable, sensitive to each of the seven fields, and has no attempt input.
func TestIdempotencyKeyProperty(t *testing.T) {
	base := crypto.IdempotencyKeyFields{TenantID: "ten_a", PrincipalID: "usr_1", TransactionID: "tx_1", LogicalAction: "act", ReasonCode: "R", PolicyScope: "pol", LogicalResolutionEpoch: "0"}
	k := crypto.IdempotencyKey(base)
	if k != crypto.IdempotencyKey(base) {
		t.Fatal("the key must be stable")
	}
	edits := []func(f *crypto.IdempotencyKeyFields){
		func(f *crypto.IdempotencyKeyFields) { f.TenantID += "x" },
		func(f *crypto.IdempotencyKeyFields) { f.PrincipalID += "x" },
		func(f *crypto.IdempotencyKeyFields) { f.TransactionID += "x" },
		func(f *crypto.IdempotencyKeyFields) { f.LogicalAction += "x" },
		func(f *crypto.IdempotencyKeyFields) { f.ReasonCode += "x" },
		func(f *crypto.IdempotencyKeyFields) { f.PolicyScope += "x" },
		func(f *crypto.IdempotencyKeyFields) { f.LogicalResolutionEpoch = "1" },
	}
	seen := map[string]bool{k: true}
	for i, e := range edits {
		f := base
		e(&f)
		nk := crypto.IdempotencyKey(f)
		if seen[nk] {
			t.Errorf("field %d does not change the key", i)
		}
		seen[nk] = true
	}
	// length-prefixing: moving a character across a field boundary changes the key.
	a := base
	a.TenantID, a.PrincipalID = "ten_ab", "usr_1"
	b := base
	b.TenantID, b.PrincipalID = "ten_a", "busr_1"
	if crypto.IdempotencyKey(a) == crypto.IdempotencyKey(b) {
		t.Error("the encoding must be unambiguous")
	}
}
