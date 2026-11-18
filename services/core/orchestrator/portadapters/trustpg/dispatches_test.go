package trustpg_test

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

type readers map[string]reconstruct.TxBeginner

func (r readers) For(tenant string) (reconstruct.TxBeginner, error) {
	if b, ok := r[tenant]; ok {
		return b, nil
	}
	return nil, trustpg.ErrNoPool
}

// REQ-RCV-003, A153, AGENT-08: the used dispatches of a key are the DISPATCH_ATTEMPTED events of the Record for the tenant, correlation, capability and target; those of
// another target, correlation, capability or tenant do not count; an event whose key cannot be read counts against every key of its correlation (as the database does).
func TestTheUsedDispatchesOfAKeyAreCountedFromTheRecord(t *testing.T) {
	w := newWorld(t)
	k, err := trustpg.NewKeyDispatches(readers{tenantA: w.readerA, tenantB: w.readerB})
	if err != nil {
		t.Fatal(err)
	}
	key := intake.DispatchKey{TenantID: tenantA, CorrelationID: corr, CapabilityID: capability, TargetRef: target}
	count := func() int {
		n, err := k.Used(ctx(t), key)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(); n != 0 {
		t.Fatalf("no dispatch yet: %d", n)
	}
	w.dispatch(t, tenantA, corr, map[string]any{"capability": capability, "target_ref": target, "attempt": 1})
	w.dispatch(t, tenantA, corr, map[string]any{"capability": capability, "target_ref": "fin.transaction:OTHER"})
	w.dispatch(t, tenantA, "corr_OTHER1", map[string]any{"capability": capability, "target_ref": target})
	w.dispatch(t, tenantA, corr, map[string]any{"capability": "fin.cap.other", "target_ref": target})
	if n := count(); n != 1 {
		t.Fatalf("one dispatch of the key among others: %d, want 1", n)
	}
	w.dispatch(t, tenantA, corr, map[string]any{"capability": capability, "target_ref": target, "attempt": 2})
	if n := count(); n != 2 {
		t.Fatalf("two dispatches: %d, want 2", n)
	}
	w.dispatch(t, tenantA, corr, map[string]any{"note": "no key"})
	if n := count(); n != 3 {
		t.Fatalf("an event whose key cannot be read: %d, want 3 (it counts against every key of its correlation)", n)
	}
}

// REQ-SEC-004, INV-12: a key of another tenant is counted as the key's own tenant reads it, never as the caller's.
func TestAnotherTenantsDispatchesDoNotCount(t *testing.T) {
	w := newWorld(t)
	k, err := trustpg.NewKeyDispatches(readers{tenantA: w.readerA, tenantB: w.readerB})
	if err != nil {
		t.Fatal(err)
	}
	w.dispatch(t, tenantA, corr, map[string]any{"capability": capability, "target_ref": target})
	n, err := k.Used(ctx(t), intake.DispatchKey{TenantID: tenantB, CorrelationID: corr, CapabilityID: capability, TargetRef: target})
	if err != nil || n != 0 {
		t.Fatalf("tenant B: %d, %v; want 0", n, err)
	}
}

// Fail closed: a key without a tenant, correlation, capability or target is an error, and a tenant with no reader is an error, never a zero.
func TestAnIncompleteKeyOrAnUnknownTenantIsAnErrorNeverZero(t *testing.T) {
	w := newWorld(t)
	k, err := trustpg.NewKeyDispatches(readers{tenantA: w.readerA})
	if err != nil {
		t.Fatal(err)
	}
	for name, key := range map[string]intake.DispatchKey{
		"no tenant":      {CorrelationID: corr, CapabilityID: capability, TargetRef: target},
		"no correlation": {TenantID: tenantA, CapabilityID: capability, TargetRef: target},
		"no capability":  {TenantID: tenantA, CorrelationID: corr, TargetRef: target},
		"no target":      {TenantID: tenantA, CorrelationID: corr, CapabilityID: capability},
		"unknown tenant": {TenantID: tenantB, CorrelationID: corr, CapabilityID: capability, TargetRef: target},
	} {
		if n, err := k.Used(ctx(t), key); err == nil {
			t.Errorf("%s: %d and no error", name, n)
		}
	}
}
