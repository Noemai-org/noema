package candidaterank

// A725 (interim): CANDIDATES_RESOLVED is written as core-trust; its payload is integer basis points plus resolved_target; the recipient share of the v2 read is floored to basis points like
// every other feature (A560). REQ-AMB-002, REQ-AMB-003, INV-21, AGENT-13, A676. All data is SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// A725: the writer of the event is core-trust, and the event type and the writer are values of the Record's own closed sets. The package cannot import the Record (standard library only), so the
// test reads the two contract files: the identity must be a runtime emitter of record/chain/actors.json and the type a member of record-event.v3.json's enum.
func TestTheEventIsWrittenAsCoreTrustAndItsNamesAreTheRecordsOwn(t *testing.T) {
	if WrittenBy != "core-trust" || EventType != "CANDIDATES_RESOLVED" {
		t.Fatalf("WrittenBy %q and EventType %q, want core-trust and CANDIDATES_RESOLVED", WrittenBy, EventType)
	}
	raw, err := os.ReadFile("../../record/chain/actors.json")
	if err != nil {
		t.Fatal(err)
	}
	var actors struct {
		Emitters []struct{ Name, Scope string } `json:"emitters"`
	}
	if err := json.Unmarshal(raw, &actors); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range actors.Emitters {
		if e.Name == WrittenBy && e.Scope == "RUNTIME" {
			found = true
		}
	}
	if !found {
		t.Errorf("%s is not a RUNTIME emitter of actors.json", WrittenBy)
	}
	schema, err := os.ReadFile("../../../../contracts/schemas/record-event.v3.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(schema), `"`+EventType+`"`) {
		t.Errorf("%s is not an event type of record-event.v3.json", EventType)
	}
}

// A725, A676: resolved_target is the resolved candidate's own resource reference (the opaque `<vertical>.<kind>:<id>` of the read), named only for an AUTO_RESOLVED state. It is not the candidate's
// counterparty_ref (an ordinal that holds within one answer only and could be read as an identity across sets) and not a name.
func TestTheAuditPayloadNamesTheResolvedTargetOnlyForAnAutoResolve(t *testing.T) {
	c := goodConfig()
	cands, tgt := auditSet()
	res, err := c.Rank(cands, tgt, Context{})
	if err != nil {
		t.Fatal(err)
	}
	if p := res.AuditPayload(); p.ResolvedCandidateID != "cand_top" || p.ResolvedTarget != "test.txn:TOP0001" {
		t.Errorf("resolved %q target %q, want cand_top and test.txn:TOP0001 (the top candidate's own reference, not the other's)", p.ResolvedCandidateID, p.ResolvedTarget)
	}
	flagged, err := c.Rank(cands, tgt, Context{MaterialFlag: true})
	if err != nil {
		t.Fatal(err)
	}
	if p := flagged.AuditPayload(); p.ResolvedTarget != "" {
		t.Errorf("a set that did not resolve names the target %q", p.ResolvedTarget)
	}
	for _, st := range []State{NoCandidates, Ambiguous, TooManyCandidates} {
		if p := (Resolution{State: st, ResolvedID: "cand_x", ResolvedRef: "test.txn:X"}).AuditPayload(); p.ResolvedTarget != "" {
			t.Errorf("state %s: target %q", st, p.ResolvedTarget)
		}
	}
	// A candidate that carried no reference gives a payload with no resolved_target key at all (never an empty string).
	bare := []Candidate{{ID: "cand_bare", AmountMinor: 100000, Currency: "NGN", OccurredAt: time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC), RecipientBP: 10000, AliasMatch: true, Direction: "OUTGOING"}}
	one, err := c.Rank(bare, tgt, Context{})
	if err != nil {
		t.Fatal(err)
	}
	if one.State != AutoResolved {
		t.Fatalf("state %s, want AUTO_RESOLVED", one.State)
	}
	if raw := marshalAudit(t, one); strings.Contains(string(raw), "resolved_target") {
		t.Errorf("a candidate without a reference gives a resolved_target: %s", raw)
	}
}

// AGENT-13, A676: a resource reference that is not the contract's shape (`<vertical>.<kind>:<id>`) is refused whole, so that no name or free text can reach the Record through it.
func TestRankRefusesAResourceRefThatIsNotAReference(t *testing.T) {
	c := goodConfig()
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	for name, ref := range map[string]string{
		"a name": "Ada Obi", "no id": "test.txn:", "a space": "test.txn:TX 1", "no kind": "TX1", "upper vertical": "Test.txn:TX1",
		"a newline": "test.txn:TX1\nx", "too long": "test.txn:" + strings.Repeat("a", 65),
	} {
		if _, err := c.Rank([]Candidate{{ID: "cand_a", ResourceRef: ref, OccurredAt: at}}, Target{}, Context{}); !errors.Is(err, ErrCandidates) {
			t.Errorf("%s (%q): err = %v, want ErrCandidates", name, ref, err)
		}
	}
	for _, ref := range []string{"", "fin.transaction:TX88213", "test.txn:" + strings.Repeat("a", 64)} {
		if _, err := c.Rank([]Candidate{{ID: "cand_a", ResourceRef: ref, OccurredAt: at}}, Target{}, Context{}); err != nil {
			t.Errorf("%q: %v", ref, err)
		}
	}
}

// A725, A560: the exact share of the v2 read becomes basis points by floor division, in integers: never rounded, never through a float. A share below 0 or above 1, a zero or negative
// denominator, and a numerator or denominator the contract cannot hold are errors, never clamped.
func TestShareBPFloorsAnExactShareToBasisPoints(t *testing.T) {
	for _, tc := range []struct {
		name     string
		num, den int64
		want     int64
	}{
		{"zero", 0, 1, 0}, {"one", 1, 1, 10000}, {"one in a larger unit", 1000000000, 1000000000, 10000}, {"zero over a large unit", 0, 1000000000, 0},
		{"a third floors to 3333", 1, 3, 3333}, {"two thirds floors to 6666 (rounding gives 6667)", 2, 3, 6666},
		{"3499 of 10000", 3499, 10000, 3499}, {"34999 of 100000 floors to 3499", 34999, 100000, 3499}, {"35000 of 100000", 35000, 100000, 3500}, {"35001 of 100000", 35001, 100000, 3500},
		{"just under one", 9999999, 10000000, 9999}, {"just under one in the largest unit", 999999999, 1000000000, 9999},
		{"a share below one basis point", 1, 10001, 0}, {"one basis point", 1, 10000, 1},
		{"57 of 100 (a float gives 5699)", 57, 100, 5700}, {"29 of 100 (a float gives 2899)", 29, 100, 2900}, {"58 of 100", 58, 100, 5800},
		{"a half", 1, 2, 5000},
	} {
		got, err := ShareBP(tc.num, tc.den)
		if err != nil || got != tc.want {
			t.Errorf("%s: ShareBP(%d, %d) = %d, %v, want %d", tc.name, tc.num, tc.den, got, err, tc.want)
		}
	}
}

func TestShareBPRefusesWhatIsNotAShareOfOneOrLess(t *testing.T) {
	for _, tc := range []struct {
		name     string
		num, den int64
	}{
		{"over one", 2, 1}, {"just over one", 10001, 10000}, {"a share of one and a bit in large units", 1000000001, 1000000000},
		{"zero denominator", 1, 0}, {"zero over zero", 0, 0}, {"negative denominator", 1, -1}, {"negative numerator", -1, 1}, {"both negative", -1, -2},
		{"the largest integers", 9223372036854775807, 9223372036854775807 - 1},
	} {
		got, err := ShareBP(tc.num, tc.den)
		if !errors.Is(err, ErrShare) || got != 0 {
			t.Errorf("%s: ShareBP(%d, %d) = %d, %v, want 0 and ErrShare", tc.name, tc.num, tc.den, got, err)
		}
	}
	// The largest integers in a share of exactly one do not overflow.
	if got, err := ShareBP(9223372036854775807, 9223372036854775807); err != nil || got != 10000 {
		t.Errorf("max over max = %d, %v, want 10000", got, err)
	}
}

// A725, A560: whatever share is given, the answer is in [0, 10000] and is the floor: x*10000 >= got*den and x*10000 < (got+1)*den, checked in integers over a sweep of shares.
func TestShareBPIsTheFloorOverASweep(t *testing.T) {
	for den := int64(1); den <= 300; den++ {
		for num := int64(0); num <= den; num++ {
			got, err := ShareBP(num, den)
			if err != nil || got < 0 || got > UnitBP {
				t.Fatalf("ShareBP(%d, %d) = %d, %v", num, den, got, err)
			}
			if num*UnitBP < got*den || num*UnitBP >= (got+1)*den {
				t.Fatalf("ShareBP(%d, %d) = %d is not the floor", num, den, got)
			}
		}
	}
}
