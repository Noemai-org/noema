// Package trustpack is the Trust intake's production adapters over the pack's data (TRU-02 ports; ruling A306, interim; A313, A319): Capabilities (T4, T11 to T14), Scope (T5)
// and Authority (T6), and the escalation set the Record's writer takes (REQ-TRU-010). A pack is data, loaded and never imported as code (SoT 51): no capability ID, node ID,
// graph ID or industry term is written in this package (INV-14); the composition root names the pack file and the escalation graph from configuration.
//
// LoadPack reads the pack once and refuses it whole when it cannot give a sound answer (INV-16). The adapters hold the loaded pack and read nothing else: the capability registry
// is built from the pack's descriptors (execution/registry), the roles come from the descriptors (capability) and from the trust profiles (intent node), and the authorized
// sets come from the action graphs the pack lists.
package trustpack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"

	"github.com/Noemai-org/noema/services/core/execution/registry"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// Errors of the adapters. Callers compare with errors.Is.
var (
	// ErrConfig is returned by an adapter that was not built by its constructor (the zero value), by a constructor given no pack or no port, and by a call on a nil pack: nothing is
	// ever answered from nothing.
	ErrConfig = errors.New("trustpack: the adapter is not configured")
	// ErrPack is returned by LoadPack for pack data that cannot give a sound answer. The error names the rule, never the content of the pack.
	ErrPack = errors.New("trustpack: the pack data is not usable")
	// ErrSession is returned for a question about no tenant.
	ErrSession = errors.New("trustpack: the question names no tenant")
	// ErrIntent is returned for an admitted intent that names no tenant, no intent ID or no version.
	ErrIntent = errors.New("trustpack: the admitted intent is not usable")
	// ErrCapability is returned by Authorized for a capability the registry does not know or that is prohibited: such a capability roots no authorization.
	ErrCapability = errors.New("trustpack: the capability cannot root an authorization")
	// ErrEscalationGraph is returned by EscalationSetFromPack for a graph that is missing from the pack or that has no WRITE node.
	ErrEscalationGraph = errors.New("trustpack: the escalation graph is missing or has no write node")
)

func packErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrPack, fmt.Sprintf(format, args...))
}

// Pack is the pack's data as the adapters read it, loaded once and read-only after that, so it is safe to share. The zero value is not a pack: every adapter built from it, or from
// a nil *Pack, refuses.
type Pack struct {
	// raw is the pack file as LoadPack read and decoded it: the one copy of its bytes (PackBytes).
	raw []byte
	reg *registry.Registry
	// capRoles is each capability's descriptor roles (authority.roles_allowed), by capability ID.
	capRoles map[string]map[string]struct{}
	// nodes is each intent node's trust profile roles; a node with no profile has no entry in roles and is out of scope.
	nodes map[string]nodeAuthority
	// writes is the capability IDs of each graph's WRITE nodes, sorted, by graph ID.
	writes map[string][]string
	// graphRoots is the capability IDs of each graph's root nodes, by graph ID.
	graphRoots map[string]map[string]struct{}
	// rooted is, for a capability that is the capability of a declared root node, the capability IDs of every node that depends on that root, the root included, sorted, over
	// every graph that declares it a root.
	rooted map[string][]string
	// owned is, for each intent node whose trust profile says the principal must own a slot, what the pack says of that ownership, read at load and judged by OwnershipKinds.
	owned map[string]nodeOwnership
	// writeNodes is each intent node's write_when and slot declarations, as the pack file wrote them, read at load and returned as a copy by WriteWhens.
	writeNodes map[string]WriteWhen
	// ref is `<pack id>@<pack version>` as the pack file carries them (Ref).
	ref string
}

type nodeAuthority struct {
	profiled bool
	roles    map[string]struct{}
}

// strictDecode decodes one JSON value into v (through the generated contract types, which refuse a field they do not know) and refuses data after it.
func strictDecode(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("data after the end of the value")
	}
	return nil
}

// LoadPack reads the pack file `packFile` from `root` and the action graph files it lists (paths relative to `root`, as `action_graph_files` writes them). It refuses the whole pack,
// with ErrPack, when:
//   - a file cannot be read or does not decode through its contract type, or has data after its end;
//   - the capability registry refuses the descriptors (incomplete, repeated, or a monetary write not marked prohibited);
//   - an intent node is repeated, or names a trust profile the pack does not have, or a trust profile is repeated;
//   - a graph is repeated, has no node or no root, repeats a node ID, has a root or an edge end that is not one of its nodes, or has a node whose capability is not registered or is
//     prohibited (a prohibited capability is in no graph, BD-08).
func LoadPack(root fs.FS, packFile string) (*Pack, error) {
	if root == nil || packFile == "" {
		return nil, ErrConfig
	}
	raw, err := fs.ReadFile(root, packFile)
	if err != nil {
		return nil, packErr("the pack file cannot be read: %v", err)
	}
	var doc contracts.PackV1
	if err := strictDecode(raw, &doc); err != nil {
		return nil, packErr("the pack does not decode: %v", err)
	}
	reg, err := registry.New(doc.Capabilities)
	if err != nil {
		return nil, packErr("the capability registry refuses the pack: %v", err)
	}
	p := &Pack{raw: raw, reg: reg, capRoles: map[string]map[string]struct{}{}, nodes: map[string]nodeAuthority{}, writes: map[string][]string{}, graphRoots: map[string]map[string]struct{}{}, rooted: map[string][]string{}, owned: map[string]nodeOwnership{}, writeNodes: map[string]WriteWhen{}}
	p.ref = string(doc.PackId) + "@" + string(doc.PackVersion)
	for _, d := range doc.Capabilities {
		p.capRoles[string(d.CapabilityId)] = setOf(d.Authority.RolesAllowed)
	}
	if err := p.loadNodes(doc); err != nil {
		return nil, err
	}
	if doc.ActionGraphFiles != nil {
		if err := p.loadGraphs(root, *doc.ActionGraphFiles); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *Pack) loadNodes(doc contracts.PackV1) error {
	profiles := map[string]map[string]struct{}{}
	mustOwn := map[string][]string{}
	for _, tp := range doc.TrustProfiles {
		id := string(tp.ProfileId)
		if _, dup := profiles[id]; dup {
			return packErr("a trust profile is repeated")
		}
		profiles[id] = setOf(tp.Authority.RolesAllowed)
		for _, name := range tp.Authority.PrincipalMustOwn {
			mustOwn[id] = append(mustOwn[id], string(name))
		}
	}
	for _, n := range doc.IntentNodes {
		id := string(n.NodeId)
		if id == "" {
			return packErr("an intent node has no ID")
		}
		if _, dup := p.nodes[id]; dup {
			return packErr("an intent node is repeated")
		}
		p.writeNodes[id] = readWriteWhen(n)
		if n.TrustProfile == nil {
			p.nodes[id] = nodeAuthority{}
			continue
		}
		roles, ok := profiles[*n.TrustProfile]
		if !ok {
			return packErr("an intent node names a trust profile the pack does not have")
		}
		p.nodes[id] = nodeAuthority{profiled: true, roles: roles}
		if own := mustOwn[*n.TrustProfile]; len(own) > 0 {
			p.owned[id] = readOwnership(own, n.Slots)
		}
	}
	return nil
}

func (p *Pack) loadGraphs(root fs.FS, files []string) error {
	seen := map[string]bool{}
	rooted := map[string]map[string]struct{}{}
	for _, rel := range files {
		raw, err := fs.ReadFile(root, rel)
		if err != nil {
			return packErr("a graph file cannot be read: %v", err)
		}
		var g contracts.ActionGraphV2
		if err := strictDecode(raw, &g); err != nil {
			return packErr("a graph file does not decode: %v", err)
		}
		id := string(g.GraphId)
		if id == "" || seen[id] {
			return packErr("a graph has no ID or its ID is repeated")
		}
		seen[id] = true
		if err := p.loadGraph(id, g, rooted); err != nil {
			return err
		}
	}
	for c, set := range rooted {
		p.rooted[c] = sortedKeys(set)
	}
	return nil
}

func (p *Pack) loadGraph(id string, g contracts.ActionGraphV2, rooted map[string]map[string]struct{}) error {
	if len(g.Nodes) == 0 || len(g.Roots) == 0 {
		return packErr("a graph has no node or no root")
	}
	capOf := map[string]string{}
	writes := map[string]struct{}{}
	for _, n := range g.Nodes {
		nid, c := string(n.NodeId), string(n.CapabilityId)
		if _, dup := capOf[nid]; dup || nid == "" {
			return packErr("a graph node has no ID or its ID is repeated")
		}
		if !p.reg.Registered(c) {
			return packErr("a graph node names a capability that is not registered or is prohibited")
		}
		capOf[nid] = c
		if n.Kind == contracts.ActionGraphV2NodesItemKindWRITE {
			writes[c] = struct{}{}
		}
	}
	downstream := map[string][]string{}
	for _, e := range g.Edges {
		from, to := string(e.DependsOn), string(e.NodeId)
		if _, ok := capOf[from]; !ok {
			return packErr("a graph edge names a node the graph does not have")
		}
		if _, ok := capOf[to]; !ok {
			return packErr("a graph edge names a node the graph does not have")
		}
		downstream[from] = append(downstream[from], to)
	}
	p.graphRoots[id] = map[string]struct{}{}
	for _, r := range g.Roots {
		rid := string(r)
		rc, ok := capOf[rid]
		if !ok {
			return packErr("a graph root is not a node of the graph")
		}
		p.graphRoots[id][rc] = struct{}{}
		if rooted[rc] == nil {
			rooted[rc] = map[string]struct{}{}
		}
		for _, nid := range reach(rid, downstream) {
			rooted[rc][capOf[nid]] = struct{}{}
		}
	}
	if len(writes) > 0 {
		p.writes[id] = sortedKeys(writes)
	}
	return nil
}

// reach is the node and every node that depends on it, directly or not (the edges point from a dependency to its dependents). A cycle ends: a node is visited once.
func reach(start string, downstream map[string][]string) []string {
	seen := map[string]bool{start: true}
	order := []string{start}
	for i := 0; i < len(order); i++ {
		for _, next := range downstream[order[i]] {
			if !seen[next] {
				seen[next] = true
				order = append(order, next)
			}
		}
	}
	return order
}

func setOf(list []string) map[string]struct{} {
	s := make(map[string]struct{}, len(list))
	for _, x := range list {
		s[x] = struct{}{}
	}
	return s
}

func sortedKeys(s map[string]struct{}) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// writeCapabilities is the sorted capability IDs of the WRITE nodes of the graph `graphID`, as a copy the caller owns. It is the one reading of "the writes of a graph", shared by the
// escalation set and the authorized set so that they cannot drift (A346). An empty graph ID, a graph the pack does not have and a graph with no WRITE node are ErrEscalationGraph.
func (p *Pack) writeCapabilities(graphID string) ([]string, error) {
	writes := p.writes[graphID]
	if graphID == "" || len(writes) == 0 {
		return nil, fmt.Errorf("%w: the graph named has no write node in the pack", ErrEscalationGraph)
	}
	return append([]string{}, writes...), nil
}

// rootsGraph reports whether the capability is the capability of a root node of the graph `graphID`.
func (p *Pack) rootsGraph(capabilityID, graphID string) bool {
	_, ok := p.graphRoots[graphID][capabilityID]
	return ok
}

func (p *Pack) usable() bool { return p != nil && p.reg != nil }

// RatePerPrincipalPerHour is the rate limit the capability's descriptor declares (REQ-TRU-012), for the Rates adapter. A pack that is not loaded, an unknown or a prohibited
// capability and a descriptor with no limit all answer declared=false.
func (p *Pack) RatePerPrincipalPerHour(id string) (limit int, declared bool) {
	if !p.usable() {
		return 0, false
	}
	return p.reg.RatePerPrincipalPerHour(id)
}

// capability is the registry's answer for an ID: whether it is registered at all (an allowed capability or a prohibited one: a prohibited capability is registered, REQ-CAP-002) and
// whether it is prohibited. IDs match exactly, no case folding and no trimming.
func (p *Pack) capability(id string) (known, prohibited bool) {
	prohibited = p.reg.IsProhibited(id)
	return prohibited || p.reg.Registered(id), prohibited
}
