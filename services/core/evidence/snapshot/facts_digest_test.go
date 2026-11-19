package snapshot_test

// TRU-07 (A365, refined; REQ-TRU-009, REQ-CTX-004, REQ-RES-001, INV-25, INV-19, INV-21; SoT 43.2a): the digest of the mapped, normalized facts of one evidence item. The raw payload can
// differ between two reads of unchanged evidence (a volatile or an unmapped field), so the confirmation binds the facts the evidence mapping gives, which are what the decision rests on.
// FactsDigest is a pure function of those facts: no payload, no evidence ID, no time. All data is SYNTHETIC (AGENT-25).
//
// The pinned values were computed by an independent Python implementation (vectors_r4t13b.py, not run in CI; the pattern of content_hash_test.go): SHA-256 over LP("noema.evidence_item_facts.v1"),
// LP(decimal count of facts), then each fact in bytewise order of its entity as LP(entity) LP(status) LP(decimal count of fields) and each field in bytewise order of its name as
// LP(name) LP(kind) LP(text); LP is a 4-byte big-endian length and the bytes. Integers, timestamps and durations are canonical base-10 text of an int64 and booleans are "true" or "false",
// so no value has two spellings and none is a float.

import (
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
)

func enum(s string) snapshot.FactValue { return snapshot.FactValue{Kind: snapshot.KindEnum, Text: s} }

func fact(entity, status string, fields map[string]snapshot.FactValue) snapshot.Fact {
	return snapshot.Fact{Entity: entity, Status: status, Fields: fields}
}

func f1() []snapshot.Fact {
	return []snapshot.Fact{fact("debit", "KNOWN", map[string]snapshot.FactValue{"state": enum("POSTED")})}
}

func f2() []snapshot.Fact { // given out of order on purpose
	return []snapshot.Fact{fact("switch", "KNOWN", map[string]snapshot.FactValue{"state": enum("SUCCESSFUL")}), f1()[0]}
}

func f3() []snapshot.Fact { // every kind of value
	return []snapshot.Fact{fact("payment", "KNOWN", map[string]snapshot.FactValue{
		"currency": {Kind: snapshot.KindString, Text: "NGN"}, "amount": {Kind: snapshot.KindInteger, Text: "150000"}, "direction": enum("DEBIT"),
		"settled": {Kind: snapshot.KindBoolean, Text: "true"}, "at": {Kind: snapshot.KindTimestamp, Text: "1763463600"}, "ttl": {Kind: snapshot.KindDuration, Text: "-30"},
		"ref": {Kind: snapshot.KindReference, Text: "acct-7"}})}
}

func f4() []snapshot.Fact { return []snapshot.Fact{fact("credit", "UNKNOWN", nil)} } // a fact with no field

const (
	vecF1 = "sha256:8a1742b78829362cb21471c84b8f62cd297b588092533cfd1ceb427972f1c0cd"
	vecF2 = "sha256:05c4c1e0f4cb189c3152f7cf1cf0066c73f2c9fd64ccabdc6d93e2cf79df35fb"
	vecF3 = "sha256:f8d84a1ea7076cb3275a6370ab49aa2d5b2a95af678584b45980977b0b3b635c"
	vecF4 = "sha256:23d0bd027259d4dc68ab5601393208176d349963fe3463d6d283cd98a2f4e566"
)

func digest(t *testing.T, facts []snapshot.Fact) string {
	t.Helper()
	d, err := snapshot.FactsDigest(facts)
	if err != nil {
		t.Fatalf("FactsDigest(%v): %v", facts, err)
	}
	return d
}

// The encoding is pinned by independently computed vectors: one fact, two facts given out of order, every kind of value, and a fact with no field. They also pin the domain tag.
func TestFactsDigestMatchesTheIndependentVectors(t *testing.T) {
	for name, c := range map[string]struct {
		facts []snapshot.Fact
		want  string
	}{"one fact": {f1(), vecF1}, "two facts out of order": {f2(), vecF2}, "every kind": {f3(), vecF3}, "no field": {f4(), vecF4}} {
		if got := digest(t, c.facts); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
		if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(c.want) {
			t.Errorf("%s: the pinned vector is not in the contract's Hash form", name)
		}
	}
}

// A365 refined, INV-21: the digest does not depend on the order of the facts or on the order a map yields its fields: a fact with seven fields gives the vector every time (Go's map order
// is random), and the facts shuffled with a fixed seed give one value.
func TestFactsDigestIsNormalizedKeyOrderAndFactOrderDoNotMatter(t *testing.T) {
	for n := 0; n < 100; n++ {
		if got := digest(t, f3()); got != vecF3 {
			t.Fatalf("run %d: %s, want %s", n, got, vecF3)
		}
	}
	var facts []snapshot.Fact
	for i := 0; i < 9; i++ {
		facts = append(facts, fact(fmt.Sprintf("entity%d", i), "KNOWN", map[string]snapshot.FactValue{"state": enum(fmt.Sprintf("V%d", i)), "other": enum("X")}))
	}
	want := digest(t, facts)
	r := rand.New(rand.NewSource(3652)) // #nosec G404 -- a fixed seed for a reproducible test, not a secret
	for n := 0; n < 100; n++ {
		shuffled := append([]snapshot.Fact(nil), facts...)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := digest(t, shuffled); got != want {
			t.Fatalf("shuffle %d: %s, want %s", n, got, want)
		}
	}
}

// A365 refined, INV-25: a change of any mapped fact is another digest: an amount, a status, a currency, a direction, the entity, a field's name or its kind, a field more or fewer, a
// fact more or fewer.
func TestFactsDigestChangesWithAnyMappedFact(t *testing.T) {
	base := digest(t, f3())
	edit := func(f func(*snapshot.Fact)) []snapshot.Fact {
		facts := f3()
		fields := map[string]snapshot.FactValue{}
		for k, v := range facts[0].Fields {
			fields[k] = v
		}
		facts[0].Fields = fields
		f(&facts[0])
		return facts
	}
	for name, facts := range map[string][]snapshot.Fact{
		"amount": edit(func(x *snapshot.Fact) {
			x.Fields["amount"] = snapshot.FactValue{Kind: snapshot.KindInteger, Text: "150001"}
		}),
		"status": edit(func(x *snapshot.Fact) { x.Status = "STALE" }),
		"currency": edit(func(x *snapshot.Fact) {
			x.Fields["currency"] = snapshot.FactValue{Kind: snapshot.KindString, Text: "USD"}
		}),
		"direction":      edit(func(x *snapshot.Fact) { x.Fields["direction"] = enum("CREDIT") }),
		"entity":         edit(func(x *snapshot.Fact) { x.Entity = "payments" }),
		"a field's name": edit(func(x *snapshot.Fact) { x.Fields["amount2"] = x.Fields["amount"]; delete(x.Fields, "amount") }),
		"a field's kind": edit(func(x *snapshot.Fact) {
			x.Fields["direction"] = snapshot.FactValue{Kind: snapshot.KindString, Text: "DEBIT"}
		}),
		"a field dropped":  edit(func(x *snapshot.Fact) { delete(x.Fields, "ref") }),
		"a field added":    edit(func(x *snapshot.Fact) { x.Fields["extra"] = enum("Y") }),
		"a fact added":     append(f3(), f4()[0]),
		"the fact dropped": f4(),
	} {
		if got := digest(t, facts); got == base {
			t.Errorf("%s changed and the digest did not", name)
		}
	}
}

// A365 refined: a value is typed, so the same text of another kind is another digest, and a value does not run into its neighbour (the boundary between name, kind and text, and between
// two fields, is kept).
func TestFactsDigestKeepsKindsAndFieldBoundariesApart(t *testing.T) {
	asString := digest(t, []snapshot.Fact{fact("e", "KNOWN", map[string]snapshot.FactValue{"f": {Kind: snapshot.KindString, Text: "A"}})})
	asEnum := digest(t, []snapshot.Fact{fact("e", "KNOWN", map[string]snapshot.FactValue{"f": enum("A")})})
	if asString == asEnum {
		t.Error("a string and an enum of the same text gave one digest")
	}
	x := digest(t, []snapshot.Fact{fact("e", "KNOWN", map[string]snapshot.FactValue{"ab": enum("c")})})
	y := digest(t, []snapshot.Fact{fact("e", "KNOWN", map[string]snapshot.FactValue{"a": enum("bc")})})
	if x == y {
		t.Error("the boundary between a field's name and its text was not kept")
	}
	p := digest(t, []snapshot.Fact{fact("e", "KNOWN", map[string]snapshot.FactValue{"a": enum("1"), "b": enum("2")})})
	q := digest(t, []snapshot.Fact{fact("e", "KNOWN", map[string]snapshot.FactValue{"a": enum("2"), "b": enum("1")})})
	if p == q {
		t.Error("a different pairing of names and values gave one digest")
	}
	if digest(t, []snapshot.Fact{fact("ab", "KNOWN", nil)}) == digest(t, []snapshot.Fact{fact("a", "bKNOWN", nil)}) {
		t.Error("the boundary between entity and status was not kept")
	}
}

// A365 refined: domain separation. The digest of facts is never the set hash or the content hash of strings that look like them, and the pinned vectors reproduce the tag.
func TestFactsDigestIsDomainSeparated(t *testing.T) {
	got := digest(t, f1())
	set, err := snapshot.SetHash([]snapshot.Pair{{EvidenceID: "debit", PayloadHash: ph(1)}})
	if err != nil {
		t.Fatal(err)
	}
	if got == set {
		t.Error("the facts digest is a set hash")
	}
	if got != vecF1 {
		t.Errorf("the pinned vector (domain tag included) is not reproduced: %s", got)
	}
}

// A365 refined: what cannot be digested is refused, never hashed: no facts, an empty entity, status, field name or kind, a kind that is not one of the seven, a value that is not the
// canonical text of its kind (a float, a leading zero, a plus sign, an exponent, a different case of a boolean), a repeated entity.
func TestFactsDigestRefusesWhatCannotBeDigested(t *testing.T) {
	one := func(f snapshot.Fact) []snapshot.Fact { return []snapshot.Fact{f} }
	val := func(kind, text string) []snapshot.Fact {
		return one(fact("e", "KNOWN", map[string]snapshot.FactValue{"f": {Kind: kind, Text: text}}))
	}
	for name, facts := range map[string][]snapshot.Fact{
		"no facts":              nil,
		"an empty entity":       one(fact("", "KNOWN", nil)),
		"an empty status":       one(fact("e", "", nil)),
		"an empty field name":   one(fact("e", "KNOWN", map[string]snapshot.FactValue{"": enum("A")})),
		"an empty kind":         val("", "A"),
		"an unknown kind":       val("float", "1.5"),
		"an empty enum":         val(snapshot.KindEnum, ""),
		"an empty reference":    val(snapshot.KindReference, ""),
		"a float integer":       val(snapshot.KindInteger, "1.0"),
		"an exponent integer":   val(snapshot.KindInteger, "1e3"),
		"a leading zero":        val(snapshot.KindInteger, "01"),
		"a plus sign":           val(snapshot.KindInteger, "+1"),
		"minus zero":            val(snapshot.KindInteger, "-0"),
		"an empty integer":      val(snapshot.KindInteger, ""),
		"beyond int64":          val(snapshot.KindInteger, "9223372036854775808"),
		"a float timestamp":     val(snapshot.KindTimestamp, "1763463600.5"),
		"a float duration":      val(snapshot.KindDuration, "1.5"),
		"a capital boolean":     val(snapshot.KindBoolean, "True"),
		"a numeric boolean":     val(snapshot.KindBoolean, "1"),
		"a repeated entity":     {fact("e", "KNOWN", nil), fact("e", "KNOWN", nil)},
		"one bad among good":    append(f2(), fact("", "KNOWN", nil)),
		"an entity with spaces": one(fact(strings.Repeat(" ", 3), "KNOWN", nil)),
	} {
		got, err := snapshot.FactsDigest(facts)
		if name == "an entity with spaces" { // not refused: the function does not judge names, only emptiness
			if err != nil || got == "" {
				t.Errorf("%s: %q, %v; want a digest", name, got, err)
			}
			continue
		}
		if !errors.Is(err, snapshot.ErrFactsInput) || got != "" {
			t.Errorf("%s: %q, %v; want ErrFactsInput and no digest", name, got, err)
		}
	}
	for _, ok := range []string{"0", "-1", "9223372036854775807", "-9223372036854775808"} {
		if _, err := snapshot.FactsDigest(val(snapshot.KindInteger, ok)); err != nil {
			t.Errorf("integer %q is canonical and was refused: %v", ok, err)
		}
	}
}

// The input is not modified.
func TestFactsDigestDoesNotModifyItsInput(t *testing.T) {
	facts := f2()
	digest(t, facts)
	if facts[0].Entity != "switch" || facts[1].Entity != "debit" || len(facts[0].Fields) != 1 {
		t.Error("FactsDigest reordered or changed its input")
	}
}
