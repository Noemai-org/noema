package snapshot_test

// TRU-07 (A365; REQ-TRU-009, REQ-CTX-004, INV-25, INV-17, INV-21; SoT 43.2a): the content hash of a snapshot is what a confirmation binds as its evidence, instead of the hash over the
// evidence IDs. The evidence layer mints a new evidence ID for every read, so two snapshots of identical data never share a set hash; the content hash covers the sorted (source,
// source record ID, payload hash) triples only, so a re-read of unchanged evidence gives the same value and a changed record gives another. All data is SYNTHETIC (AGENT-25).
//
// The two pinned values were computed by an independent Python implementation (SHA-256 over LP("noema.evidence_content_set.v1") and then, for each distinct triple in sorted order,
// LP(source) LP(source_record_id) LP(payload_hash), LP being a 4-byte big-endian length and the bytes), not by the function under test.

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
	return snapshot.ContentItem{Source: source, SourceRecordID: record, PayloadHash: ph(n)}
}

func three() []snapshot.ContentItem {
	return []snapshot.ContentItem{triple("ledger.synth", "rec-1", 1), triple("switch.synth", "rec-2", 2), triple("settle.synth", "rec-3", 3)}
}

const (
	vecOneItem    = "sha256:d4af355041bed444ceb2d16f27f28c3c26787d1d6e45e41eef5cdcd060221533"
	vecThreeItems = "sha256:38b9af14bdbd1c1e1ed1a1401dce726998e1e1c34a4f3fdf9225dea2852b8310"
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
			"source":      func(c *snapshot.ContentItem) { c.Source += "x" },
			"record ID":   func(c *snapshot.ContentItem) { c.SourceRecordID += "x" },
			"payload":     func(c *snapshot.ContentItem) { c.PayloadHash = ph(900 + i) },
			"all swapped": func(c *snapshot.ContentItem) { c.Source, c.SourceRecordID = c.SourceRecordID, c.Source },
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

// A365: the order is by the three fields in turn, bytewise; two items that differ only in the record ID or only in the payload are told apart, whichever comes first.
func TestContentHashSortsByEveryFieldInTurn(t *testing.T) {
	a := []snapshot.ContentItem{triple("s", "r1", 1), triple("s", "r1", 2), triple("s", "r2", 1)}
	b := []snapshot.ContentItem{a[2], a[1], a[0]}
	if mustHash(t, a) != mustHash(t, b) {
		t.Error("the order of items that share a source changed the hash")
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
		pairs[i] = snapshot.Pair{EvidenceID: it.SourceRecordID, PayloadHash: it.PayloadHash}
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

// A365 (defined, not refused): a repeated triple is one fact. The same record read twice with the same payload is the same content, so a snapshot that holds it twice binds the
// same as one that holds it once; the repeat changes nothing and fails nothing.
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
		"no payload hash":    {{Source: "s", SourceRecordID: "r"}},
		"an upper-case hash": {{Source: "s", SourceRecordID: "r", PayloadHash: strings.ToUpper(ph(10))}},
		"a short hash":       {{Source: "s", SourceRecordID: "r", PayloadHash: "sha256:abc"}},
		"one bad among good": append(three(), snapshot.ContentItem{Source: "s", SourceRecordID: "", PayloadHash: ph(4)}),
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
