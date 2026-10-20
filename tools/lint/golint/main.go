// Command golint enforces the Go architecture rules of PLT-03 (SoT §51, REQ-OPS-003).
//
// It parses the sources with go/parser and walks them with go/ast; it needs no module download.
// Output is a JSON array of violations. Exit status is 0 whether or not violations exist: the
// harness (tools/lint/run.py) decides.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Violation is one broken rule at one place.
type Violation struct {
	Rule string `json:"rule"`
	File string `json:"file"`
	Line int    `json:"line"`
	Msg  string `json:"msg"`
}

// srcFile is one parsed Go file under services/core.
type srcFile struct {
	rel  string // path relative to services/core, slash separated
	repo string // path relative to the root, slash separated
	area string // boundary the file belongs to, for example "trust" or "platform/clock"
	test bool
	fset *token.FileSet
	file *ast.File
}

type linter struct {
	root       string
	modulePath string
	files      []*srcFile
	out        []Violation
}

func (l *linter) add(rule string, f *srcFile, pos token.Pos, format string, args ...any) {
	l.out = append(l.out, Violation{Rule: rule, File: f.repo, Line: f.fset.Position(pos).Line, Msg: fmt.Sprintf(format, args...)})
}

func main() {
	root := flag.String("root", ".", "repository root (or a canary tree laid out like one)")
	flag.Parse()
	l := &linter{root: *root}
	if err := l.load(); err != nil {
		fmt.Fprintln(os.Stderr, "golint:", err)
		os.Exit(2)
	}
	l.importRules()
	l.subpackageRules()
	l.astRules()
	l.manifestRules()
	l.mockRule()
	if l.out == nil {
		l.out = []Violation{}
	}
	if err := json.NewEncoder(os.Stdout).Encode(l.out); err != nil {
		fmt.Fprintln(os.Stderr, "golint:", err)
		os.Exit(2)
	}
}

// skipDir reports directories that are never scanned. Canaries are left out here by path, which
// weakens no rule: they are scanned only when the harness points the root at them.
func skipDir(rel string) bool {
	switch filepath.Base(rel) {
	case ".git", "node_modules", ".venv", "__pycache__":
		return true
	}
	return rel == "tools/lint/canaries"
}

func (l *linter) load() error {
	coreDir := filepath.Join(l.root, "services", "core")
	if mod, err := os.ReadFile(filepath.Join(coreDir, "go.mod")); err == nil {
		for _, line := range strings.Split(string(mod), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
				l.modulePath = strings.TrimSpace(rest)
			}
		}
	}
	if _, err := os.Stat(coreDir); err != nil {
		return nil
	}
	return filepath.WalkDir(coreDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		repo := filepath.ToSlash(mustRel(l.root, p))
		if d.IsDir() {
			if skipDir(repo) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", repo, perr)
		}
		rel := filepath.ToSlash(mustRel(coreDir, p))
		l.files = append(l.files, &srcFile{rel: rel, repo: repo, area: areaOf(filepath.ToSlash(filepath.Dir(rel))), test: strings.HasSuffix(p, "_test.go"), fset: fset, file: file})
		return nil
	})
}

func mustRel(base, p string) string {
	r, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return r
}

// areaOf maps a directory (relative to services/core) to its boundary name.
func areaOf(dir string) string {
	parts := strings.Split(dir, "/")
	if (parts[0] == "platform" || parts[0] == "internal") && len(parts) > 1 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

func unquote(lit *ast.BasicLit) string {
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return lit.Value
	}
	return s
}
