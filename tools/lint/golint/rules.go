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
		if !f.test {
			l.crashHookRule(f)
		}
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

// crashHookName is the field the crash-point matrix uses to stop a transaction or a step at a named point (execution/ledger, execution/controller, outcome/verifier). Production
// leaves it nil: a hook that is set in a running service lets a caller choose where the process "dies".
const crashHookName = "Boundary"

// crashHookRule: the crash hook is installed in test files only (INV-03, INV-07; noema#553). Reading, calling or declaring it is how it works and is not flagged. In a file that is not a
// _test.go file these are flagged: an assignment to a field named Boundary (also through parentheses), taking its address (a pointer can set it later), a composite-literal key that sets it,
// and a positional composite literal of a struct that declares it. Setting it to nil is never flagged.
func (l *linter) crashHookRule(f *srcFile) {
	for _, pos := range crashHookSites(f.file, l.hookTypes()) {
		l.add("crash-hook", f, pos, "the crash hook %q is installed in non-test code; only _test.go files may set it", crashHookName)
	}
}

// hookIndex maps the name of a struct type that declares a field named Boundary to the position of that field among the type's fields (an unkeyed literal lists them in order; a
// field declared with several names counts once per name, an embedded field once).
type hookIndex map[string][]int

// hookTypes reads every parsed file of the core once (the declarations are in non-test files; a type declared in a test file is a test's own).
func (l *linter) hookTypes() hookIndex {
	if l.hooks == nil {
		files := make([]*ast.File, 0, len(l.files))
		for _, f := range l.files {
			files = append(files, f.file)
		}
		l.hooks = hookTypesOf(files)
	}
	return l.hooks
}

func hookTypesOf(files []*ast.File) hookIndex {
	out := hookIndex{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			position := 0
			for _, field := range st.Fields.List {
				width := len(field.Names)
				if width == 0 {
					width = 1
				}
				for i, name := range field.Names {
					if name.Name == crashHookName {
						out[spec.Name.Name] = append(out[spec.Name.Name], position+i)
					}
				}
				position += width
			}
			return true
		})
	}
	return out
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// typeNameOf is the bare name of the type of a composite literal: `L` or `pkg.L` (a pointer or an array element is the caller's business).
func typeNameOf(e ast.Expr) string {
	switch t := unparen(e).(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.StarExpr:
		return typeNameOf(t.X) // []*L{{...}}: the elided literal is an L
	}
	return ""
}

// crashHookSites returns the positions where the file sets a field named Boundary to something other than nil. `types` is the index of the struct types that declare it.
func crashHookSites(file *ast.File, types hookIndex) []token.Pos {
	var out []token.Pos
	isNil := func(e ast.Expr) bool { return identName(unparen(e)) == "nil" }
	isHook := func(e ast.Expr) bool {
		sel, ok := unparen(e).(*ast.SelectorExpr)
		return ok && sel.Sel.Name == crashHookName
	}
	// positional checks one composite literal whose type name is known; elements of a keyed literal are the KeyValueExpr case below.
	positional := func(name string, lit *ast.CompositeLit) {
		for _, index := range types[name] {
			if index < len(lit.Elts) {
				if _, keyed := lit.Elts[index].(*ast.KeyValueExpr); !keyed && !isNil(lit.Elts[index]) {
					out = append(out, lit.Elts[index].Pos())
				}
			}
		}
	}
	elided := map[*ast.CompositeLit]string{} // `[]L{{...}}` and `map[K]L{k: {...}}`: the inner literal has no type of its own
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				if !isHook(lhs) {
					continue
				}
				if len(v.Lhs) == len(v.Rhs) && isNil(v.Rhs[i]) {
					continue
				}
				out = append(out, unparen(lhs).Pos())
			}
		case *ast.UnaryExpr:
			if v.Op == token.AND && isHook(v.X) {
				out = append(out, v.Pos()) // a pointer to the hook can set it later, anywhere
			}
		case *ast.KeyValueExpr:
			if identName(v.Key) == crashHookName && !isNil(v.Value) {
				out = append(out, v.Pos())
			}
		case *ast.CompositeLit:
			name := typeNameOf(v.Type)
			if v.Type == nil {
				name = elided[v]
			}
			switch t := v.Type.(type) {
			case *ast.ArrayType:
				for _, e := range v.Elts {
					if inner, ok := e.(*ast.CompositeLit); ok && inner.Type == nil {
						elided[inner] = typeNameOf(t.Elt)
					}
				}
			case *ast.MapType:
				for _, e := range v.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						if inner, ok := kv.Value.(*ast.CompositeLit); ok && inner.Type == nil {
							elided[inner] = typeNameOf(t.Value)
						}
					}
				}
			}
			if name != "" {
				positional(name, v)
			}
		}
		return true
	})
	return out
}
