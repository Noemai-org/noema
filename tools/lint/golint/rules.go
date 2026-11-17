package main

import (
	"go/ast"
	"go/token"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var moneyAreas = map[string]bool{"trust": true, "execution": true, "outcome": true, "record": true}

// moneyName matches names that hold money: *_minor, *Minor, amount*.
var moneyName = regexp.MustCompile(`(?i)^(amount.*|.*minor)$`)

// isDecimalType reports a decimal or arbitrary-precision type: Decimal, decimal.Decimal, big.Float, big.Rat, also behind a pointer
// (SoT money rules M4: money is an integer in minor units, never a decimal).
func isDecimalType(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.StarExpr:
		return isDecimalType(v.X)
	case *ast.Ident:
		return v.Name == "Decimal"
	case *ast.SelectorExpr:
		pkg := identName(v.X)
		return v.Sel.Name == "Decimal" || (pkg == "big" && (v.Sel.Name == "Float" || v.Sel.Name == "Rat"))
	}
	return false
}

// mentionsMoney reports whether an expression names a money value anywhere inside it.
func mentionsMoney(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && moneyName.MatchString(id.Name) {
			found = true
		}
		return !found
	})
	return found
}

// isPowerOfTen reports a literal 10, 100, 1000 ... (also 1e2 or 100.0) or math.Pow10 / math.Pow(10, n): a literal exponent multiplier.
func isPowerOfTen(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return isPowerOfTen(v.X)
	case *ast.BasicLit:
		if v.Kind != token.INT && v.Kind != token.FLOAT {
			return false
		}
		f, err := strconv.ParseFloat(strings.ReplaceAll(v.Value, "_", ""), 64)
		if err != nil || f < 10 {
			return false
		}
		exp := math.Log10(f)
		return math.Abs(exp-math.Round(exp)) < 1e-9
	case *ast.CallExpr:
		sel, ok := v.Fun.(*ast.SelectorExpr)
		if !ok || identName(sel.X) != "math" {
			return false
		}
		if sel.Sel.Name == "Pow10" {
			return true
		}
		return sel.Sel.Name == "Pow" && len(v.Args) == 2 && isPowerOfTen(v.Args[0])
	}
	return false
}

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
		case *ast.BinaryExpr:
			if v.Op == token.MUL || v.Op == token.QUO || v.Op == token.REM {
				if (mentionsMoney(v.X) && isPowerOfTen(v.Y)) || (mentionsMoney(v.Y) && isPowerOfTen(v.X)) {
					l.add("money-literal-multiplier", f, v.OpPos, "a literal power of ten on a money value; the exponent comes from contracts/currencies.json (M1)")
				}
			}
		case *ast.Field:
			if isDecimalType(v.Type) {
				for _, name := range v.Names {
					if moneyName.MatchString(name.Name) {
						l.add("money-decimal-field", f, name.Pos(), "money field %q has a decimal type", name.Name)
					}
				}
			}
			if isFloatType(v.Type) {
				for _, name := range v.Names {
					if moneyName.MatchString(name.Name) {
						l.add("money-float-field", f, name.Pos(), "money field %q has a float type", name.Name)
					}
				}
			}
		case *ast.ValueSpec:
			if v.Type != nil && isDecimalType(v.Type) {
				for _, name := range v.Names {
					if moneyName.MatchString(name.Name) {
						l.add("money-decimal-field", f, name.Pos(), "money value %q has a decimal type", name.Name)
					}
				}
			}
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
