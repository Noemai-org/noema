package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func sites(t *testing.T, src string) int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", "package x\n"+src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return len(crashHookSites(file, hookTypesOf([]*ast.File{file})))
}

// A crash hook is installed in non-test code: the harness of the crash matrix is the only caller allowed to do that, and it lives in _test.go files (noema#553, my note 1).
func TestCrashHookInstalledIsFound(t *testing.T) {
	cases := map[string]string{
		"assignment":                  `func f(l *L) { l.Boundary = func(P) error { return nil } }`,
		"assignment through a paren":  `func f(l *L) { (*l).Boundary = hook }`,
		"one of several assignments":  `func f(l *L) { l.Other, l.Boundary = 1, hook }`,
		"a composite literal":         `var c = Config{Boundary: hook}`,
		"a pointer literal":           `func f() *L { return &L{Boundary: func(P) error { return nil }} }`,
		"a literal in an argument":    `func f() { run(Config{Boundary: hook}) }`,
		"a copy of another hook":      `func f(l, m *L) { l.Boundary = m.Boundary }`,
		"nil in the wrong position":   `func f(l *L) { l.Other, l.Boundary = nil, hook }`,
		"a hook through an index key": `var cs = []Config{{Boundary: hook}}`,
	}
	for name, src := range cases {
		if n := sites(t, src); n != 1 {
			t.Errorf("%s: %d sites, want 1", name, n)
		}
	}
}

// Declaring the hook, calling it, comparing it with nil and clearing it are not installing one.
func TestCrashHookDeclaredCalledOrClearedIsNotFound(t *testing.T) {
	cases := map[string]string{
		"a field declaration":          `type L struct{ Boundary func(P) error }`,
		"a call":                       `func f(l *L, p P) error { return l.Boundary(p) }`,
		"a nil comparison":             `func f(l *L) bool { return l.Boundary == nil }`,
		"a read":                       `func f(l *L) func(P) error { h := l.Boundary; return h }`,
		"an explicit nil":              `func f(l *L) { l.Boundary = nil }`,
		"a nil literal":                `var c = Config{Boundary: nil}`,
		"another name":                 `func f(l *L) { l.Boundaries = hook; l.boundary = hook; _ = Config{Boundaryx: hook} }`,
		"a method named Boundary":      `func (l *L) Boundary() {}`,
		"a local variable named so":    `func f() { Boundary := hook; _ = Boundary }`,
		"a map key named Boundary str": `var m = map[string]int{"Boundary": 1}`,
	}
	for name, src := range cases {
		if n := sites(t, src); n != 0 {
			t.Errorf("%s: %d sites, want 0", name, n)
		}
	}
}

// The forms C1 found that the first version of the rule missed (Codex P2s, verified by C1 on noema#628): a parenthesised target, a pointer to the field, and a positional literal.
const hookType = "type L struct{ A int; Boundary func(P) error }\n"

func TestCrashHookInstalledThroughAParenAPointerOrAPositionalLiteralIsFound(t *testing.T) {
	cases := map[string]string{
		"a parenthesised target":              `func f(l *L) { (l.Boundary) = hook }`,
		"a doubly parenthesised target":       `func f(l *L) { ((l.Boundary)) = hook }`,
		"the address of the field":            `func f(l *L) { p := &l.Boundary; *p = hook }`,
		"the address, parenthesised":          `func f(l *L) { _ = (&(l.Boundary)) }`,
		"a positional literal":                `var v = L{1, hook}`,
		"a positional literal of pkg.L":       `var v = pkg.L{1, hook}`,
		"a positional pointer literal":        `var v = &L{1, hook}`,
		"an elided literal in a slice":        `var v = []L{{1, hook}}`,
		"an elided literal in a pointer list": `var v = []*L{{1, hook}}`,
		"an elided literal in a map":          `var v = map[string]L{"a": {1, hook}}`,
		"a positional literal in a call":      `func f() { run(L{2, func(P) error { return nil }}) }`,
	}
	for name, src := range cases {
		if n := sites(t, hookType+src); n != 1 {
			t.Errorf("%s: %d sites, want 1", name, n)
		}
	}
}

func TestCrashHookClearedOrNotDeclaredIsNotFoundInThoseForms(t *testing.T) {
	cases := map[string]string{
		"a positional literal with nil":    `var v = L{1, nil}`,
		"a positional literal short of it": `var v = L{1}`,
		"an elided literal with nil":       `var v = []L{{1, nil}}`,
		"a type that does not declare it":  `type M struct{ A int; B func(P) error }; var v = M{1, hook}`,
		"a parenthesised nil":              `func f(l *L) { (l.Boundary) = nil }`,
		"the address of another field":     `func f(l *L) { p := &l.A; *p = 1 }`,
		"a keyed literal with nil":         `var v = L{A: 1, Boundary: nil}`,
	}
	for name, src := range cases {
		if n := sites(t, hookType+src); n != 0 {
			t.Errorf("%s: %d sites, want 0", name, n)
		}
	}
}

// The index of a field is its position among ALL fields: several names in one declaration and an embedded field each take their place.
func TestTheHookIndexCountsEveryFieldBefore(t *testing.T) {
	src := "package x" + string(rune(10)) + "type L struct{ A, B int; C; Boundary func(P) error; D int }"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := hookTypesOf([]*ast.File{file})["L"]
	if len(got) != 1 || got[0] != 3 {
		t.Errorf("hook index %v, want [3]", got)
	}
}
