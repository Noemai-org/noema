package main

// STM-01 compose, lane 1: the pack source and what is built from it (A333, A377 items 6 and 7, A475, A486, A510; interim, relayed, not yet committed). Production gives the Trust
// adapters the manifest-verified pack (platform/packload) and nothing else: Capabilities, Scope and Authority are built over the verified bytes, the action graphs are the ones the
// manifest pins (no second read of the directory), and PackVersion is the verified pack's own `<id>@<version>`. With none of the three pack settings the pack is a named gap
// (the process starts, /readyz names it, A469, A486); with some of them, or with an invalid one, or with a pack that does not verify or cannot be built, the process stops, with a
// fixed text that carries no path, no value and no pack content (AGENT-13). Candidates stays a named gap: its reader, ranker and labeller are not built. REQ-STM-001, REQ-TRU-004,
// REQ-TRU-005, REQ-OPS-003; INV-14, INV-16, INV-21, AGENT-13, AGENT-16. Every value is SYNTHETIC (the fintech pack under /packs is synthetic).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/understanding"
)

const psrcManifest = "fintech/manifest.v1.json"

func psrcPacks() string { return filepath.Join("..", "..", "..", "..", "packs") }

func psrcEnv(dir string) envMap {
	return envMap{EnvPackDir: dir, EnvPackManifest: psrcManifest, EnvPackMaxBytes: "1048576", EnvTrustEscalationGraph: syntheticEscalationGraph}
}

// psrcProduction runs the production adapter source (the seam compose.go plugs) over an environment.
func psrcProduction(t *testing.T, e envMap) (TrustAdapters, error) {
	t.Helper()
	return productionDeps().TrustAdapters(Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, nil, clock.NewManual(time.Unix(1_800_000_000, 0)))
}

// psrcCopy copies the manifest and every file it lists into a fresh directory, so a test can break one of them.
func psrcCopy(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	raw, err := os.ReadFile(filepath.Join(psrcPacks(), filepath.FromSlash(psrcManifest)))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, rel := range append([]string{psrcManifest}, func() []string {
		var out []string
		for _, f := range m.Files {
			out = append(out, f.Path)
		}
		return out
	}()...) {
		b, err := os.ReadFile(filepath.Join(psrcPacks(), filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// psrcReseal rewrites every digest of the manifest from the files as they are now, as CI's step does after an edit the manifest is meant to cover.
func psrcReseal(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(psrcManifest))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, f := range m["files"].([]any) {
		e := f.(map[string]any)
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(e["path"].(string))))
		if err != nil {
			t.Fatal(err)
		}
		s := sha256.Sum256(b)
		e["sha256"] = hex.EncodeToString(s[:])
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func psrcNotAList(t *testing.T, err error) {
	t.Helper()
	var u *UnbuiltAdaptersError
	if err == nil || errors.As(err, &u) || errors.Is(err, errPackNotConfigured) {
		t.Fatalf("error %v; want a startup error that is neither a named gap nor a list of waiting adapters", err)
	}
}

// The real pack, through its real manifest: Capabilities, Scope and Authority are built, PackVersion is the verified pack's own id and version (the same the Interpreter port is given),
// and what is still waiting is named, Candidates among it: its reader, ranker and labeller are not built, so no quiet nil is plugged in. A333; REQ-TRU-004, REQ-TRU-005.
func TestProductionBuildsThePackAdaptersAndPackVersionFromTheVerifiedPack(t *testing.T) {
	a, err := psrcProduction(t, psrcEnv(psrcPacks()))
	names := unbuilt(t, err)
	if _, ok := a.Capabilities.(trustpack.Capabilities); !ok {
		t.Errorf("Capabilities is %T, want trustpack.Capabilities", a.Capabilities)
	}
	if _, ok := a.Scope.(trustpack.Scope); !ok {
		t.Errorf("Scope is %T, want trustpack.Scope", a.Scope)
	}
	if _, ok := a.Authority.(trustpack.Authority); !ok {
		t.Errorf("Authority is %T, want trustpack.Authority", a.Authority)
	}
	raw, rerr := os.ReadFile(filepath.Join(psrcPacks(), "fintech_v1.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	view, verr := understanding.NewPackView(raw)
	if verr != nil {
		t.Fatal(verr)
	}
	id, version := view.Ref()
	if want := id + "@" + version; a.PackVersion != want {
		t.Errorf("PackVersion %q, want %q (the Interpreter port's pack ref)", a.PackVersion, want)
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, built := range []string{"Pack", "Capabilities", "Scope", "Authority", "PackVersion"} {
		if have[built] {
			t.Errorf("%s is still named as not built: %v", built, names)
		}
	}
	if !have["Candidates"] || a.Candidates != nil {
		t.Errorf("Candidates must stay a named gap while its reader, ranker and labeller are not built: names %v, adapter %v", names, a.Candidates)
	}
}

// With none of the three pack settings the pack is a named gap, not a stop (A469, A486): the list starts with "Pack", names every adapter, and carries no value.
func TestWithNoPackSettingThePackIsANamedGap(t *testing.T) {
	a, err := psrcProduction(t, envMap{EnvTrustEscalationGraph: syntheticEscalationGraph})
	names := unbuilt(t, err)
	if len(names) == 0 || names[0] != "Pack" {
		t.Errorf("names %v; want Pack first", names)
	}
	if !errors.Is(err, errPackNotConfigured) {
		t.Errorf("error %v does not carry the gap as its cause", err)
	}
	if a.Capabilities != nil || a.Scope != nil || a.Authority != nil || a.PackVersion != "" {
		t.Errorf("an adapter is built with no pack: %+v", a)
	}
}

// Some of the settings without the others is a mistake of the operator, not a gap: the process stops and the error names the setting that is missing, never a value.
func TestAPartlyGivenPackSettingGroupIsAStartupErrorNamingTheMissingOne(t *testing.T) {
	for _, drop := range []string{EnvPackDir, EnvPackManifest, EnvPackMaxBytes} {
		e := psrcEnv(psrcPacks())
		delete(e, drop)
		_, err := psrcProduction(t, e)
		psrcNotAList(t, err)
		if !strings.Contains(err.Error(), drop) {
			t.Errorf("without %s: error %q does not name it", drop, err)
		}
		if strings.Contains(err.Error(), psrcPacks()) {
			t.Errorf("without %s: error %q carries a setting's value", drop, err)
		}
	}
}

// An invalid bound is fatal, names the setting and never repeats the value. AGENT-16.
func TestAnInvalidPackBoundIsAStartupErrorThatDoesNotEchoTheValue(t *testing.T) {
	for _, bad := range []string{"0", "-1", "lots", "1.5", "007", "99999999999999999999"} {
		e := psrcEnv(psrcPacks())
		e[EnvPackMaxBytes] = bad
		_, err := psrcProduction(t, e)
		psrcNotAList(t, err)
		if !strings.Contains(err.Error(), EnvPackMaxBytes) || strings.Contains(err.Error(), bad) {
			t.Errorf("bound %q: error %q; want one naming %s without the value", bad, err, EnvPackMaxBytes)
		}
	}
}

// A file changed after the manifest was made stops the start, for the pack file and for an action graph alike (A510: a graph is served only from the verified bytes). The cause is
// packload's, the text carries no path and no content. INV-16, AGENT-13.
func TestAFileChangedAfterTheManifestStopsTheStart(t *testing.T) {
	for name, rel := range map[string]string{"the pack file": "fintech_v1.json", "an action graph": "fintech/graph/g1_investigate.json"} {
		dir := psrcCopy(t)
		f, err := os.OpenFile(filepath.Join(dir, filepath.FromSlash(rel)), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("\n"); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		a, perr := psrcProduction(t, psrcEnv(dir))
		psrcNotAList(t, perr)
		if !errors.Is(perr, packload.ErrHash) {
			t.Errorf("%s: error %v; want packload.ErrHash", name, perr)
		}
		if strings.Contains(perr.Error(), dir) || strings.Contains(perr.Error(), rel) {
			t.Errorf("%s: error %q carries the directory or a path", name, perr)
		}
		if a.Capabilities != nil || a.Scope != nil || a.Authority != nil {
			t.Errorf("%s: adapters built over a refused pack: %+v", name, a)
		}
	}
}

// A graph the pack names and the manifest does not pin is refused: no unverified graph is ever served (A510).
func TestAGraphTheManifestDoesNotPinIsRefused(t *testing.T) {
	dir := psrcCopy(t)
	path := filepath.Join(dir, filepath.FromSlash(psrcManifest))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	var kept []any
	dropped := false
	for _, f := range m["files"].([]any) {
		if e := f.(map[string]any); e["kind"] == "action_graph" && !dropped {
			dropped = true
			continue
		}
		kept = append(kept, f)
	}
	m["files"] = kept
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	_, perr := psrcProduction(t, psrcEnv(dir))
	psrcNotAList(t, perr)
	if !errors.Is(perr, packload.ErrGraph) {
		t.Errorf("error %v; want packload.ErrGraph", perr)
	}
}

// A pack whose digests are right but whose graph the Trust adapters cannot use (a node names a capability the pack does not register) is refused whole, with a fixed text: the
// parser's own message may quote the pack, so it stays reachable with errors.Is and never in Error(). INV-16, AGENT-13.
func TestAVerifiedPackTheTrustAdaptersCannotUseIsRefusedWithAFixedText(t *testing.T) {
	dir := psrcCopy(t)
	path := filepath.Join(dir, "fintech", "graph", "g1_investigate.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var g map[string]any
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	g["nodes"].([]any)[0].(map[string]any)["capability_id"] = "synthetic.not_a_registered_capability"
	out, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	psrcReseal(t, dir)
	_, perr := psrcProduction(t, psrcEnv(dir))
	psrcNotAList(t, perr)
	if !errors.Is(perr, trustpack.ErrPack) {
		t.Errorf("error %v; want trustpack.ErrPack reachable", perr)
	}
	if strings.Contains(perr.Error(), "synthetic.not_a_registered_capability") || strings.Contains(perr.Error(), dir) {
		t.Errorf("error %q quotes the pack or the directory", perr)
	}
}

// The whole composition: with the Trust settings and the pack settings given, the process starts, Trust is not plugged (the other adapters are not built) and the names of /readyz no
// longer include the pack, its three adapters or PackVersion; Candidates is still named. REQ-API-004, REQ-TRU-009; AGENT-16.
func TestTheCompositionNoLongerNamesThePackOrItsAdapters(t *testing.T) {
	e := mcEnv(t)
	for k, v := range psrcEnv(psrcPacks()) {
		e[k] = v
	}
	l := &loaded{}
	d := productionDeps()
	d.LoadKeys = l.load
	c, parts, closer, err := mcBuild(t, e, d)
	if err != nil {
		t.Fatalf("the process must start: %v", err)
	}
	defer closer()
	if c.Ports.Trust != nil || parts.Service != nil {
		t.Error("Trust is plugged while its other adapters are not built")
	}
	have := map[string]bool{}
	for _, n := range c.TrustUnbuilt {
		have[n] = true
	}
	for _, built := range []string{"Pack", "Capabilities", "Scope", "Authority", "PackVersion"} {
		if have[built] {
			t.Errorf("/readyz still names %s: %v", built, c.TrustUnbuilt)
		}
	}
	if !have["Candidates"] {
		t.Errorf("/readyz does not name Candidates: %v", c.TrustUnbuilt)
	}
}

// INV-21, A510: the Trust pack is built from the bytes the manifest verified. A second read of the directory (trustpack.LoadPack over a directory) in the composition's own code would
// serve a file that changed between the check and the read, so no non-test file of this package calls it. The checker is proved on a planted source, so it is not vacuous.
func TestNoCompositionCodeLoadsThePackFromTheDirectoryASecondTime(t *testing.T) {
	const planted = "package main\nimport \"x/trustpack\"\nfunc f() { _, _ = trustpack.LoadPack(nil, \"p\") }\n"
	if got := psrcDirectoryLoads(t, "planted.go", planted); len(got) != 1 {
		t.Fatalf("the checker did not find a planted LoadPack call: %v", got)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if got := psrcDirectoryLoads(t, name, nil); len(got) != 0 {
			t.Errorf("%s loads the pack from a directory a second time: %v", name, got)
		}
	}
}

func psrcDirectoryLoads(t *testing.T, name string, src any) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return psrcCallsOf(fset, f, "LoadPack")
}

func psrcCallsOf(fset *token.FileSet, f *ast.File, fn string) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == fn {
				out = append(out, fset.Position(c.Pos()).String())
			}
		}
		return true
	})
	return out
}
