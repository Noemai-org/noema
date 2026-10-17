package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// providerSDKs are Go module path prefixes of model-provider SDKs (INV-29, AGENT-05).
var providerSDKs = []string{
	"github.com/openai/", "github.com/sashabaranov/go-openai", "github.com/anthropics/", "github.com/liushuangls/go-anthropic",
	"google.golang.org/genai", "github.com/google/generative-ai-go", "cloud.google.com/go/aiplatform", "cloud.google.com/go/vertexai",
	"github.com/aws/aws-sdk-go-v2/service/bedrock", "github.com/cohere-ai/", "github.com/mistralai/",
}

var goMockLibraries = []string{
	"github.com/stretchr/testify/mock", "go.uber.org/mock", "github.com/golang/mock", "github.com/vektra/",
	"github.com/maxbrunsfeld/counterfeiter", "github.com/jarcoal/httpmock", "github.com/h2non/gock",
	"github.com/DATA-DOG/go-sqlmock", "github.com/agiledragon/gomonkey",
}

func hasPrefixIn(s string, prefixes []string) (string, bool) {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return p, true
		}
	}
	return "", false
}

// manifestRules: no provider SDK in the core manifest.
func (l *linter) manifestRules() {
	mod := filepath.Join(l.root, "services", "core", "go.mod")
	data, err := os.ReadFile(mod)
	if err != nil {
		return
	}
	for i, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
		if len(fields) == 0 {
			continue
		}
		if prefix, ok := hasPrefixIn(fields[0], providerSDKs); ok {
			l.out = append(l.out, Violation{Rule: "manifest-provider-sdk", File: "services/core/go.mod", Line: i + 1, Msg: "provider SDK " + fields[0] + " (matches " + prefix + ")"})
		}
	}
}

// mockRule: no mock library in benchmark or integration packages (AGENT-12, REQ-TST-004).
func (l *linter) mockRule() {
	_ = filepath.WalkDir(l.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel := filepath.ToSlash(mustRel(l.root, p))
		if d.IsDir() {
			if skipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || !(strings.HasPrefix(rel, "benchmark/") || strings.Contains("/"+rel, "/integration/")) {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if perr != nil {
			return nil
		}
		for _, spec := range file.Imports {
			path, _ := strings.CutPrefix(spec.Path.Value, `"`)
			path = strings.TrimSuffix(path, `"`)
			if prefix, ok := hasPrefixIn(path, goMockLibraries); ok {
				l.out = append(l.out, Violation{Rule: "mock-library", File: rel, Line: fset.Position(spec.Pos()).Line, Msg: "mock library " + path + " (matches " + prefix + ")"})
			}
		}
		return nil
	})
}
