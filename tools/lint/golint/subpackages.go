package main

import (
	"go/ast"
	"path"
	"reflect"
	"strconv"
	"strings"
)

// Sub-package rules (T1 of DEC-2026-10-20; PERD §64.2 as amended by PERD v1.11). Each one has a canary
// under tools/lint/canaries that fires this rule and no other.

func underDir(dir, prefix string) bool {
	return dir == prefix || strings.HasPrefix(dir, prefix+"/")
}

// candidateFields are the only names a candidates-API response may carry (REQ-CTX-006), compared
// case-insensitively with underscores removed, so CandidateID, candidate_id and candidateId are one name.
var candidateFields = map[string]bool{
	"candidates": true, "candidateid": true, "resourceref": true, "amountminor": true, "currency": true,
	"occurredat": true, "counterpartydisplay": true, "accountlast4": true, "direction": true,
	"beneficiaryaliasmatch": true,
}

func normaliseField(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

// predicateBannedImports are imports that perform I/O or break determinism; the predicate package has none (SoT §43.7).
func predicateBannedImport(p string) bool {
	switch p {
	case "os", "os/exec", "os/signal", "io/ioutil", "net", "database/sql", "syscall", "log", "log/slog", "math/rand", "math/rand/v2", "crypto/rand":
		return true
	}
	return strings.HasPrefix(p, "net/http") || strings.HasPrefix(p, "net/rpc") || strings.HasPrefix(p, "net/smtp")
}

func (l *linter) subpackageRules() {
	byDir := l.importsByDir()
	for _, f := range l.files {
		dir := path.Dir(f.rel)
		if underDir(dir, "outcome/verifier") {
			l.verifierRule(f, byDir)
		}
		if underDir(dir, "internal/predicate") {
			l.predicateRule(f, byDir)
		}
		if underDir(dir, "evidence/candidates_api") && !f.test {
			l.candidatesShapeRule(f)
		}
		if !f.test {
			l.constructRule(f, dir)
		}
	}
}

// verifierRule: the verifier reads for itself and never reuses Trust's work (INV-04, REQ-VER-001). It imports
// nothing under trust/ or evidence/, directly or through other packages.
func (l *linter) verifierRule(f *srcFile, byDir map[string][]imp) {
	for _, spec := range f.file.Imports {
		ipath := unquote(spec.Path)
		dir, local := l.inModuleDir(ipath)
		if !local {
			continue
		}
		reported := false
		l.reach(byDir, dir, func(d string) {
			if !reported && (underDir(d, "trust") || underDir(d, "evidence")) {
				reported = true
				l.add("verifier-independence", f, spec.Pos(), "outcome/verifier reaches %s through %q; the verifier uses its own reads", d, ipath)
			}
		}, nil)
	}
}

// predicateRule: the predicate language is pure (SoT §43.7). It imports nothing from trust, execution,
// outcome or record, and performs no I/O.
func (l *linter) predicateRule(f *srcFile, byDir map[string][]imp) {
	for _, spec := range f.file.Imports {
		ipath := unquote(spec.Path)
		if dir, local := l.inModuleDir(ipath); local {
			reported := false
			l.reach(byDir, dir, func(d string) {
				if !reported && (underDir(d, "trust") || underDir(d, "execution") || underDir(d, "outcome") || underDir(d, "record")) {
					reported = true
					l.add("predicate-purity", f, spec.Pos(), "internal/predicate reaches %s through %q", d, ipath)
				}
			}, nil)
		} else if predicateBannedImport(ipath) {
			l.add("predicate-purity", f, spec.Pos(), "internal/predicate imports %q; the predicate language performs no I/O and is deterministic", ipath)
		}
	}
	ast.Inspect(f.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && identName(sel.X) == "fmt" && strings.HasPrefix(sel.Sel.Name, "Print") {
			l.add("predicate-purity", f, call.Pos(), "fmt.%s writes to standard output", sel.Sel.Name)
		}
		return true
	})
}

// candidatesShapeRule: a candidates-API response carries only the masked candidate shape (REQ-CTX-006).
// Struct types named *Response or *Candidate are checked field by field; they must be declared here.
func (l *linter) candidatesShapeRule(f *srcFile) {
	ast.Inspect(f.file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || !(strings.HasSuffix(ts.Name.Name, "Response") || strings.HasSuffix(ts.Name.Name, "Candidate")) {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			l.add("candidates-api-shape", f, ts.Pos(), "%s must be a struct declared here, so its fields can be checked", ts.Name.Name)
			return true
		}
		for _, field := range st.Fields.List {
			if len(field.Names) == 0 {
				l.add("candidates-api-shape", f, field.Pos(), "%s embeds a type; list the masked fields explicitly", ts.Name.Name)
				continue
			}
			for _, name := range field.Names {
				if !candidateFields[normaliseField(name.Name)] {
					l.add("candidates-api-shape", f, name.Pos(), "%s has field %q outside the masked candidate shape", ts.Name.Name, name.Name)
				}
			}
			if field.Tag != nil {
				tag, err := strconv.Unquote(field.Tag.Value)
				if err == nil {
					if j := reflect.StructTag(tag).Get("json"); j != "" {
						if jn := strings.Split(j, ",")[0]; jn != "" && jn != "-" && !candidateFields[normaliseField(jn)] {
							l.add("candidates-api-shape", f, field.Pos(), "%s has json name %q outside the masked candidate shape", ts.Name.Name, jn)
						}
					}
				}
			}
		}
		return true
	})
}

// constructors maps a type that only one package may construct to that package's directory.
var constructors = map[string]string{"DecisionRecord": "trust/decide", "DecisionGrant": "trust/grants"}

func typeName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

// constructRule: nothing outside trust/decide builds a decision and nothing outside trust/grants builds a
// grant (REQ-TRU-001, REQ-TRU-008, AGENT-22). A composite literal or new() of the type is a construction;
// reading one back from storage (json.Unmarshal into a variable) is not. Generated definitions and test files are exempt.
func (l *linter) constructRule(f *srcFile, dir string) {
	if underDir(dir, "internal/contracts") {
		return
	}
	check := func(name string, pos ast.Node) {
		owner, ok := constructors[name]
		if ok && !underDir(dir, owner) {
			l.add("construct-decision-grant", f, pos.Pos(), "%s constructed in %s; only %s may construct it", name, dir, owner)
		}
	}
	ast.Inspect(f.file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CompositeLit:
			if v.Type != nil {
				check(typeName(v.Type), v)
			}
		case *ast.CallExpr:
			if identName(v.Fun) == "new" && len(v.Args) == 1 {
				check(typeName(v.Args[0]), v)
			}
		}
		return true
	})
}
