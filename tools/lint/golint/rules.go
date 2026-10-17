package main

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
)

var moneyAreas = map[string]bool{"trust": true, "execution": true, "outcome": true, "record": true}

// moneyName matches names that hold money: *_minor, *Minor, amount*.
var moneyName = regexp.MustCompile(`(?i)^(amount.*|.*minor)$`)

func isFloatType(e ast.Expr) bool {
	name := identName(e)
	return name == "float32" || name == "float64"
}

// astRules runs the rules that read syntax trees.
func (l *linter) astRules() {
	for _, f := range l.files {
		l.clockRule(f)
		l.moneyRules(f)
		l.termRules(f)
		l.crossServiceRule(f)
		l.buildTagRule(f)
		if decisionAreas[f.area] && !f.test {
			l.thresholdRule(f)
		}
	}
}

// clockRule: time only through platform/clock (INV-21).
func (l *linter) clockRule(f *srcFile) {
	if f.area == "platform/clock" {
		return
	}
	local := ""
	for _, spec := range f.file.Imports {
		if unquote(spec.Path) == "time" {
			local = "time"
			if spec.Name != nil {
				local = spec.Name.Name
			}
		}
	}
	if local == "" || local == "_" || local == "." {
		return
	}
	ast.Inspect(f.file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if ok && identName(sel.X) == local && (sel.Sel.Name == "Now" || sel.Sel.Name == "Since" || sel.Sel.Name == "Until") {
			l.add("clock", f, sel.Pos(), "time.%s outside platform/clock", sel.Sel.Name)
		}
		return true
	})
}

// moneyRules: no floats in the four deciding areas, and none on money fields anywhere (BD-08, SoT money rules).
func (l *linter) moneyRules(f *srcFile) {
	ast.Inspect(f.file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if moneyAreas[f.area] && (v.Name == "float32" || v.Name == "float64") {
				l.add("money-float-area", f, v.Pos(), "%s in %s", v.Name, f.area)
			}
		case *ast.BasicLit:
			if moneyAreas[f.area] && v.Kind == token.FLOAT {
				l.add("money-float-area", f, v.Pos(), "float literal %s in %s", v.Value, f.area)
			}
		case *ast.Field:
			if isFloatType(v.Type) {
				for _, name := range v.Names {
					if moneyName.MatchString(name.Name) {
						l.add("money-float-field", f, name.Pos(), "money field %q has a float type", name.Name)
					}
				}
			}
		case *ast.ValueSpec:
			if v.Type != nil && isFloatType(v.Type) {
				for _, name := range v.Names {
					if moneyName.MatchString(name.Name) {
						l.add("money-float-field", f, name.Pos(), "money value %q has a float type", name.Name)
					}
				}
			}
		case *ast.TypeSpec:
			if st, ok := v.Type.(*ast.StructType); ok && v.Name.Name == "Quantity" {
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						if strings.EqualFold(name.Name, "value") && isFloatType(field.Type) {
							l.add("money-float-field", f, name.Pos(), "Quantity.%s has a float type", name.Name)
						}
					}
				}
			}
		}
		return true
	})
}

// eachText calls fn for every identifier and string literal. Comments are never visited.
func eachText(f *srcFile, fn func(pos token.Pos, text string)) {
	ast.Inspect(f.file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			fn(v.Pos(), v.Name)
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				fn(v.Pos(), unquote(v))
			}
		}
		return true
	})
}

// termRules: forbidden industry terms and benchmark-aware branches in core.
func (l *linter) termRules(f *srcFile) {
	eachText(f, func(pos token.Pos, text string) {
		if term, ok := matchTerm(text, coreForbiddenTerms); ok {
			l.add("forbidden-term", f, pos, "forbidden core term %q in %q", term, text)
		}
		if term, ok := matchTerm(text, benchmarkTerms); ok {
			l.add("benchmark-branch", f, pos, "benchmark-aware term %q in %q", term, text)
		}
	})
}

// buildTagRule: a build constraint naming a benchmark is a benchmark branch.
func (l *linter) buildTagRule(f *srcFile) {
	for _, group := range f.file.Comments {
		if group.Pos() > f.file.Package {
			break
		}
		for _, c := range group.List {
			if strings.HasPrefix(c.Text, "//go:build") || strings.HasPrefix(c.Text, "// +build") {
				if _, ok := matchTerm(c.Text, []string{"benchmark", "bench"}); ok {
					l.add("benchmark-branch", f, c.Pos(), "build constraint names a benchmark: %s", c.Text)
				}
			}
		}
	}
}

// crossServiceRule: core never references the model service.
func (l *linter) crossServiceRule(f *srcFile) {
	eachText(f, func(pos token.Pos, text string) {
		if strings.Contains(text, "services/model") || strings.Contains(text, "noema_model") {
			l.add("cross-service", f, pos, "core references the model service: %q", text)
		}
	})
}

// thresholdRule: decision code holds no threshold literals (REQ-OPS-003). Only 0 and 1 are allowed;
// every other number comes from platform/config.
func (l *linter) thresholdRule(f *srcFile) {
	ast.Inspect(f.file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if ok && (lit.Kind == token.INT || lit.Kind == token.FLOAT) && lit.Value != "0" && lit.Value != "1" {
			l.add("threshold-literal", f, lit.Pos(), "numeric literal %s in decision code; read it from platform/config", lit.Value)
		}
		return true
	})
}
