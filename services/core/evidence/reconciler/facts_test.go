package reconciler_test

// TRU-07 (A365, refined; REQ-TRU-009, REQ-CTX-001, REQ-RES-001; INV-19, INV-21, INV-25; AGENT-13): Facts gives the mapped, normalized facts of one evidence item, by the SAME evidence
// mapping Classify uses (one function reads the payload and applies the mapping; there is no second mapping). A confirmation binds the digest of these facts, not the raw payload, because
// two reads of unchanged evidence can differ in a volatile or unmapped field. The payload store is a spy; the real rule set and the real mapping of the fintech pack run. SYNTHETIC.

import (
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/evidence/reconciler"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

func factsOf(t *testing.T, w *world, i int) []snapshot.Fact {
	t.Helper()
	got, err := w.rec.Facts(ctx(t), tenant, w.items[i])
	if err != nil {
		t.Fatalf("Facts: %v", err)
	}
	return got
}

func digestOf(t *testing.T, facts []snapshot.Fact) string {
	t.Helper()
	d, err := snapshot.FactsDigest(facts)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The facts are the rows the mapping gives, with the item's status: a posted debit in the ledger is the entity debit, KNOWN, state POSTED, typed as an enum.
func TestFactsAreTheRowsTheMappingGivesWithTheItemsStatus(t *testing.T) {
	w := build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("PENDING"))
	want := map[int][]snapshot.Fact{
		0: {{Entity: "debit", Status: "KNOWN", Fields: map[string]snapshot.FactValue{"state": {Kind: snapshot.KindEnum, Text: "POSTED"}}}},
		1: {{Entity: "switch", Status: "KNOWN", Fields: map[string]snapshot.FactValue{"state": {Kind: snapshot.KindEnum, Text: "SUCCESSFUL"}}}},
		2: {{Entity: "credit", Status: "KNOWN", Fields: map[string]snapshot.FactValue{"state": {Kind: snapshot.KindEnum, Text: "PENDING"}}}},
	}
	for i, exp := range want {
		got := factsOf(t, w, i)
		if len(got) != 1 || got[0].Entity != exp[0].Entity || got[0].Status != exp[0].Status || len(got[0].Fields) != 1 || got[0].Fields["state"] != exp[0].Fields["state"] {
			t.Errorf("item %d: %+v, want %+v", i, got, exp)
		}
	}
}

// A365 refined: two payloads of one item that differ only in what the mapping drops or normalizes (a volatile field, an unmapped field, the order of the keys and of the entries, an
// entry the mapping does not look at) give the same facts and the same digest.
func TestPayloadsThatDifferOnlyInWhatTheMappingDropsGiveTheSameFactsDigest(t *testing.T) {
	posted := map[string]any{"entry_type": "DEBIT", "posting_status": "POSTED", "amount": "5000"}
	other := map[string]any{"entry_type": "CREDIT", "posting_status": "POSTED"}
	a := build(t, spec{source: ledger, payload: map[string]any{"entries": []any{posted, other}}})
	b := build(t, spec{source: ledger, payload: map[string]any{"entries": []any{other, posted, map[string]any{"entry_type": "FEE"}}, "fetched_at": "2026-11-17T09:00:01Z", "request_id": "r-2"}})
	if a.items[0].PayloadHash == b.items[0].PayloadHash {
		t.Fatal("the fixture is wrong: the two payloads have one hash")
	}
	da, db := digestOf(t, factsOf(t, a, 0)), digestOf(t, factsOf(t, b, 0))
	if da != db {
		t.Errorf("payloads that differ only in unmapped content gave two digests: %s and %s", da, db)
	}
	s1, s2 := build(t, switchIs("SUCCESS")), build(t, spec{source: sw, payload: map[string]any{"semantic_status": "SUCCESS", "latency_ms": 41, "trace": "x"}})
	if digestOf(t, factsOf(t, s1, 0)) != digestOf(t, factsOf(t, s2, 0)) {
		t.Error("an unmapped volatile field of the switch changed the digest")
	}
}

// A365 refined, INV-25: a change of a mapped fact is another digest: the switch's state, the credit's state, a debit that is no longer posted (the row has no field, so it is UNKNOWN).
func TestAChangeOfAMappedFactChangesTheFactsDigest(t *testing.T) {
	base := digestOf(t, factsOf(t, build(t, switchIs("SUCCESS")), 0))
	for name, w := range map[string]*world{
		"the switch timed out":  build(t, switchIs("TIMED_OUT")),
		"the switch was failed": build(t, switchIs("REJECTED")),
		"the switch is stale":   build(t, spec{source: sw, payload: map[string]any{"semantic_status": "SUCCESS"}, status: contracts.EnumsV1FactStatusSTALE}),
		"a status not listed":   build(t, switchIs("SOMETHING_NEW")),
	} {
		if got := digestOf(t, factsOf(t, w, 0)); got == base {
			t.Errorf("%s: the digest did not change", name)
		}
	}
	posted := digestOf(t, factsOf(t, build(t, ledgerPosted()), 0))
	notPosted := digestOf(t, factsOf(t, build(t, spec{source: ledger, payload: map[string]any{"entries": []any{map[string]any{"entry_type": "DEBIT", "posting_status": "PENDING"}}}}), 0))
	if posted == notPosted {
		t.Error("a debit that is no longer posted gave the digest of a posted one")
	}
	if digestOf(t, factsOf(t, build(t, destIs("CREDITED")), 0)) == digestOf(t, factsOf(t, build(t, destIs("NOT_CREDITED")), 0)) {
		t.Error("the credit's state did not change the digest")
	}
}

// The rule Classify applies is the rule Facts applies: a KNOWN item whose payload gives the row no field is UNKNOWN with no field; a non-KNOWN item's payload is not read and its row has
// no field and the item's own status.
func TestAnItemThatGivesNoFieldIsUnknownAndANonKnownItemIsNotRead(t *testing.T) {
	w := build(t, switchIs("SOMETHING_NEW"), spec{source: ledger, payload: map[string]any{"entries": []any{}}, status: contracts.EnumsV1FactStatusSTALE})
	got := factsOf(t, w, 0)
	if len(got) != 1 || got[0].Status != "UNKNOWN" || len(got[0].Fields) != 0 {
		t.Errorf("a KNOWN item with no mapped field: %+v, want one UNKNOWN fact with no field", got)
	}
	stale := factsOf(t, w, 1)
	if len(stale) != 1 || stale[0].Entity != "debit" || stale[0].Status != "STALE" || len(stale[0].Fields) != 0 {
		t.Errorf("a STALE item: %+v, want one STALE debit with no field", stale)
	}
	for _, ref := range w.st.asked {
		if ref == "pay_b" {
			t.Fatal("the payload of a STALE item was read")
		}
	}
}

// A365 refined: where Classify turns a payload that is not the sealed one, not an object or not canonical into an UNKNOWN row, Facts refuses it: a binding cannot rest on it (fail closed).
func TestFactsRefuseAPayloadThatIsNotTheSealedOne(t *testing.T) {
	for name, raw := range map[string]string{
		"a hash mismatch":  `{"semantic_status":"TIMED_OUT"}`,
		"not an object":    `["a"]`,
		"not canonical":    `{"semantic_status":"SUCCESS","x":1.5}`,
		"trailing content": `{"semantic_status":"SUCCESS"} {}`,
	} {
		w := build(t, switchIs("SUCCESS"))
		w.st.payloads["pay_a"] = []byte(raw)
		got, err := w.rec.Facts(ctx(t), tenant, w.items[0])
		if !errors.Is(err, reconciler.ErrPayload) || got != nil {
			t.Errorf("%s: %+v, %v; want ErrPayload and no fact", name, got, err)
		}
	}
}

// A store that fails is a fault, not an absence: Facts is refused (ErrRead), the cause is reachable and no text of the store is in the error (AGENT-13).
func TestFactsRefuseAFailingStoreWithoutItsText(t *testing.T) {
	w := build(t, switchIs("SUCCESS"))
	w.st.err = errors.New("dial postgres://trust_rw:s3cret@db.internal:5432/noema: connection refused")
	got, err := w.rec.Facts(ctx(t), tenant, w.items[0])
	if !errors.Is(err, reconciler.ErrRead) || !errors.Is(err, payloadread.ErrUnavailable) || got != nil {
		t.Fatalf("%+v, %v; want ErrRead over ErrUnavailable and no fact", got, err)
	}
	for _, leak := range []string{"postgres", "s3cret", "db.internal"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the error carries %q: %v", leak, err)
		}
	}
}

// An item Facts cannot use is refused: no tenant, no evidence ID, no source, a source the mapping does not know, a status that is not one. The payload is read as the caller's tenant.
func TestFactsRefuseAnItemTheMappingCannotUse(t *testing.T) {
	cases := map[string]func(*world){
		"no evidence id":                     func(w *world) { w.items[0].EvidenceID = "" },
		"no source":                          func(w *world) { w.items[0].Source = "" },
		"a source the mapping does not know": func(w *world) { w.items[0].Source = "fin.src.unknown" },
		"a status that is not a status":      func(w *world) { w.items[0].Status = "MAYBE" },
	}
	for name, mutate := range cases {
		w := build(t, switchIs("SUCCESS"))
		mutate(w)
		if got, err := w.rec.Facts(ctx(t), tenant, w.items[0]); !errors.Is(err, reconciler.ErrItem) || got != nil {
			t.Errorf("%s: %+v, %v; want ErrItem", name, got, err)
		}
	}
	w := build(t, switchIs("SUCCESS"))
	if _, err := w.rec.Facts(ctx(t), "", w.items[0]); !errors.Is(err, reconciler.ErrItem) {
		t.Errorf("no tenant: %v, want ErrItem", err)
	}
	factsOf(t, w, 0)
	for _, got := range w.st.tenants {
		if got != tenant {
			t.Fatalf("a payload was read as %q", got)
		}
	}
}

// Facts does not need the item's times or its freshness limit (the evidence adapter has the stored columns only, and the stricter limit applied at read time is not stored).
func TestFactsNeedNoTimeAndNoFreshnessLimitOfTheItem(t *testing.T) {
	w := build(t, switchIs("SUCCESS"))
	it := w.items[0]
	it.ReadAt, it.EffectiveAt, it.EffectiveFreshnessSeconds = it.ReadAt.AddDate(1, 0, 0), it.EffectiveAt.AddDate(1, 0, 0), 0
	if _, err := w.rec.Facts(ctx(t), tenant, it); err != nil {
		t.Fatalf("Facts: %v", err)
	}
}
