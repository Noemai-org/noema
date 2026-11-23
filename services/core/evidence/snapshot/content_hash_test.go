package snapshot_test

// TRU-07 (A365, A435; REQ-TRU-009, REQ-CTX-004, INV-25, INV-17, INV-21; SoT 43.2a): the content hash of a snapshot is what a confirmation binds as its evidence, instead of the hash
// over the evidence IDs. The evidence layer mints a new evidence ID for every read, so two snapshots of identical data never share a set hash; the content hash covers the sorted
// (source, source record ID, facts digest, status) entries only, where the facts digest is the digest of the item's MAPPED, NORMALIZED facts (FactsDigest; the raw payload hash is not
// used: two reads of unchanged evidence can differ in a volatile or unmapped field) and the status is the item's fact status as the Reconciler gives it. A re-read of unchanged
// evidence gives the same value and a changed mapped fact or status gives another. All data is SYNTHETIC (AGENT-25).
//
// The pinned values were computed by an independent Python implementation (SHA-256 over LP("noema.evidence_content_set.v3") and then, for each distinct entry in sorted order,
// LP(source) LP(source_record_id) LP(facts_digest) LP(status), LP being a 4-byte big-endian length and the bytes), not by the function under test.

import (
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
)

func ph(n int) string { return fmt.Sprintf("sha256:%064x", n) }

func triple(source, record string, n int) snapshot.ContentItem {
	return snapshot.ContentItem{Source: source, SourceRecordID: record, FactsDigest: ph(n), Status: "KNOWN"}
}

// The three digests are those of the facts F1, F2 and F4 of facts_digest_test.go (independently computed there).
const (
	digestLedger = "sha256:8a1742b78829362cb21471c84b8f62cd297b588092533cfd1ceb427972f1c0cd"
	digestSwitch = "sha256:05c4c1e0f4cb189c3152f7cf1cf0066c73f2c9fd64ccabdc6d93e2cf79df35fb"
	digestSettle = "sha256:23d0bd027259d4dc68ab5601393208176d349963fe3463d6d283cd98a2f4e566"
)

func three() []snapshot.ContentItem {
	return []snapshot.ContentItem{{Source: "ledger.synth", SourceRecordID: "rec-1", FactsDigest: digestLedger, Status: "KNOWN"},
		{Source: "switch.synth", SourceRecordID: "rec-2", FactsDigest: digestSwitch, Status: "KNOWN"}, {Source: "settle.synth", SourceRecordID: "rec-3", FactsDigest: digestSettle, Status: "KNOWN"}}
}

const (
	vecOneItem    = "sha256:26c3f1f7c48cb1fa355d8af99006fdeb7bba303aaf34611faeacbec8e9bb4383"
	vecThreeItems = "sha256:19c085913c1e060b940dd8a3b9814d7edcebd82251dbd1f93240c82c87a0b77f"
	// the same three items with the statuses STALE, KNOWN and UNKNOWN
	vecMixedStatuses = "sha256:d5adb07baccb43dfc41f083968af88db5476251aad7663eccc553406e761b693"
)

func mustHash(t *testing.T, items []snapshot.ContentItem) string {
	t.Helper()
	h, err := snapshot.ContentHash(items)
	if err != nil {
		t.Fatalf("ContentHash(%v): %v", items, err)
	}
	return h
}

// The construction is pinned by independently computed vectors, in the form of a content hash.
func TestContentHashMatchesTheIndependentVectors(t *testing.T) {
	if got := mustHash(t, three()[:1]); got != vecOneItem {
		t.Errorf("one item: %s, want %s", got, vecOneItem)
	}
	if got := mustHash(t, three()); got != vecThreeItems {
		t.Errorf("three items: %s, want %s", got, vecThreeItems)
	}
	mixed := three()
	mixed[0].Status, mixed[2].Status = "STALE", "UNKNOWN"
	if got := mustHash(t, mixed); got != vecMixedStatuses {
		t.Errorf("three items with other statuses: %s, want %s", got, vecMixedStatuses)
	}
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(vecThreeItems) {
		t.Error("the pinned vector is not in the contract's Hash form")
	}
}

// A365: the value does not depend on the evidence IDs. The function takes no ID at all; the same data read again, under whatever IDs, is the same value (the adapter test and the
// database test show that the IDs of two snapshots differ and the content hash does not).
func TestContentHashHasNoEvidenceIDInIt(t *testing.T) {
	a := mustHash(t, three())
	b := mustHash(t, append([]snapshot.ContentItem(nil), three()...))
	if a != b {
		t.Errorf("the same content gave %s and %s", a, b)
	}
}

// A365, INV-25: any change of the source, the record ID or the payload hash of any one item is another value, and so is one item more or fewer.
func TestContentHashChangesWithAnyFieldOfAnyItem(t *testing.T) {
	base := mustHash(t, three())
	for i := range three() {
		for name, edit := range map[string]func(*snapshot.ContentItem){
			"source":       func(c *snapshot.ContentItem) { c.Source += "x" },
			"record ID":    func(c *snapshot.ContentItem) { c.SourceRecordID += "x" },
			"facts digest": func(c *snapshot.ContentItem) { c.FactsDigest = ph(900 + i) },
			"status":       func(c *snapshot.ContentItem) { c.Status = "STALE" },
			"all swapped":  func(c *snapshot.ContentItem) { c.Source, c.SourceRecordID = c.SourceRecordID, c.Source },
		} {
			items := three()
			edit(&items[i])
			if got := mustHash(t, items); got == base {
				t.Errorf("item %d, %s changed: the hash did not", i, name)
			}
		}
	}
	if mustHash(t, three()[:2]) == base || mustHash(t, append(three(), triple("other.synth", "rec-9", 9))) == base {
		t.Error("one item fewer or more gave the same hash")
	}
}

// A365, INV-21: the order of the items does not matter. A fixed seed shuffles the items many times.
func TestContentHashIsOrderIndependent(t *testing.T) {
	var items []snapshot.ContentItem
	for i := 0; i < 12; i++ {
		items = append(items, triple(fmt.Sprintf("src%d.synth", i%4), fmt.Sprintf("rec-%d", i), i+1))
	}
	want := mustHash(t, items)
	r := rand.New(rand.NewSource(365)) // #nosec G404 -- a fixed seed for a reproducible test, not a secret
	for n := 0; n < 200; n++ {
		shuffled := append([]snapshot.ContentItem(nil), items...)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := mustHash(t, shuffled); got != want {
			t.Fatalf("shuffle %d: %s, want %s", n, got, want)
		}
	}
}

// A365 (refined): the order is by source, then record ID, and items that share a record ID across sources are told apart by the source: shuffling them never changes the value.
func TestContentHashSortsBySourceThenRecordID(t *testing.T) {
	items := []snapshot.ContentItem{triple("a.synth", "rec-1", 1), triple("b.synth", "rec-1", 2), triple("c.synth", "rec-1", 3), triple("a.synth", "rec-2", 4), triple("b.synth", "rec-2", 5)}
	want := mustHash(t, items)
	r := rand.New(rand.NewSource(3651)) // #nosec G404 -- a fixed seed for a reproducible test, not a secret
	for n := 0; n < 100; n++ {
		shuffled := append([]snapshot.ContentItem(nil), items...)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := mustHash(t, shuffled); got != want {
			t.Fatalf("shuffle %d: %s, want %s", n, got, want)
		}
	}
}

// A365: the order is by the three fields in turn, bytewise; two items that differ only in the record ID or only in the payload are told apart, whichever comes first.
func TestContentHashSortsByEveryFieldInTurn(t *testing.T) {
	a := []snapshot.ContentItem{triple("s", "r1", 1), triple("s", "r1", 2), triple("s", "r2", 1)}
	b := []snapshot.ContentItem{a[2], a[1], a[0]}
	if mustHash(t, a) != mustHash(t, b) {
		t.Error("the order of items that share a source changed the hash")
	}
}

// A435: the status is part of the entry: two entries that differ only in the status are two entries (both count, in either order).
func TestContentHashCountsTheStatusOfAnEntry(t *testing.T) {
	known, stale := triple("s", "r1", 1), triple("s", "r1", 1)
	stale.Status = "STALE"
	both := mustHash(t, []snapshot.ContentItem{known, stale})
	if both != mustHash(t, []snapshot.ContentItem{stale, known}) {
		t.Error("the order of two entries that differ only in status changed the hash")
	}
	if both == mustHash(t, []snapshot.ContentItem{known}) || both == mustHash(t, []snapshot.ContentItem{stale}) {
		t.Error("an entry that differs only in status was counted as the same entry")
	}
	if mustHash(t, []snapshot.ContentItem{known}) == mustHash(t, []snapshot.ContentItem{stale}) {
		t.Error("the status did not change the hash")
	}
}

// A365: a fact is a whole triple. Moving characters between fields (no separator can be forged) is another value, and so is a different pairing of records and payloads.
func TestContentHashKeepsTheFieldsApart(t *testing.T) {
	x := mustHash(t, []snapshot.ContentItem{triple("ab", "c", 1)})
	y := mustHash(t, []snapshot.ContentItem{triple("a", "bc", 1)})
	if x == y {
		t.Error("the boundary between source and record ID was not kept")
	}
	p := mustHash(t, []snapshot.ContentItem{triple("s", "r1", 1), triple("s", "r2", 2)})
	q := mustHash(t, []snapshot.ContentItem{triple("s", "r1", 2), triple("s", "r2", 1)})
	if p == q {
		t.Error("a different pairing of records and payloads gave the same hash")
	}
}

// A365: the domain tag separates this hash from every other: the content hash of a snapshot is never the set hash of the same payloads, and the empty tag gives another value.
func TestContentHashIsDomainSeparated(t *testing.T) {
	items := three()
	pairs := make([]snapshot.Pair, len(items))
	for i, it := range items {
		pairs[i] = snapshot.Pair{EvidenceID: it.SourceRecordID, PayloadHash: it.FactsDigest}
	}
	set, err := snapshot.SetHash(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if mustHash(t, items) == set {
		t.Error("the content hash is the set hash over the same strings: there is no domain separation")
	}
	if got := mustHash(t, items); got != vecThreeItems {
		t.Errorf("the pinned vector (domain tag included) is not reproduced: %s", got)
	}
}

// A365, A435 (defined, not refused): a repeated entry is one fact. The same record read twice with the same facts and the same status is the same content, so a snapshot that holds it
// twice binds the same as one that holds it once; the repeat changes nothing and fails nothing.
func TestContentHashDefinesARepeatedTripleAsOneFact(t *testing.T) {
	once := mustHash(t, three())
	twice := mustHash(t, append(three(), three()[0]))
	if once != twice {
		t.Errorf("a repeated triple changed the hash: %s and %s", once, twice)
	}
}

// A365: what cannot be bound is refused, never hashed: no items, an empty source, an empty record ID, a payload hash that is not in the contract's form.
func TestContentHashRefusesWhatCannotBeBound(t *testing.T) {
	for name, items := range map[string][]snapshot.ContentItem{
		"no items":           nil,
		"an empty source":    {triple("", "r", 1)},
		"an empty record ID": {triple("s", "", 1)},
		"no facts digest":    {{Source: "s", SourceRecordID: "r", Status: "KNOWN"}},
		"no status":          {{Source: "s", SourceRecordID: "r", FactsDigest: ph(1)}},
		"an upper-case hash": {{Source: "s", SourceRecordID: "r", FactsDigest: strings.ToUpper(ph(10)), Status: "KNOWN"}},
		"a short hash":       {{Source: "s", SourceRecordID: "r", FactsDigest: "sha256:abc", Status: "KNOWN"}},
		"one bad among good": append(three(), snapshot.ContentItem{Source: "s", SourceRecordID: "", FactsDigest: ph(4), Status: "KNOWN"}),
	} {
		got, err := snapshot.ContentHash(items)
		if !errors.Is(err, snapshot.ErrContentHashInput) || got != "" {
			t.Errorf("%s: %q, %v; want ErrContentHashInput and no hash", name, got, err)
		}
	}
}

// The input is not modified.
func TestContentHashDoesNotModifyItsInput(t *testing.T) {
	items := []snapshot.ContentItem{triple("z", "r", 2), triple("a", "r", 1)}
	mustHash(t, items)
	if items[0].Source != "z" || items[1].Source != "a" {
		t.Error("ContentHash reordered its input")
	}
}
