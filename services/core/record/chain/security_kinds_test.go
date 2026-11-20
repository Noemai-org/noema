package chain_test

// REQ-EXE-008, REQ-AUD-001, INV-10, PERD 32.2 (ruling A114): the writer accepts exactly the SECURITY_EVENT kinds the contract lists (record-event.v3 SecurityKind), among
// them KILL_SWITCH_CLEARED and KILL_SWITCH_CLEAR_REFUSED, and the chain verifies with them. The two are kinds, never event types. Real PostgreSQL; all values SYNTHETIC.

import (
	"fmt"
	"testing"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

func TestEveryKindOfTheContractIsAcceptedByTheWriterAndTheChainVerifies(t *testing.T) {
	kinds := loadSchema(t).Defs["SecurityKind"].Enum
	for _, named := range []string{"KILL_SWITCH_CLEARED", "KILL_SWITCH_CLEAR_REFUSED"} { // PERD 32.2 names these two; the contract must list them
		found := false
		for _, k := range kinds {
			found = found || k == named
		}
		if !found {
			t.Errorf("record-event.v3 SecurityKind does not list %s, which PERD 32.2 names", named)
			kinds = append(kinds, named) // the writer is still held to it
		}
	}
	if len(kinds) < 7 {
		t.Fatalf("the schema lists %d security kinds: the scan does not see them", len(kinds))
	}
	w := newWorld(t, "ten_aa")
	accepted := 0
	for i, k := range kinds {
		tx, err := w.pool("ten_aa").Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.app.Append(ctx(t), tx, chain.Event{TenantID: "ten_aa", CorrelationID: "corr_abc123", WrittenBy: "core-execution", Actor: "core-execution",
			EventType: "SECURITY_EVENT", Kind: k, ObjectRef: fmt.Sprintf("ten_%04d", i+1), Payload: map[string]any{}})
		if err != nil {
			_ = tx.Rollback(ctx(t))
			t.Errorf("the writer refuses the security kind %s: %v", k, err)
			continue
		}
		if err := tx.Commit(ctx(t)); err != nil {
			t.Fatal(err)
		}
		accepted++
	}
	if n, err := chain.Verify(ctx(t), w.admin, "ten_aa"); err != nil || n != int64(accepted) {
		t.Fatalf("verify = %d, %v; want %d events", n, err, accepted)
	}
}

// The two kill-switch words are kinds of SECURITY_EVENT only: as event types the core refuses them (PERD 32.2 lists neither as an event type).
func TestTheKillSwitchKindsAreNotEventTypes(t *testing.T) {
	w := newWorld(t, "ten_aa")
	for _, k := range []string{"KILL_SWITCH_CLEARED", "KILL_SWITCH_CLEAR_REFUSED"} {
		tx, err := w.pool("ten_aa").Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.app.Append(ctx(t), tx, chain.Event{TenantID: "ten_aa", CorrelationID: "corr_abc123", WrittenBy: "core-execution", Actor: "core-execution",
			EventType: k, ObjectRef: "ten_0001", Payload: map[string]any{}}); err == nil {
			t.Errorf("%s was appended as an event type", k)
		}
		_ = tx.Rollback(ctx(t))
	}
}
