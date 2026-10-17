package main

import (
	"go/ast"
	"path"
	"strings"
)

// decisionAreas are the three powers that never import each other (SoT §13, INV-01).
var decisionAreas = map[string]bool{"trust": true, "execution": true, "outcome": true}

// modelFreeAreas never import an explanation or model client (INV-01, INV-02).
var modelFreeAreas = map[string]bool{"trust": true, "execution": true, "outcome": true, "record": true}

var modelSegments = map[string]bool{"explanation": true, "modelclient": true, "providers": true, "voice": true, "understanding": true}

// imp is one import statement.
type imp struct {
	path string
	file *srcFile
	line int
}

func isModelClient(path string) bool {
	if strings.Contains(path, "services/model") {
		return true
	}
	for _, seg := range strings.Split(path, "/") {
		if modelSegments[seg] {
			return true
		}
	}
	return false
}

// inModuleDir returns the services/core-relative directory of an in-module import path.
func (l *linter) inModuleDir(path string) (string, bool) {
	if l.modulePath == "" {
		return "", false
	}
	rest, ok := strings.CutPrefix(path, l.modulePath+"/")
	return rest, ok
}

func (l *linter) importsByDir() map[string][]imp {
	byDir := map[string][]imp{}
	for _, f := range l.files {
		dir := path.Dir(f.rel)
		for _, spec := range f.file.Imports {
			byDir[dir] = append(byDir[dir], imp{path: unquote(spec.Path), file: f, line: f.fset.Position(spec.Pos()).Line})
		}
	}
	return byDir
}

// importRules implements the three import-boundary rules. Reachability is transitive through
// in-module packages, except that traversal stops at internal/contracts, the one allowed meeting
// point of the three powers.
func (l *linter) importRules() {
	byDir := l.importsByDir()
	for _, f := range l.files {
		for _, spec := range f.file.Imports {
			ipath := unquote(spec.Path)
			dir, local := l.inModuleDir(ipath)
			if local && areaOf(dir) == "adapters" && f.area != "execution" && f.area != "evidence" && f.area != "adapters" {
				l.add("import-adapters", f, spec.Pos(), "package %q imports adapters; only execution and evidence may", f.area)
			}
			if decisionAreas[f.area] && local {
				l.reach(byDir, dir, func(d string) {
					if other := areaOf(d); decisionAreas[other] && other != f.area {
						l.add("import-boundary", f, spec.Pos(), "%s reaches %s through %q; use internal/contracts", f.area, other, ipath)
					}
				}, nil)
			}
			if modelFreeAreas[f.area] {
				if isModelClient(ipath) {
					l.add("import-model-client", f, spec.Pos(), "%s imports a model or explanation client: %q", f.area, ipath)
				} else if local {
					l.reach(byDir, dir, nil, func(i imp) {
						if isModelClient(i.path) {
							l.add("import-model-client", f, spec.Pos(), "%s reaches a model or explanation client %q through %q", f.area, i.path, ipath)
						}
					})
				}
			}
		}
	}
}

// reach visits every in-module directory reachable from start. visitDir sees each directory;
// visitImp sees each import statement inside them.
func (l *linter) reach(byDir map[string][]imp, start string, visitDir func(string), visitImp func(imp)) {
	seen := map[string]bool{}
	stack := []string{start}
	for len(stack) > 0 {
		d := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[d] || areaOf(d) == "internal/contracts" {
			continue
		}
		seen[d] = true
		if visitDir != nil {
			visitDir(d)
		}
		for _, i := range byDir[d] {
			if visitImp != nil {
				visitImp(i)
			}
			if next, ok := l.inModuleDir(i.path); ok {
				stack = append(stack, next)
			}
		}
	}
}

func identName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
