package packload_test

// A510 (interim; UND-04, PCK-02; REQ-OPS-003, REQ-PLN-002; INV-14, INV-21; AGENT-13, AGENT-16): the action graphs a pack names are hash-pinned in the manifest like every other pack file, and the
// core serves only a graph it has verified. Every fault is a real file in a real directory. The packs are SYNTHETIC and made per test; they name no vertical the core knows.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/packload"
)

const (
	graphOne  = "demo/graph/g_one.json"
	graphTwo  = "demo/graph/g_two.json"
	graphBody = `{"graph_id":"demo.g_one","version":1}`
	graphTwoB = `{"graph_id":"demo.g_two","version":1}`
	kindGraph = "action_graph"
)

func packNaming(graphs ...string) string {
	quoted := make([]string, 0, len(graphs))
	for _, g := range graphs {
		quoted = append(quoted, `"`+g+`"`)
	}
	return `{"pack_id":"demo","pack_version":"1.0.0","action_graph_files":[` + strings.Join(quoted, ",") + `]}`
}

// newGraphWorld is a pack of the three earlier files and two action graphs, the pack file naming both and the manifest pinning both.
func newGraphWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.write(graphOne, graphBody)
	w.write(graphTwo, graphTwoB)
	w.setPack(packNaming(graphOne, graphTwo))
	w.list = append(w.list, entry{kindGraph, graphOne, sum(graphBody)}, entry{kindGraph, graphTwo, sum(graphTwoB)})
	w.seal()
	return w
}

// setPack rewrites the pack file and the digest the manifest pins for it.
func (w *world) setPack(body string) {
	w.t.Helper()
	w.write(packRel, body)
	w.list[0].Sha = sum(body)
	w.seal()
}

func (w *world) dropEntry(path string) {
	w.t.Helper()
	kept := w.list[:0:0]
	for _, e := range w.list {
		if e.Path != path {
			kept = append(kept, e)
		}
	}
	w.list = kept
	w.seal()
}

func refusedWith(t *testing.T, what string, err error, kind error, p packload.Pack) {
	t.Helper()
	wantErr(t, what, err, kind, p)
	if len(p.ActionGraphs) != 0 {
		t.Errorf("%s: a refused pack returned %d graphs", what, len(p.ActionGraphs))
	}
}

func kindOf(t *testing.T, err error) string {
	t.Helper()
	var pe *packload.Error
	if !errors.As(err, &pe) {
		t.Fatalf("%v is not a *packload.Error", err)
	}
	return pe.Kind
}

// A510: a pack whose named graphs are exactly the ones the manifest pins loads, and hands back the bytes each digest covers, by path.
func TestAPackWithItsGraphsPinnedLoadsThemVerified(t *testing.T) {
	p, err := newGraphWorld(t).load()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ActionGraphs) != 2 || string(p.ActionGraphs[graphOne]) != graphBody || string(p.ActionGraphs[graphTwo]) != graphTwoB {
		t.Errorf("graphs %v; want the two pinned graphs with their bytes", p.ActionGraphs)
	}
}

// A pack that names no graph and pins none loads with none (nothing to serve is not an error).
func TestAPackThatNamesNoGraphLoadsWithNone(t *testing.T) {
	p, err := newWorld(t).load()
	if err != nil || len(p.ActionGraphs) != 0 {
		t.Errorf("%v %v; want a pack with no graphs", p.ActionGraphs, err)
	}
	w := newWorld(t)
	w.setPack(packNaming())
	if p, err = w.load(); err != nil || len(p.ActionGraphs) != 0 {
		t.Errorf("an empty list: %v %v", p.ActionGraphs, err)
	}
}

// INV-21, AGENT-16: a byte of a pinned graph changed after the manifest was made refuses the whole pack, naming the kind and no path or content.
func TestAChangedByteInAPinnedGraphRefusesThePack(t *testing.T) {
	w := newGraphWorld(t)
	w.write(graphTwo, graphTwoB+" ")
	p, err := w.load()
	refusedWith(t, "a changed graph", err, packload.ErrHash, p)
	if k := kindOf(t, err); k != kindGraph {
		t.Errorf("kind %q; want %q", k, kindGraph)
	}
	if strings.Contains(err.Error(), "g_two") || strings.Contains(err.Error(), "demo") {
		t.Errorf("the error names pack content: %v", err)
	}
}

// A graph the pack names and the manifest does not pin is never served: the pack is refused.
func TestAGraphThePackNamesButTheManifestDoesNotPinIsRefused(t *testing.T) {
	w := newGraphWorld(t)
	w.dropEntry(graphTwo)
	if err := os.Remove(filepath.Join(w.dir, filepath.FromSlash(graphTwo))); err != nil { // no file on disk: only the two lists disagree
		t.Fatal(err)
	}
	p, err := w.load()
	refusedWith(t, "a named graph that is not pinned", err, packload.ErrGraph, p)
	if k := kindOf(t, err); k != kindGraph {
		t.Errorf("kind %q; want %q", k, kindGraph)
	}
	w = newGraphWorld(t) // the file is on disk but unpinned: the pack is refused either way, here as an unlisted file of the kind
	w.dropEntry(graphTwo)
	p, err = w.load()
	refusedWith(t, "a named graph that is on disk and not pinned", err, packload.ErrUnlisted, p)
}

// A graph the manifest pins and the pack does not name is refused too: the two lists are the same set.
func TestAPinnedGraphThePackDoesNotNameIsRefused(t *testing.T) {
	w := newGraphWorld(t)
	w.setPack(packNaming(graphOne))
	p, err := w.load()
	refusedWith(t, "a pinned graph that is not named", err, packload.ErrGraph, p)
	w = newWorld(t) // a pack that names none, with a graph pinned
	w.write(graphOne, graphBody)
	w.list = append(w.list, entry{kindGraph, graphOne, sum(graphBody)})
	w.seal()
	p, err = w.load()
	refusedWith(t, "a graph pinned for a pack that names none", err, packload.ErrGraph, p)
}

// A manifest entry with no file behind it is refused as a missing file.
func TestAGraphEntryWhoseFileIsMissingIsRefused(t *testing.T) {
	w := newGraphWorld(t)
	if err := os.Remove(filepath.Join(w.dir, filepath.FromSlash(graphTwo))); err != nil {
		t.Fatal(err)
	}
	p, err := w.load()
	refusedWith(t, "a missing graph", err, packload.ErrFile, p)
}

// A path listed twice, and a graph under a kind the manifest does not know, make the manifest unusable.
func TestADuplicateGraphAndAnUnknownKindMakeTheManifestUnusable(t *testing.T) {
	w := newGraphWorld(t)
	w.list = append(w.list, entry{kindGraph, graphOne, sum(graphBody)})
	w.seal()
	p, err := w.load()
	refusedWith(t, "a graph listed twice", err, packload.ErrManifest, p)
	for _, kind := range []string{"graph", "action_graphs", "Action_Graph", ""} {
		w = newGraphWorld(t)
		w.list[len(w.list)-1].Kind = kind
		w.seal()
		p, err = w.load()
		refusedWith(t, "kind "+kind, err, packload.ErrManifest, p)
	}
}

// A graph listed as one of the three single kinds is a second file of that kind: the manifest is unusable.
func TestAGraphListedUnderAnotherKindIsRefused(t *testing.T) {
	for _, kind := range []string{"pack", "evidence_mapping", "recon_rules"} {
		w := newGraphWorld(t)
		w.list[len(w.list)-1].Kind = kind
		w.seal()
		p, err := w.load()
		refusedWith(t, "a graph listed as "+kind, err, packload.ErrManifest, p)
	}
}

// A graph file in the graph folder that the manifest does not list is a candidate nobody pinned: refused. A file of another extension there is not a graph.
func TestAnUnlistedGraphFileInTheGraphFolderIsRefused(t *testing.T) {
	w := newGraphWorld(t)
	w.write("demo/graph/g_extra.json", graphBody)
	p, err := w.load()
	refusedWith(t, "an unlisted graph", err, packload.ErrUnlisted, p)
	if k := kindOf(t, err); k != kindGraph {
		t.Errorf("kind %q; want %q", k, kindGraph)
	}
	w = newGraphWorld(t)
	w.write("demo/graph/README.md", "notes")
	if _, err = w.load(); err != nil {
		t.Errorf("a note beside the graphs: %v", err)
	}
}

// A graph path that leaves the directory, or a graph that is a link, is refused before anything is read through it.
func TestAGraphPathThatEscapesOrIsALinkIsRefused(t *testing.T) {
	w := newGraphWorld(t)
	w.setPack(packNaming(graphOne, "../outside.json"))
	w.list[len(w.list)-1] = entry{kindGraph, "../outside.json", sum(graphTwoB)}
	w.seal()
	p, err := w.load()
	refusedWith(t, "a graph that escapes", err, packload.ErrPath, p)

	w = newGraphWorld(t)
	link := filepath.Join(w.dir, filepath.FromSlash(graphTwo))
	other := filepath.Join(w.dir, "demo", "other.json")
	if err := os.WriteFile(other, []byte(graphTwoB), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Skip("symlinks are not available here")
	}
	p, err = w.load()
	refusedWith(t, "a graph that is a link", err, packload.ErrLink, p)
}

// The pack names its graphs in a member the loader reads from the verified bytes only: a pack whose list is not strings, or repeats a path, is refused.
func TestAPackWhoseGraphListIsNotUsableIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"not an array": `{"pack_id":"demo","pack_version":"1.0.0","action_graph_files":"demo/graph/g_one.json"}`,
		"not strings":  `{"pack_id":"demo","pack_version":"1.0.0","action_graph_files":[1,2]}`,
		"a repeat":     packNaming(graphOne, graphOne),
	} {
		w := newGraphWorld(t)
		w.dropEntry(graphTwo) // only the first graph is pinned, so a repeat of it is the one thing wrong
		if err := os.Remove(filepath.Join(w.dir, filepath.FromSlash(graphTwo))); err != nil {
			t.Fatal(err)
		}
		w.setPack(body)
		p, err := w.load()
		refusedWith(t, name, err, packload.ErrGraph, p)
	}
}
