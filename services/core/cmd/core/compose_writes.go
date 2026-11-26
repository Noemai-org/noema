package main

import (
	"errors"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// compose_writes.go builds the Writes input of lane 2 of the G1 final composition (STM-01 composition gap, A684 step 1; A543, A364, A551, interim, relayed, not yet committed).
// REQ-STM-001, REQ-TRU-004, REQ-NFR-002; INV-01, INV-03, INV-12, INV-14, INV-16; AGENT-13, AGENT-16.
//
// intake.Writes answers two questions for T3: does the request lead to a write (the pack's rule) and are writes enabled for the tenant (its kill switch). Both come from what the
// composition already holds:
//
//   - the rule is trustport.NewPackWriteRule over Pack.WriteWhens() of the SAME verified pack the other Trust inputs read (in.Pack, the one trustInputsIn carries): the predicate is
//     the pack's data and core names no node or slot (INV-14);
//   - the kill switch is trustport.NewKillSwitch over the per-tenant trust_rw logins (no tenant is served by another's login, INV-12), the pack's own reference and the bound
//     NOEMA_TRUST_DB_TIMEOUT_MS (no default).
//
// Fail closed. A part that is absent is a gap, never a default rule: no pack, a pack that is no usable Pack or holds no intent node, no served tenant, or no login. A pack none of
// whose nodes carries a write_when is not a gap: it answers "no write" for every node it holds (A693, A543, interim, relayed, not yet committed). A part that is present and invalid is an error with fixed text:
// a write_when that does not compile over its node's slots (the whole rule is refused, never a rule without that node), or a bound that is not positive. No error carries a
// predicate, a slot, a tenant or a path (AGENT-13).
func buildWritesInput(in trustInputsIn) (intake.Writes, error) {
	if in.Pack == nil || len(in.Tenants) == 0 || len(in.Pools) == 0 {
		return nil, nil
	}
	whens, err := in.Pack.WriteWhens()
	if errors.Is(err, trustpack.ErrConfig) {
		return nil, nil
	}
	if err != nil {
		return nil, inputsFail("the pack's write_when data cannot be read", err)
	}
	if len(whens) == 0 {
		return nil, nil
	}
	rule, err := trustport.NewPackWriteRule(whens)
	if err != nil {
		return nil, inputsFail("the pack's write rule is not usable", err)
	}
	if _, err := in.pools(); err != nil {
		return nil, err
	}
	sw, err := trustport.NewKillSwitch(trustport.PoolsOf(in.Pools), in.Pack.Ref(), in.Timeout)
	if err != nil {
		return nil, inputsFail("the tenant kill switch cannot be built", err)
	}
	w, err := trustport.NewWrites(sw, rule)
	if err != nil {
		return nil, inputsFail("the Writes adapter cannot be built", err)
	}
	return w, nil
}
