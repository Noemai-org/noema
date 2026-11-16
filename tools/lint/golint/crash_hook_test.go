package main

import (
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
	return len(crashHookSites(file))
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
