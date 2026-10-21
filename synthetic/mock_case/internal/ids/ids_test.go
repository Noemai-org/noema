package ids

import (
	"regexp"
	"testing"
	"time"
)

var ulidRE = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)

func TestULIDShapeAndDeterminism(t *testing.T) {
	at := time.Date(2026, 10, 1, 8, 40, 0, 0, time.UTC)
	a := ULID(42, "case", 1, at)
	if !ulidRE.MatchString(a) {
		t.Fatalf("not a ULID: %s", a)
	}
	if a != ULID(42, "case", 1, at) {
		t.Fatal("same seed, kind, counter and time must give the same ULID")
	}
	for name, other := range map[string]string{
		"seed": ULID(43, "case", 1, at), "kind": ULID(42, "evidence", 1, at), "counter": ULID(42, "case", 2, at),
	} {
		if other[10:] == a[10:] {
			t.Errorf("changing the %s must change the random part", name)
		}
	}
}

func TestULIDSortsByTimeThenIsUniqueAcrossCounters(t *testing.T) {
	early := ULID(1, "case", 5, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	late := ULID(1, "case", 1, time.Date(2026, 10, 1, 0, 0, 1, 0, time.UTC))
	if !(early < late) {
		t.Fatalf("ULIDs must sort by time: %s !< %s", early, late)
	}
	seen := map[string]bool{}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for c := int64(1); c <= 5000; c++ {
		id := CaseID(7, c, at)
		if seen[id] {
			t.Fatalf("duplicate id at counter %d", c)
		}
		seen[id] = true
	}
}

func TestKnownEncoding(t *testing.T) {
	// The timestamp part of a ULID is the standard Crockford encoding of the millisecond count.
	if got := ULID(0, "x", 0, time.UnixMilli(0).UTC())[:10]; got != "0000000000" {
		t.Fatalf("epoch must encode as zeros, got %s", got)
	}
	if got := ULID(0, "x", 0, time.UnixMilli(1469918176385).UTC())[:10]; got != "01ARYZ6S41" {
		t.Fatalf("got %s, want 01ARYZ6S41 (the ULID specification's example time)", got)
	}
}

func TestReferences(t *testing.T) {
	at := time.Date(2026, 10, 1, 8, 40, 0, 0, time.UTC)
	if got := CaseReference(at, 1); got != "CMP-2026-00001" {
		t.Fatal(got)
	}
	if got := ComplaintRef(at, 12); got != "CPR-2026-00012" {
		t.Fatal(got)
	}
}
