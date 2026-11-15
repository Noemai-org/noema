package snapshot_test

import (
	"encoding/json"
	"math/rand"
	"os"
	"testing"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
)

// EVD-03, REQ-CTX-004 (sealed context snapshots), INV-17, DEC-2026-10-16 C1: the evidence_set_hash is the golden vectors' construction, in Go.
// SYNTHETIC vectors from contracts/fixtures/golden_vectors.json (CON-04); the same file the Python check (tools/check_vectors.py) runs.

type vec struct {
	Name     string          `json:"name"`
	Input    []snapshot.Pair `json:"input"`
	Expected string          `json:"expected_evidence_set_hash"`
}

type negative struct {
	Name   string          `json:"name"`
	Input  []snapshot.Pair `json:"input"`
	Reason string          `json:"reason"`
}

func load(t *testing.T) ([]vec, []negative) {
	t.Helper()
	raw, err := os.ReadFile("../../../../contracts/fixtures/golden_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Positive []vec      `json:"evidence_set_hash_vectors"`
		Negative []negative `json:"evidence_set_hash_negative_vectors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Positive) == 0 || len(doc.Negative) == 0 {
		t.Fatal("the golden file has no evidence-set vectors")
	}
	return doc.Positive, doc.Negative
}

func TestTheGoldenVectorsGiveTheExpectedHash(t *testing.T) {
	positive, _ := load(t)
	for _, v := range positive {
		got, err := snapshot.SetHash(v.Input)
		if err != nil || got != v.Expected {
			t.Errorf("%s: got %s, %v; want %s", v.Name, got, err, v.Expected)
		}
	}
}

func TestEveryNegativeVectorIsRefused(t *testing.T) {
	_, negative := load(t)
	for _, n := range negative {
		if got, err := snapshot.SetHash(n.Input); err == nil {
			t.Errorf("%s (%s) was hashed to %s", n.Name, n.Reason, got)
		}
	}
}

// The order the caller lists the pairs in never matters (the hash sorts by the bytes of evidence_id), and the input slice is not reordered or changed.
func TestTheHashDoesNotDependOnInputOrderAndDoesNotChangeTheInput(t *testing.T) {
	positive, _ := load(t)
	var items []snapshot.Pair
	for _, v := range positive {
		if len(v.Input) > len(items) {
			items = v.Input
		}
	}
	want, err := snapshot.SetHash(items)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(7)) // #nosec G404 -- a seeded permutation in a test
	for i := 0; i < 50; i++ {
		shuffled := append([]snapshot.Pair(nil), items...)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		before := append([]snapshot.Pair(nil), shuffled...)
		got, err := snapshot.SetHash(shuffled)
		if err != nil || got != want {
			t.Fatalf("permutation %d: %s, %v; want %s", i, got, err, want)
		}
		for k := range before {
			if before[k] != shuffled[k] {
				t.Fatal("SetHash reordered its input")
			}
		}
	}
}

// Anything that changes the set changes the hash: a payload hash, an evidence id, an extra item, a dropped item. The empty set is a fact about the function and
// the sealer refuses it separately.
func TestAnyChangeToTheSetChangesTheHash(t *testing.T) {
	a := snapshot.Pair{EvidenceID: "evd_01", PayloadHash: "sha256:" + string(make([]byte, 0)) + "0000000000000000000000000000000000000000000000000000000000000000"}
	b := snapshot.Pair{EvidenceID: "evd_02", PayloadHash: "sha256:1111111111111111111111111111111111111111111111111111111111111111"}
	base, _ := snapshot.SetHash([]snapshot.Pair{a, b})
	variants := map[string][]snapshot.Pair{
		"a payload hash": {a, {EvidenceID: b.EvidenceID, PayloadHash: "sha256:2222222222222222222222222222222222222222222222222222222222222222"}},
		"an evidence id": {a, {EvidenceID: "evd_03", PayloadHash: b.PayloadHash}},
		"a dropped item": {a},
		"an extra item":  {a, b, {EvidenceID: "evd_09", PayloadHash: b.PayloadHash}},
	}
	for name, v := range variants {
		if got, err := snapshot.SetHash(v); err != nil || got == base {
			t.Errorf("%s did not change the hash (%v)", name, err)
		}
	}
}
