package ids_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strings"
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
		id := mustID(t, g, p)
		if !shape.MatchString(id) {
			t.Fatalf("%q does not match <prefix>_<26 Crockford characters>", id)
		}
	}
	if !regexp.MustCompile(`^rcp_[0-9A-Za-z]{2,32}$`).MatchString(mustReceipt(t, g)) {
		t.Fatal("a receipt ID does not match the rcp pattern of common.v1")
	}
}

// ULIDs sort by time (PERD 32.3), and the same clock and entropy give the same ID (INV-21).
func TestIDsSortByTimeAndAreReproducible(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	g := ids.New(clk, bytes.NewReader(bytes.Repeat([]byte{7}, 100)))
	var made []string
	for i := 0; i < 5; i++ {
		made = append(made, mustID(t, g, "evt"))
		clk.Advance(2 * time.Millisecond)
	}
	if !sort.StringsAreSorted(made) {
		t.Fatalf("IDs made at increasing times do not sort: %v", made)
	}
	a := ids.New(clock.NewManual(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)), bytes.NewReader(bytes.Repeat([]byte{9}, 10)))
	b := ids.New(clock.NewManual(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)), bytes.NewReader(bytes.Repeat([]byte{9}, 10)))
	if mustID(t, a, "rcp") != mustID(t, b, "rcp") {
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

// An entropy failure is a typed error from ID and ReceiptID, never a panic (INV-07; the controller refuses on it).
func TestAnEntropyFailureIsAnErrorNotAPanic(t *testing.T) {
	g := ids.New(clock.NewManual(time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)), bytes.NewReader(nil))
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if id, err := g.ID("rcp"); err == nil || id != "" {
		t.Fatalf("ID = %q, %v; want an error and no ID", id, err)
	}
	if id, err := g.ReceiptID(); err == nil || id != "" {
		t.Fatalf("ReceiptID = %q, %v; want an error and no ID", id, err)
	}
}

// Nothing in the package panics: a source scan, so a later Must cannot come back unnoticed.
func TestThePackageHasNoPanic(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "panic" {
					t.Errorf("%s calls panic", e.Name())
				}
			}
			return true
		})
	}
}

func mustID(t *testing.T, g *ids.Generator, prefix string) string {
	t.Helper()
	id, err := g.ID(prefix)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustReceipt(t *testing.T, g *ids.Generator) string {
	t.Helper()
	id, err := g.ReceiptID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
