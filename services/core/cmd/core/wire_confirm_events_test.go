package main

// A710, A724 (interim, relayed; REQ-TRU-009; INV-03, INV-12; PERD 32.4): the composition gives each tenant's confirmation store the Record events of the store, appended in the store's own
// transaction by the core-trust appender, so a row and its event commit together. The Confirmations adapter itself still lacks the request read and the confirm policy, which stay named
// gaps. Identifiers are SYNTHETIC.

import (
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg2"
)

func decInputsWithStores(t *testing.T, over map[string]string) decisionInputs {
	in := decInputs(t, over)
	in.RequestPools = tiPools(t, "ten_aa", "ten_bb")
	return in
}

func TestEachTenantsStoreGetsTheRecordEventsAndTheTenantsOwnLogin(t *testing.T) {
	in := decInputsWithStores(t, nil)
	p, err := buildDecisions(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.RequestStores) != 2 {
		t.Fatalf("stores %v; want one for each of the two tenants", p.RequestStores)
	}
	for tenant, st := range p.RequestStores {
		if st == nil || st.Pool != in.RequestPools[tenant] || st.IDs == nil {
			t.Errorf("%s: store %+v is not over this tenant's own login and an ID source", tenant, st)
			continue
		}
		ev, ok := st.Events.(trustpg2.RecordEvents)
		if !ok || ev.Appender == nil || ev.Appender.Signers["core-trust"] == nil {
			t.Errorf("%s: events %T; want trustpg2.RecordEvents over an appender that signs as core-trust", tenant, st.Events)
		}
	}
	if p.RequestStores["ten_aa"] == p.RequestStores["ten_bb"] {
		t.Error("two tenants share one store")
	}
}

func TestWithTheStoresBuiltTheConfirmationsGapNamesOnlyTheRequestReadAndTheConfirmPolicy(t *testing.T) {
	p, err := buildDecisions(decInputsWithStores(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	g, ok := decGap(p, "Confirmations")
	if !ok || strings.Join(g.Missing, "|") != strings.Join([]string{needRequestRead, needConfirmPolicy}, "|") {
		t.Errorf("gap %+v (named %v); want exactly %q", g, ok, []string{needRequestRead, needConfirmPolicy})
	}
}

func TestWithoutTheLoginsOrWithAnIncompleteRecorderThereIsNoStoreAndTheEventsStayNamed(t *testing.T) {
	all := []string{needRequestRead, needRequestEvents, needConfirmPolicy}
	noPools := decInputs(t, nil)
	incomplete := decInputsWithStores(t, map[string]string{EnvTrustKeyDir: "<unset>"})
	for name, in := range map[string]decisionInputs{"no logins": noPools, "no signing keys": incomplete} {
		p, err := buildDecisions(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(p.RequestStores) != 0 {
			t.Errorf("%s: stores %v; want none (fail closed: no store without the Record events)", name, p.RequestStores)
		}
		g, ok := decGap(p, "Confirmations")
		if !ok || strings.Join(g.Missing, "|") != strings.Join(all, "|") {
			t.Errorf("%s: gap %+v; want all three named", name, g)
		}
	}
}
