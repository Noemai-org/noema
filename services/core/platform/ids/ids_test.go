package ids_test

import (
	"bytes"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

var shape = regexp.MustCompile(`^[a-z]{2,6}_[0-9A-HJKMNP-TV-Z]{26}$`)

// PERD 32.3: <prefix>_<ULID>, and the common.v1 patterns for rcp_ and recev_ accept it.
func TestIDShapeAndCommonPatterns(t *testing.T) {
	g := ids.New(clock.NewManual(time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)), nil)
	for _, p := range []string{"rcp", "recev", "corr", "grt"} {
		id := g.Must(p)
		if !shape.MatchString(id) {
			t.Fatalf("%q does not match <prefix>_<26 Crockford characters>", id)
		}
	}
	if !regexp.MustCompile(`^rcp_[0-9A-Za-z]{2,32}$`).MatchString(g.ReceiptID()) {
		t.Fatal("a receipt ID does not match the rcp pattern of common.v1")
	}
}

// ULIDs sort by time (PERD 32.3), and the same clock and entropy give the same ID (INV-21).
func TestIDsSortByTimeAndAreReproducible(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	g := ids.New(clk, bytes.NewReader(bytes.Repeat([]byte{7}, 100)))
	var made []string
	for i := 0; i < 5; i++ {
		made = append(made, g.Must("evt"))
		clk.Advance(2 * time.Millisecond)
	}
	if !sort.StringsAreSorted(made) {
		t.Fatalf("IDs made at increasing times do not sort: %v", made)
	}
	a := ids.New(clock.NewManual(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)), bytes.NewReader(bytes.Repeat([]byte{9}, 10)))
	b := ids.New(clock.NewManual(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)), bytes.NewReader(bytes.Repeat([]byte{9}, 10)))
	if a.Must("rcp") != b.Must("rcp") {
		t.Fatal("same clock and entropy gave different IDs")
	}
}

func TestBadPrefixAndShortEntropyAreErrors(t *testing.T) {
	g := ids.New(clock.NewManual(time.Unix(0, 0)), bytes.NewReader([]byte{1, 2}))
	for _, p := range []string{"", "x", "TOOLONGP", "ab1", "a_b"} {
		if _, err := g.ID(p); err == nil {
			t.Fatalf("prefix %q was accepted", p)
		}
	}
	if _, err := g.ID("rcp"); err == nil {
		t.Fatal("a failed entropy source returned an ID")
	}
}
