package trustpack_test

import (
	"errors"
	"os"
	"testing"
	"testing/fstest"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

// Proves REQ-CAP-001, REQ-CAP-002, REQ-AG-001, INV-14, INV-18 (the pack is data and a bad pack gives no adapter), INV-16 (fail closed).

func TestTheRealPackLoads(t *testing.T) {
	if p, err := trustpack.LoadPack(os.DirFS(packsDir()), packFileName); err != nil || p == nil {
		t.Fatalf("LoadPack(the fintech pack) = %v, %v", p, err)
	}
}

// A pack that cannot give a sound answer gives no Pack at all: no partial pack, so no adapter can answer from half of one (INV-16, REQ-CAP-001).
func TestABadPackIsRefusedWholeAndNeverPartly(t *testing.T) {
	escalatePath := escalateGraphPath(t)
	prohibited := prohibitedCaps(t)[0]
	cases := []struct {
		name string
		make func(m map[string][]byte)
	}{
		{"the pack file is missing", func(m map[string][]byte) { delete(m, packFileName) }},
		{"the pack is not JSON", func(m map[string][]byte) { m[packFileName] = []byte("not json") }},
		{"the pack has data after its end", func(m map[string][]byte) { m[packFileName] = append(m[packFileName], []byte(` {}`)...) }},
		{"the pack has an unknown field", func(m map[string][]byte) {
			m[packFileName] = edit(t, m[packFileName], func(p map[string]any) { p["surprise"] = 1 })
		}},
		{"a listed graph file is missing", func(m map[string][]byte) { delete(m, escalatePath) }},
		{"a graph file is not JSON", func(m map[string][]byte) { m[escalatePath] = []byte("{") }},
		{"a graph file has data after its end", func(m map[string][]byte) { m[escalatePath] = append(m[escalatePath], []byte(` []`)...) }},
		{"a listed graph path leaves the root", func(m map[string][]byte) {
			m[packFileName] = edit(t, m[packFileName], func(p map[string]any) { p["action_graph_files"] = []any{"../outside.json"} })
		}},
		{"a graph node names a capability the registry does not know", func(m map[string][]byte) {
			m[escalatePath] = edit(t, m[escalatePath], func(g map[string]any) { setNodeCap(g, 0, "x.cap.unregistered") })
		}},
		{"a graph node names a prohibited capability", func(m map[string][]byte) {
			m[escalatePath] = edit(t, m[escalatePath], func(g map[string]any) { setNodeCap(g, 0, prohibited) })
		}},
		{"an edge names a node the graph does not have", func(m map[string][]byte) {
			m[escalatePath] = edit(t, m[escalatePath], func(g map[string]any) { g["edges"].([]any)[0].(map[string]any)["depends_on"] = "no_such_node" })
		}},
		{"a root names a node the graph does not have", func(m map[string][]byte) {
			m[escalatePath] = edit(t, m[escalatePath], func(g map[string]any) { g["roots"] = []any{"no_such_node"} })
		}},
		{"a node ID repeats in a graph", func(m map[string][]byte) {
			m[escalatePath] = edit(t, m[escalatePath], func(g map[string]any) {
				ns := g["nodes"].([]any)
				ns[1].(map[string]any)["node_id"] = ns[0].(map[string]any)["node_id"]
			})
		}},
		{"two graph files carry one graph ID", func(m map[string][]byte) {
			m["copy.json"] = m[escalatePath]
			m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
				p["action_graph_files"] = append(p["action_graph_files"].([]any), "copy.json")
			})
		}},
		{"an intent node names a trust profile the pack does not have", func(m map[string][]byte) {
			m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
				for _, n := range p["intent_nodes"].([]any) {
					if n.(map[string]any)["trust_profile"] != nil {
						n.(map[string]any)["trust_profile"] = "x.trust.nothing"
					}
				}
			})
		}},
		{"an intent node repeats", func(m map[string][]byte) {
			m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
				ns := p["intent_nodes"].([]any)
				p["intent_nodes"] = append(ns, ns[0])
			})
		}},
		{"a trust profile repeats", func(m map[string][]byte) {
			m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
				ps := p["trust_profiles"].([]any)
				p["trust_profiles"] = append(ps, ps[0])
			})
		}},
		{"a monetary write is not marked prohibited", func(m map[string][]byte) {
			m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
				for _, c := range p["capabilities"].([]any) {
					if c.(map[string]any)["capability_id"] == prohibited {
						c.(map[string]any)["prohibited"] = false
					}
				}
			})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := files(t)
			c.make(m)
			p, err := loadFiles(t, m)
			if !errors.Is(err, trustpack.ErrPack) || p != nil {
				t.Fatalf("LoadPack = %v, %v; want a nil Pack and ErrPack", p, err)
			}
		})
	}
}

func TestNoRootAndNoNameGiveNoPack(t *testing.T) {
	if p, err := trustpack.LoadPack(nil, packFileName); !errors.Is(err, trustpack.ErrConfig) || p != nil {
		t.Fatalf("LoadPack(nil root) = %v, %v; want ErrConfig", p, err)
	}
	if p, err := trustpack.LoadPack(fstest.MapFS{}, ""); !errors.Is(err, trustpack.ErrConfig) || p != nil {
		t.Fatalf("LoadPack(no name) = %v, %v; want ErrConfig", p, err)
	}
}

// INV-14, SoT 51: the adapters name no capability, node or graph of any pack. A planted literal is found, so the scan is not vacuous.
func TestNoPackIdentifierIsWrittenInTheAdapterCode(t *testing.T) {
	prefixes := packPrefixes(t)
	if len(prefixes) == 0 {
		t.Fatal("no pack prefix read from the pack")
	}
	found := scanNonTestSource(t, prefixes)
	if len(found) != 0 {
		t.Fatalf("pack identifiers written in the adapter code: %v", found)
	}
	if got := scanSource(t, "package x\nvar _ = \""+prefixes[0]+"cap.planted\"\n", prefixes); len(got) != 1 {
		t.Fatalf("the scan did not find a planted literal: %v", got)
	}
}
