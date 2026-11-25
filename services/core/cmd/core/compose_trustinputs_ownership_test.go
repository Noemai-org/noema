package main

// STM-01 lane 2 (A537, A475, interim, relayed, not yet committed): the composition builds the Ownership adapter, one instance per served tenant (A320), from the pack's ownership data
// only (Pack.OwnershipKinds through NodesFromKinds; the guard TestTheCompositionBuildsOwnershipNodesOnlyFromThePack reads this file's source too). REQ-STM-001, REQ-TRU-002,
// REQ-TRU-003; INV-11, INV-12, INV-14, INV-19; AGENT-13, AGENT-16. The store is a real reader of the candidates contract; the faults are real (a failing reader, a tenant with no
// instance, an expired context). SYNTHETIC.

import (
	"errors"
	"slices"
	"strings"
	"testing"

	ownershipport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/ownership"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// tiOwnedNode is a node of the real pack whose trust profile says the principal must own a slot, and the kind of that slot, both read from the pack and never written here.
func tiOwnedNode(t *testing.T) (node, kind string) {
	t.Helper()
	kinds, err := tiPack(t).OwnershipKinds()
	if err != nil || len(kinds) == 0 {
		t.Fatalf("the real pack gives no ownership data: %v", err)
	}
	for n, k := range kinds {
		return n, k
	}
	return "", ""
}

func tiIntent(tenant, principal, node, target string) intake.AdmittedIntent {
	return intake.AdmittedIntent{TenantID: tenant, IntentID: "int_synth01", Version: 1, IntentNode: node, CorrelationID: "corr_synth01", PrincipalID: principal, TargetRef: target}
}

func tiHas(list []string, name string) bool { return slices.Contains(list, name) }

func TestOwnershipIsBuiltFromThePackAndAnswersOwnedAndNotOwned(t *testing.T) { // REQ-TRU-002, REQ-TRU-003; INV-12
	node, kind := tiOwnedNode(t)
	in := tiIn(t)
	store := &tiCandidates{rows: []tiOwned{{"ten_aa", "usr_alice", kind + ":TX0001"}, {"ten_aa", "usr_bob", kind + ":TX0002"}}}
	in.OwnershipReader = store
	got, err := buildTrustInputs(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ownership == nil || tiHas(got.Gaps, "Ownership") {
		t.Fatalf("Ownership is not built (gaps %v)", got.Gaps)
	}
	cases := map[string]struct {
		intent intake.AdmittedIntent
		want   bool
	}{
		"the principal's own resource":               {tiIntent("ten_aa", "usr_alice", node, kind+":TX0001"), true},
		"another principal's resource":               {tiIntent("ten_aa", "usr_alice", node, kind+":TX0002"), false},
		"a resource that does not exist":             {tiIntent("ten_aa", "usr_alice", node, kind+":TX9999"), false},
		"a target of another kind":                   {tiIntent("ten_aa", "usr_alice", node, "fin.account:AC0001"), false},
		"a node the pack gives no owned slot":        {tiIntent("ten_aa", "usr_alice", "fin.meta.out_of_scope", ""), true},
		"the same resource read as the other tenant": {tiIntent("ten_bb", "usr_alice", node, kind+":TX0001"), false},
	}
	for name, c := range cases {
		owned, err := got.Ownership.OwnsTarget(tiDeadline(t), c.intent.TenantID, c.intent.PrincipalID, c.intent)
		if err != nil || owned != c.want {
			t.Errorf("%s: got %v, %v; want %v with no error", name, owned, err, c.want)
		}
	}
}

// A tenant that is not served has no instance: the answer is an error, never "owned" and never "not owned", and nothing is read (INV-11, INV-19).
func TestOwnershipOfATenantThatIsNotServedIsAnErrorAndReadsNothing(t *testing.T) {
	node, kind := tiOwnedNode(t)
	in := tiIn(t)
	store := &tiCandidates{rows: []tiOwned{{"ten_zz", "usr_alice", kind + ":TX0001"}}}
	in.OwnershipReader = store
	got, err := buildTrustInputs(in)
	if err != nil || got.Ownership == nil {
		t.Fatalf("built %v, %v", got.Ownership, err)
	}
	owned, err := got.Ownership.OwnsTarget(tiDeadline(t), "ten_zz", "usr_alice", tiIntent("ten_zz", "usr_alice", node, kind+":TX0001"))
	if err == nil || owned {
		t.Fatalf("got %v, %v; want an error and not owned", owned, err)
	}
	if store.reads() != 0 {
		t.Errorf("%d reads for a tenant that is not served", store.reads())
	}
	if strings.Contains(err.Error(), "ten_zz") {
		t.Errorf("the error carries the tenant: %q", err)
	}
}

// A read that fails is an error and never "not owned" (INV-19), through the composed adapter.
func TestOwnershipWithAFailingReadIsAnErrorNeverNotOwned(t *testing.T) {
	node, kind := tiOwnedNode(t)
	in := tiIn(t)
	failure := errors.New("the enterprise read port is down")
	in.OwnershipReader = &tiCandidates{fail: failure}
	got, err := buildTrustInputs(in)
	if err != nil || got.Ownership == nil {
		t.Fatalf("built %v, %v", got.Ownership, err)
	}
	owned, err := got.Ownership.OwnsTarget(tiDeadline(t), "ten_aa", "usr_alice", tiIntent("ten_aa", "usr_alice", node, kind+":TX0001"))
	if !errors.Is(err, ownershipport.ErrRead) || owned {
		t.Fatalf("got %v, %v; want ErrRead and not owned", owned, err)
	}
}

// With no enterprise read there is no Ownership: a named gap, never an adapter that answers from nothing (A469). The process starts.
func TestOwnershipWithNoEnterpriseReadIsANamedGap(t *testing.T) {
	in := tiIn(t)
	in.OwnershipReader = nil
	got, err := buildTrustInputs(in)
	if err != nil {
		t.Fatalf("a gap is not an error: %v", err)
	}
	if got.Ownership != nil || !tiHas(got.Gaps, "Ownership") {
		t.Fatalf("Ownership %v, gaps %v; want none and a named gap", got.Ownership, got.Gaps)
	}
}

// With no pack, or with no served tenant, there is nothing to build from or for: a named gap too.
func TestOwnershipWithNoPackOrNoServedTenantIsAGap(t *testing.T) {
	in := tiIn(t)
	in.Pack = nil
	got, err := buildTrustInputs(in)
	if err != nil || got.Ownership != nil || !tiHas(got.Gaps, "Ownership") {
		t.Fatalf("no pack: %v, %v, gaps %v", got.Ownership, err, got.Gaps)
	}
	in = tiIn(t)
	in.Tenants = nil
	got, err = buildTrustInputs(in)
	if err != nil || got.Ownership != nil || !tiHas(got.Gaps, "Ownership") {
		t.Fatalf("no served tenant: %v, %v, gaps %v", got.Ownership, err, got.Gaps)
	}
}

// fill gives the composition the built port and touches nothing else.
func TestFillSetsOnlyTheBuiltInputs(t *testing.T) {
	got, err := buildTrustInputs(tiIn(t))
	if err != nil {
		t.Fatal(err)
	}
	a := got.fill(TrustAdapters{PackVersion: "synthetic.pack.v1"})
	if a.Ownership == nil || a.PackVersion != "synthetic.pack.v1" || a.Intents != nil || a.Recorder != nil || a.Planner != nil {
		t.Fatalf("fill gave %+v", a)
	}
}
