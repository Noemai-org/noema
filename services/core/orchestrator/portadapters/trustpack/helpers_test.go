package trustpack_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// SYNTHETIC fixtures only (AGENT-25): the fintech pack under /packs is synthetic data, and the tenant below names nobody. Every expectation is read from the pack files by the
// code in this file, which does not use the package under test, so a test never compares the adapter with itself.

const (
	packFileName    = "fintech_v1.json"
	tenant          = "ten_synthetic_1"
	otherTenant     = "ten_synthetic_2"
	intentIDForTest = "int_synth01"
)

func packsDir() string { return filepath.Join("..", "..", "..", "..", "..", "packs") }

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

// The independent reader of the pack: only the fields the tests compare with.
type rawCap struct {
	ID         string `json:"capability_id"`
	Prohibited bool   `json:"prohibited"`
	Authority  struct {
		Roles []string `json:"roles_allowed"`
	} `json:"authority"`
}

type rawNode struct {
	ID           string  `json:"node_id"`
	TrustProfile *string `json:"trust_profile"`
}

type rawProfile struct {
	ID        string `json:"profile_id"`
	Authority struct {
		Roles []string `json:"roles_allowed"`
	} `json:"authority"`
}

type rawPack struct {
	Caps     []rawCap     `json:"capabilities"`
	Nodes    []rawNode    `json:"intent_nodes"`
	Profiles []rawProfile `json:"trust_profiles"`
	Graphs   []string     `json:"action_graph_files"`
}

type rawGraphNode struct {
	ID         string `json:"node_id"`
	Kind       string `json:"kind"`
	Capability string `json:"capability_id"`
}

type rawGraph struct {
	ID    string         `json:"graph_id"`
	Roots []string       `json:"roots"`
	Nodes []rawGraphNode `json:"nodes"`
	Edges []struct {
		Node      string `json:"node_id"`
		DependsOn string `json:"depends_on"`
	} `json:"edges"`
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func readPackFile(t testing.TB, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(packsDir(), rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readRawPack(t testing.TB) rawPack {
	t.Helper()
	var p rawPack
	if err := json.Unmarshal(readPackFile(t, packFileName), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func readRawGraphs(t testing.TB) []rawGraph {
	t.Helper()
	var out []rawGraph
	for _, rel := range readRawPack(t).Graphs {
		var g rawGraph
		if err := json.Unmarshal(readPackFile(t, rel), &g); err != nil {
			t.Fatal(err)
		}
		out = append(out, g)
	}
	return out
}

func graphByWrite(t testing.TB, wantWrite bool) rawGraph {
	t.Helper()
	for _, g := range readRawGraphs(t) {
		has := false
		for _, n := range g.Nodes {
			has = has || n.Kind == "WRITE"
		}
		if has == wantWrite {
			return g
		}
	}
	t.Fatal("no such graph in the pack")
	return rawGraph{}
}

// escalateGraph is the pack's graph of three write nodes in a chain (the escalation plan, by its shape in the pack file); complaintGraph the one-node write graph; investigateGraph the
// graph with no write node. The tests find them by shape and never by a literal ID.
func escalateGraph(t testing.TB) rawGraph {
	t.Helper()
	for _, g := range readRawGraphs(t) {
		w := 0
		for _, n := range g.Nodes {
			if n.Kind == "WRITE" {
				w++
			}
		}
		if w == 3 {
			return g
		}
	}
	t.Fatal("no graph with three write nodes")
	return rawGraph{}
}

func complaintGraph(t testing.TB) rawGraph {
	t.Helper()
	for _, g := range readRawGraphs(t) {
		if len(g.Nodes) == 1 && g.Nodes[0].Kind == "WRITE" {
			return g
		}
	}
	t.Fatal("no one-node write graph")
	return rawGraph{}
}

func investigateGraph(t testing.TB) rawGraph {
	t.Helper()
	g := graphByWrite(t, false)
	if len(g.Nodes) < 2 {
		t.Fatal("the graph with no write node is not the investigation graph")
	}
	return g
}

func capsOf(g rawGraph) []string {
	var out []string
	for _, n := range g.Nodes {
		out = append(out, n.Capability)
	}
	sort.Strings(out)
	return out
}

func writeCapsOf(g rawGraph) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.Kind == "WRITE" {
			out = append(out, n.Capability)
		}
	}
	sort.Strings(out)
	return out
}

func nodeCap(g rawGraph, nodeID string) string {
	for _, n := range g.Nodes {
		if n.ID == nodeID {
			return n.Capability
		}
	}
	return ""
}

// closure is the capabilities of the nodes that depend, directly or not, on the node (the node included), found by a fixpoint over the edges.
func closure(g rawGraph, root string) []string {
	in := map[string]bool{root: true}
	for changed := true; changed; {
		changed = false
		for _, e := range g.Edges {
			if in[e.DependsOn] && !in[e.Node] {
				in[e.Node], changed = true, true
			}
		}
	}
	var out []string
	for _, n := range g.Nodes {
		if in[n.ID] {
			out = append(out, n.Capability)
		}
	}
	sort.Strings(out)
	return out
}

func prohibitedCaps(t testing.TB) []string {
	t.Helper()
	var out []string
	for _, c := range readRawPack(t).Caps {
		if c.Prohibited {
			out = append(out, c.ID)
		}
	}
	if len(out) != 8 {
		t.Fatalf("the pack marks %d capabilities prohibited, want the 8 of SoT 34.3", len(out))
	}
	return out
}

func allowedCaps(t testing.TB) []rawCap {
	t.Helper()
	var out []rawCap
	for _, c := range readRawPack(t).Caps {
		if !c.Prohibited {
			out = append(out, c)
		}
	}
	return out
}

// files is the real pack and its graph files, as bytes by path, for a test to change a copy of.
func files(t testing.TB) map[string][]byte {
	t.Helper()
	m := map[string][]byte{packFileName: readPackFile(t, packFileName)}
	for _, rel := range readRawPack(t).Graphs {
		m[rel] = readPackFile(t, rel)
	}
	return m
}

func mapFS(m map[string][]byte) fstest.MapFS {
	fsys := fstest.MapFS{}
	for k, v := range m {
		fsys[k] = &fstest.MapFile{Data: v}
	}
	return fsys
}

// edit decodes JSON into a generic value, lets the test change it, and encodes it again.
func edit(t testing.TB, raw []byte, f func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	f(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func loadReal(t testing.TB) *trustpack.Pack {
	t.Helper()
	p, err := trustpack.LoadPack(os.DirFS(packsDir()), packFileName)
	if err != nil {
		t.Fatalf("the fintech pack does not load: %v", err)
	}
	return p
}

func loadFiles(t testing.TB, m map[string][]byte) (*trustpack.Pack, error) {
	t.Helper()
	return trustpack.LoadPack(mapFS(m), packFileName)
}

func mustLoadFiles(t testing.TB, m map[string][]byte) *trustpack.Pack {
	t.Helper()
	p, err := loadFiles(t, m)
	if err != nil {
		t.Fatalf("the changed pack does not load: %v", err)
	}
	return p
}

// remedySpy answers every question with one remedy and records what it was asked.
type remedySpy struct {
	id      string
	present bool
	err     error
	asked   []string
}

func (r *remedySpy) RequestedRemedy(_ context.Context, tenantID, intentID string, version int) (string, bool, error) {
	r.asked = append(r.asked, tenantID+"/"+intentID)
	return r.id, r.present, r.err
}

func admitted(node string) intake.AdmittedIntent {
	return intake.AdmittedIntent{TenantID: tenant, IntentID: intentIDForTest, Version: 1, IntentNode: node}
}

func firstNodeWithProfile(t testing.TB) rawNode {
	t.Helper()
	for _, n := range readRawPack(t).Nodes {
		if n.TrustProfile != nil {
			return n
		}
	}
	t.Fatal("no intent node with a trust profile")
	return rawNode{}
}

func nodeWithoutProfile(t testing.TB) rawNode {
	t.Helper()
	for _, n := range readRawPack(t).Nodes {
		if n.TrustProfile == nil {
			return n
		}
	}
	t.Fatal("no intent node without a trust profile")
	return rawNode{}
}

func profileRoles(t testing.TB, id string) []string {
	t.Helper()
	for _, p := range readRawPack(t).Profiles {
		if p.ID == id {
			return p.Authority.Roles
		}
	}
	t.Fatalf("no trust profile %q", id)
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var roles = []string{"customer", "ops", "approver", "audit", "", "root", "Customer", "OPS", " ops", "ops "}

// containsID reports whether a graph file's graph_id is id.
func containsID(t testing.TB, raw []byte, id string) bool {
	t.Helper()
	var g rawGraph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g.ID == id
}

// setNodeCap changes the capability of the i-th node of a decoded graph.
func setNodeCap(g map[string]any, i int, capability string) {
	g["nodes"].([]any)[i].(map[string]any)["capability_id"] = capability
}

// escalateGraphPath is the pack-relative path of the file of escalateGraph.
func escalateGraphPath(t testing.TB) string {
	t.Helper()
	id := escalateGraph(t).ID
	for _, rel := range readRawPack(t).Graphs {
		if containsID(t, readPackFile(t, rel), id) {
			return rel
		}
	}
	t.Fatal("the escalation graph file was not found")
	return ""
}
