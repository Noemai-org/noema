package trustpack_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Proves REQ-CAP-001, REQ-CAP-002, REQ-TRU-004, REQ-TRU-005; INV-05, INV-14, INV-16, INV-18, BD-08, A255 (the role), A306 (the authorized set).

// newCaps builds the adapter with the pack's escalation graph as the named graph (found by shape, never by a literal ID): the graph ID is configuration (A346).
func newCaps(t testing.TB, p *trustpack.Pack, r trustpack.Remedies) trustpack.Capabilities {
	t.Helper()
	return newCapsFor(t, p, r, escalateGraph(t).ID)
}

func newCapsFor(t testing.TB, p *trustpack.Pack, r trustpack.Remedies, graphID string) trustpack.Capabilities {
	t.Helper()
	c, err := trustpack.NewCapabilities(p, r, graphID)
	if err != nil {
		t.Fatalf("NewCapabilities: %v", err)
	}
	return c
}

func TestCapabilitiesNeedAPackAndAPort(t *testing.T) {
	if _, err := trustpack.NewCapabilities(nil, &remedySpy{}, escalateGraph(t).ID); !errors.Is(err, trustpack.ErrConfig) {
		t.Fatalf("no pack: %v", err)
	}
	if _, err := trustpack.NewCapabilities(loadReal(t), nil, escalateGraph(t).ID); !errors.Is(err, trustpack.ErrConfig) {
		t.Fatalf("no remedies port: %v", err)
	}
}

// A zero value is not an adapter: it never answers "allowed" and never "none" (INV-16).
func TestAZeroCapabilitiesAnswersNothing(t *testing.T) {
	var c trustpack.Capabilities
	if _, _, err := c.Requested(ctx(t), admitted("n")); !errors.Is(err, trustpack.ErrConfig) {
		t.Fatalf("Requested: %v", err)
	}
	if _, err := c.Check(ctx(t), "c", "customer"); !errors.Is(err, trustpack.ErrConfig) {
		t.Fatalf("Check: %v", err)
	}
	if ids, err := c.Authorized(ctx(t), admitted("n")); !errors.Is(err, trustpack.ErrConfig) || ids != nil {
		t.Fatalf("Authorized: %v, %v", ids, err)
	}
}

func TestRequestedNamesNoCapabilityWhenTheIntentRequestsNone(t *testing.T) {
	spy := &remedySpy{present: false}
	c := newCaps(t, loadReal(t), spy)
	got, present, err := c.Requested(ctx(t), admitted("n"))
	if err != nil || present || got != (intake.Capability{}) {
		t.Fatalf("Requested = %+v, %v, %v; want none", got, present, err)
	}
}

// T4: an allowed capability is registered and not prohibited; an unknown one is not registered (INV-18); a prohibited one is registered and prohibited and is never allowed
// (REQ-CAP-002, BD-08). Every capability of the pack is asked, and the answer is the pack's own.
func TestRequestedAnswersWhatTheRegistryKnowsForEveryCapabilityOfThePack(t *testing.T) {
	pack := readRawPack(t)
	for _, rc := range pack.Caps {
		spy := &remedySpy{id: rc.ID, present: true}
		c := newCaps(t, loadReal(t), spy)
		got, present, err := c.Requested(ctx(t), admitted("n"))
		if err != nil || !present {
			t.Fatalf("%s: Requested = %+v, %v, %v", rc.ID, got, present, err)
		}
		if got.ID != rc.ID || !got.Registered || got.Prohibited != rc.Prohibited {
			t.Errorf("%s: Requested = %+v; want registered, prohibited=%v", rc.ID, got, rc.Prohibited)
		}
	}
}

func TestRequestedRefusesWhatTheRegistryDoesNotKnow(t *testing.T) {
	for _, id := range []string{"x.cap.unknown", "", " ", pack0ID(t) + "x"} {
		spy := &remedySpy{id: id, present: true}
		c := newCaps(t, loadReal(t), spy)
		got, present, err := c.Requested(ctx(t), admitted("n"))
		if err != nil || !present || got.Registered || got.Prohibited || got.ID != id {
			t.Errorf("remedy %q: Requested = %+v, %v, %v; want present, not registered", id, got, present, err)
		}
	}
}

// IDs match exactly: no case folding, no trimming (the registry's rule).
func TestRequestedMatchesIDsExactly(t *testing.T) {
	real := allowedCaps(t)[0].ID
	for _, id := range []string{upper(real), real + " ", " " + real} {
		c := newCaps(t, loadReal(t), &remedySpy{id: id, present: true})
		if got, _, _ := c.Requested(ctx(t), admitted("n")); got.Registered {
			t.Errorf("%q is registered", id)
		}
	}
}

// The adapter asks the store for the admitted intent's own tenant, ID and version, never for another's (REQ-TRU-003).
func TestRequestedAsksForTheAdmittedIntentOnly(t *testing.T) {
	spy := &remedySpy{}
	c := newCaps(t, loadReal(t), spy)
	a := admitted("n")
	a.TenantID = otherTenant
	if _, _, err := c.Requested(ctx(t), a); err != nil {
		t.Fatal(err)
	}
	if len(spy.asked) != 1 || spy.asked[0] != otherTenant+"/"+intentIDForTest {
		t.Fatalf("asked = %v", spy.asked)
	}
}

func TestRequestedFailsClosedOnAFailedStoreAndOnAnIntentWithNoIdentity(t *testing.T) {
	boom := errors.New("store down")
	c := newCaps(t, loadReal(t), &remedySpy{err: boom, id: allowedCaps(t)[0].ID, present: true})
	if got, present, err := c.Requested(ctx(t), admitted("n")); !errors.Is(err, boom) || present || got != (intake.Capability{}) {
		t.Fatalf("a failed store: %+v, %v, %v", got, present, err)
	}
	c = newCaps(t, loadReal(t), &remedySpy{})
	for name, a := range map[string]intake.AdmittedIntent{
		"no tenant":  {IntentID: intentIDForTest, Version: 1},
		"no intent":  {TenantID: tenant, Version: 1},
		"no version": {TenantID: tenant, IntentID: intentIDForTest},
	} {
		if _, _, err := c.Requested(ctx(t), a); !errors.Is(err, trustpack.ErrIntent) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := c.Authorized(ctx(t), a); !errors.Is(err, trustpack.ErrIntent) {
			t.Errorf("%s (Authorized): %v", name, err)
		}
	}
}

// Check is the registry's answer for one capability and whether the session's ROLE may use it, from the descriptor's roles (A255). Every capability of the pack against every role
// of the matrix, the expectation read independently from the pack: allowed only when the capability is not prohibited and the descriptor lists the role, exactly.
func TestCheckRoleMatrixOverEveryCapabilityOfThePack(t *testing.T) {
	c := newCaps(t, loadReal(t), &remedySpy{})
	for _, rc := range readRawPack(t).Caps {
		for _, role := range roles {
			got, err := c.Check(ctx(t), rc.ID, role)
			if err != nil {
				t.Fatalf("%s/%q: %v", rc.ID, role, err)
			}
			want := !rc.Prohibited && contains(rc.Authority.Roles, role)
			if got.ID != rc.ID || !got.Registered || got.Prohibited != rc.Prohibited || got.RoleAllowed != want {
				t.Errorf("Check(%s, %q) = %+v; want registered, prohibited=%v, role allowed=%v", rc.ID, role, got, rc.Prohibited, want)
			}
		}
	}
}

// BD-08: a prohibited capability is never allowed, for any role, even one its own descriptor lists.
func TestAProhibitedCapabilityIsNeverAllowedForAnyRole(t *testing.T) {
	c := newCaps(t, loadReal(t), &remedySpy{})
	for _, id := range prohibitedCaps(t) {
		for _, role := range append([]string{"customer", "ops"}, roles...) {
			got, err := c.Check(ctx(t), id, role)
			if err != nil || got.RoleAllowed || !got.Prohibited || !got.Registered {
				t.Errorf("Check(%s, %q) = %+v, %v", id, role, got, err)
			}
		}
	}
}

func TestCheckOfAnUnknownCapabilityIsNotRegisteredAndNotAllowed(t *testing.T) {
	c := newCaps(t, loadReal(t), &remedySpy{})
	for _, id := range []string{"x.cap.unknown", "", upper(allowedCaps(t)[0].ID)} {
		for _, role := range []string{"customer", "ops", ""} {
			got, err := c.Check(ctx(t), id, role)
			if err != nil || got.Registered || got.Prohibited || got.RoleAllowed {
				t.Errorf("Check(%q, %q) = %+v, %v", id, role, got, err)
			}
		}
	}
}

// A second source: the pack's derived role map (role_capabilities.json, derived from the descriptors) agrees with the adapter, in both directions.
func TestCheckAgreesWithTheDerivedRoleMap(t *testing.T) {
	var m struct {
		Roles map[string][]string `json:"roles"`
	}
	if err := jsonUnmarshal(readPackFile(t, "fintech/role_capabilities.json"), &m); err != nil || len(m.Roles) == 0 {
		t.Fatalf("role map: %v", err)
	}
	c := newCaps(t, loadReal(t), &remedySpy{})
	for role, ids := range m.Roles {
		var got []string
		for _, rc := range readRawPack(t).Caps {
			if r, _ := c.Check(ctx(t), rc.ID, role); r.RoleAllowed {
				got = append(got, rc.ID)
			}
		}
		sort.Strings(got)
		want := append([]string(nil), ids...)
		sort.Strings(want)
		if !equal(got, want) {
			t.Errorf("role %q: the adapter allows %v, the derived map %v", role, got, want)
		}
	}
}

// A306, A346: the set is the capabilities of the pack graph the intent's capability roots, as the pack's own data says; for the graph the composition root names (the escalation
// graph) it is that graph's WRITE capabilities.
func TestAuthorizedIsExactlyTheCapabilitiesOfTheGraphTheRequestedCapabilityRoots(t *testing.T) {
	for _, g := range readRawGraphs(t) {
		for _, root := range g.Roots {
			id := nodeCap(g, root)
			c := newCaps(t, loadReal(t), &remedySpy{id: id, present: true})
			got, err := c.Authorized(ctx(t), admitted("n"))
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			want := closure(g, root)
			if g.ID == escalateGraph(t).ID {
				want = writeCapsOf(g)
			}
			if !equal(got, want) || !sort.StringsAreSorted(got) {
				t.Errorf("graph %s, root %s: Authorized = %v; want %v", g.ID, id, got, want)
			}
		}
	}
}

func TestAuthorizedOfTheEscalationRootIsTheThreeWritesOfItsGraph(t *testing.T) {
	g := escalateGraph(t)
	c := newCaps(t, loadReal(t), &remedySpy{id: nodeCap(g, g.Roots[0]), present: true})
	got, err := c.Authorized(ctx(t), admitted("n"))
	if err != nil || len(got) != 3 || !equal(got, writeCapsOf(g)) {
		t.Fatalf("Authorized = %v, %v; want the graph's three write capabilities %v", got, err, writeCapsOf(g))
	}
}

// The investigation graph's reads are in the set of its root ("reads included"), and none of the other graphs' capabilities is.
func TestAuthorizedOfTheInvestigationRootHoldsItsReadsAndNothingOfOtherGraphs(t *testing.T) {
	g := investigateGraph(t)
	c := newCaps(t, loadReal(t), &remedySpy{id: nodeCap(g, g.Roots[0]), present: true})
	got, err := c.Authorized(ctx(t), admitted("n"))
	if err != nil || !equal(got, capsOf(g)) {
		t.Fatalf("Authorized = %v, %v; want %v", got, err, capsOf(g))
	}
	for _, other := range []rawGraph{escalateGraph(t), complaintGraph(t)} {
		for _, id := range capsOf(other) {
			if contains(got, id) {
				t.Errorf("%s of graph %s is authorized by the investigation root", id, other.ID)
			}
		}
	}
}

// Decision (interim, for the Architect): a capability the registry does not know, or a prohibited one, roots no authorization and is an error, so the intake ends in UNKNOWN; it is
// never "none" and never a set. T4 refuses such a capability before the set is read; this is the second wall (INV-16, BD-08).
func TestAuthorizedRefusesAnUnknownOrProhibitedCapabilityWithAnError(t *testing.T) {
	ids := append([]string{"x.cap.unknown", "", " "}, prohibitedCaps(t)...)
	for _, id := range ids {
		c := newCaps(t, loadReal(t), &remedySpy{id: id, present: true})
		got, err := c.Authorized(ctx(t), admitted("n"))
		if !errors.Is(err, trustpack.ErrCapability) || got != nil {
			t.Errorf("remedy %q: Authorized = %v, %v; want nil and ErrCapability", id, got, err)
		}
	}
}

// Decision (interim, for the Architect): a registered capability that roots no graph, and an intent that requests none, authorize nothing beyond the intent's own capability, which
// the intake adds itself: an empty set and no error. That is the narrowest answer; the plan check (T14) then refuses any step outside the intent's own capability.
func TestAuthorizedOfACapabilityThatRootsNoGraphIsEmptyAndOfNoRemedyIsEmpty(t *testing.T) {
	roots := map[string]bool{}
	for _, g := range readRawGraphs(t) {
		for _, r := range g.Roots {
			roots[nodeCap(g, r)] = true
		}
	}
	n := 0
	for _, rc := range allowedCaps(t) {
		if roots[rc.ID] {
			continue
		}
		n++
		c := newCaps(t, loadReal(t), &remedySpy{id: rc.ID, present: true})
		if got, err := c.Authorized(ctx(t), admitted("n")); err != nil || len(got) != 0 {
			t.Errorf("%s (roots no graph): Authorized = %v, %v; want empty", rc.ID, got, err)
		}
	}
	if n == 0 {
		t.Fatal("every capability of the pack roots a graph: the case is not exercised")
	}
	c := newCaps(t, loadReal(t), &remedySpy{present: false})
	if got, err := c.Authorized(ctx(t), admitted("n")); err != nil || len(got) != 0 {
		t.Errorf("no remedy: Authorized = %v, %v; want empty", got, err)
	}
}

func TestAuthorizedFailsClosedOnAFailedStore(t *testing.T) {
	boom := errors.New("store down")
	c := newCaps(t, loadReal(t), &remedySpy{err: boom})
	if got, err := c.Authorized(ctx(t), admitted("n")); !errors.Is(err, boom) || got != nil {
		t.Fatalf("Authorized = %v, %v", got, err)
	}
}

// A prohibited capability is in no authorized set the pack can give (BD-08): checked over every root of the real pack.
func TestNoAuthorizedSetHoldsAProhibitedCapability(t *testing.T) {
	prohibited := prohibitedCaps(t)
	for _, rc := range allowedCaps(t) {
		c := newCaps(t, loadReal(t), &remedySpy{id: rc.ID, present: true})
		got, err := c.Authorized(ctx(t), admitted("n"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range prohibited {
			if contains(got, p) {
				t.Errorf("the set of %s holds the prohibited %s", rc.ID, p)
			}
		}
	}
}

// What the caller gets is its own: changing it does not change the next answer (INV-21: the same input, the same answer).
func TestAuthorizedHandsOutACopy(t *testing.T) {
	g := escalateGraph(t)
	c := newCaps(t, loadReal(t), &remedySpy{id: nodeCap(g, g.Roots[0]), present: true})
	first, err := c.Authorized(ctx(t), admitted("n"))
	if err != nil || len(first) == 0 {
		t.Fatal(first, err)
	}
	first[0] = "tampered"
	second, _ := c.Authorized(ctx(t), admitted("n"))
	if contains(second, "tampered") {
		t.Fatalf("a caller changed the adapter's answer: %v", second)
	}
}

// The set follows the graph's edges (for a root of a graph other than the named one): from a root that is not the first node, only the nodes that depend on it (a graph with two declared roots, a variant of the real escalation graph).
func TestAuthorizedFollowsTheEdgesFromTheRootOnly(t *testing.T) {
	m := files(t)
	path := escalateGraphPath(t)
	g := escalateGraph(t)
	second := g.Nodes[1].ID // the node that depends on the first
	m[path] = edit(t, m[path], func(x map[string]any) { x["roots"] = []any{g.Roots[0], second} })
	p := mustLoadFiles(t, m)
	named := complaintGraph(t).ID // a root that is not a root of the named graph keeps the downstream rule
	c := newCapsFor(t, p, &remedySpy{id: nodeCap(g, second), present: true}, named)
	got, err := c.Authorized(ctx(t), admitted("n"))
	if err != nil || !equal(got, closure(g, second)) || len(got) != 2 {
		t.Fatalf("Authorized(second root) = %v, %v; want %v (two of the three)", got, err, closure(g, second))
	}
	c = newCapsFor(t, p, &remedySpy{id: nodeCap(g, g.Roots[0]), present: true}, named)
	if got, err := c.Authorized(ctx(t), admitted("n")); err != nil || len(got) != 3 {
		t.Fatalf("Authorized(first root) = %v, %v; want three", got, err)
	}
}

// Two graphs rooted at one capability that is not a root of the named graph: the set is the union of both (a variant of the real pack with a second graph file).
func TestAuthorizedIsTheUnionOfTheGraphsOneCapabilityRoots(t *testing.T) {
	m := files(t)
	path := escalateGraphPath(t)
	g := escalateGraph(t)
	read := investigateGraph(t).Nodes[0].Capability
	m["extra_graph.json"] = edit(t, m[path], func(x map[string]any) {
		x["graph_id"] = "x.graph.extra"
		x["nodes"] = []any{
			map[string]any{"node_id": g.Roots[0], "kind": "WRITE", "capability_id": nodeCap(g, g.Roots[0]), "preconditions": []any{}, "postconditions": []any{}, "on_unknown": "VERIFY_BY_IDEMPOTENCY_KEY", "on_failure": "ESCALATE", "recovery_ref": "VERIFY_BY_IDEMPOTENCY_KEY", "escalation_ref": "HANDOFF"},
			map[string]any{"node_id": "extra_read", "kind": "READ", "capability_id": read, "preconditions": []any{}, "postconditions": []any{}, "on_unknown": "VERIFY_BY_IDEMPOTENCY_KEY", "on_failure": "ESCALATE", "recovery_ref": nil, "escalation_ref": nil},
		}
		x["edges"] = []any{map[string]any{"node_id": "extra_read", "depends_on": g.Roots[0], "condition": nil}}
	})
	m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
		p["action_graph_files"] = append(p["action_graph_files"].([]any), "extra_graph.json")
	})
	c := newCapsFor(t, mustLoadFiles(t, m), &remedySpy{id: nodeCap(g, g.Roots[0]), present: true}, complaintGraph(t).ID)
	got, err := c.Authorized(ctx(t), admitted("n"))
	want := append(capsOf(g), read)
	sort.Strings(want)
	if err != nil || !equal(got, want) {
		t.Fatalf("Authorized = %v, %v; want the union %v", got, err, want)
	}
}

func pack0ID(t testing.TB) string { return allowedCaps(t)[0].ID }

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// A call whose context is already done is an error, never an answer: no adapter answers a question nobody is waiting for (the context deadline of every call, AGENTS 7).
func TestACancelledContextIsAnErrorForEveryAdapter(t *testing.T) {
	done, cancel := context.WithCancel(context.Background())
	cancel()
	node := firstNodeWithProfile(t).ID
	spy := &remedySpy{id: allowedCaps(t)[0].ID, present: true}
	c := newCaps(t, loadReal(t), spy)
	if _, _, err := c.Requested(done, admitted(node)); !errors.Is(err, context.Canceled) {
		t.Errorf("Requested: %v", err)
	}
	if _, err := c.Check(done, allowedCaps(t)[0].ID, "customer"); !errors.Is(err, context.Canceled) {
		t.Errorf("Check: %v", err)
	}
	if ids, err := c.Authorized(done, admitted(node)); !errors.Is(err, context.Canceled) || ids != nil {
		t.Errorf("Authorized: %v, %v", ids, err)
	}
	if len(spy.asked) != 0 {
		t.Errorf("the store was asked %d times under a done context", len(spy.asked))
	}
	if ok, err := newScope(t, loadReal(t)).InScope(done, tenant, node); ok || !errors.Is(err, context.Canceled) {
		t.Errorf("InScope: %v, %v", ok, err)
	}
	if ok, err := newAuthority(t, loadReal(t)).RoleAllowed(done, node, "customer"); ok || !errors.Is(err, context.Canceled) {
		t.Errorf("RoleAllowed: %v, %v", ok, err)
	}
}

// A346: the constructor takes the escalation graph's ID from configuration and fails closed on one it cannot use: no pack is ErrConfig; an empty ID, an unknown one, one with other
// case and a graph with no WRITE node are ErrEscalationGraph, and no adapter is built (the zero value answers nothing).
func TestNewCapabilitiesFailsClosedOnAGraphIDItCannotUse(t *testing.T) {
	for name, id := range map[string]string{
		"empty":         "",
		"unknown":       "x.graph.missing",
		"other case":    upper(escalateGraph(t).ID),
		"padded":        escalateGraph(t).ID + " ",
		"no write node": investigateGraph(t).ID,
	} {
		c, err := trustpack.NewCapabilities(loadReal(t), &remedySpy{}, id)
		if !errors.Is(err, trustpack.ErrEscalationGraph) {
			t.Errorf("%s: err = %v; want ErrEscalationGraph", name, err)
		}
		if _, aerr := c.Authorized(ctx(t), admitted("n")); !errors.Is(aerr, trustpack.ErrConfig) {
			t.Errorf("%s: the adapter built with an unusable graph answers: %v", name, aerr)
		}
	}
	if _, err := trustpack.NewCapabilities(nil, &remedySpy{}, ""); !errors.Is(err, trustpack.ErrConfig) {
		t.Errorf("no pack and no graph ID: err = %v; want ErrConfig", err)
	}
}

// A346: the set for an intent whose capability roots the named graph is that graph's WRITE capabilities and no read or internal one, whatever else the graph holds (a variant of
// the real escalation graph with a READ node and an INTERNAL node that depend on the first write).
func TestAuthorizedOfTheNamedGraphHoldsItsWritesOnly(t *testing.T) {
	m := files(t)
	path := escalateGraphPath(t)
	g := escalateGraph(t)
	read := investigateGraph(t).Nodes[0].Capability
	var internal string
	for _, n := range investigateGraph(t).Nodes {
		if n.Kind == "INTERNAL" {
			internal = n.Capability
		}
	}
	if internal == "" {
		t.Fatal("the investigation graph has no INTERNAL node")
	}
	m[path] = edit(t, m[path], func(x map[string]any) {
		nodes := x["nodes"].([]any)
		x["nodes"] = append(nodes,
			map[string]any{"node_id": "extra_read", "kind": "READ", "capability_id": read, "preconditions": []any{}, "postconditions": []any{}, "on_unknown": "VERIFY_BY_IDEMPOTENCY_KEY", "on_failure": "ESCALATE", "recovery_ref": nil, "escalation_ref": nil},
			map[string]any{"node_id": "extra_internal", "kind": "INTERNAL", "capability_id": internal, "preconditions": []any{}, "postconditions": []any{}, "on_unknown": "VERIFY_BY_IDEMPOTENCY_KEY", "on_failure": "ESCALATE", "recovery_ref": nil, "escalation_ref": nil})
		x["edges"] = append(x["edges"].([]any), map[string]any{"node_id": "extra_read", "depends_on": g.Roots[0], "condition": nil},
			map[string]any{"node_id": "extra_internal", "depends_on": "extra_read", "condition": nil})
	})
	c := newCaps(t, mustLoadFiles(t, m), &remedySpy{id: nodeCap(g, g.Roots[0]), present: true})
	got, err := c.Authorized(ctx(t), admitted("n"))
	if err != nil || !equal(got, writeCapsOf(g)) || contains(got, read) || contains(got, internal) {
		t.Fatalf("Authorized = %v, %v; want the writes only %v", got, err, writeCapsOf(g))
	}
}

// A346: the set does not depend on the order of the nodes in the file: it is sorted (a variant of the real escalation graph with its nodes in reverse order).
func TestAuthorizedOfTheNamedGraphIsSortedWhateverTheNodeOrder(t *testing.T) {
	m := files(t)
	path := escalateGraphPath(t)
	g := escalateGraph(t)
	m[path] = edit(t, m[path], func(x map[string]any) {
		nodes := x["nodes"].([]any)
		for i, j := 0, len(nodes)-1; i < j; i, j = i+1, j-1 {
			nodes[i], nodes[j] = nodes[j], nodes[i]
		}
	})
	c := newCaps(t, mustLoadFiles(t, m), &remedySpy{id: nodeCap(g, g.Roots[0]), present: true})
	got, err := c.Authorized(ctx(t), admitted("n"))
	if err != nil || !equal(got, writeCapsOf(g)) || !sort.StringsAreSorted(got) {
		t.Fatalf("Authorized = %v, %v; want %v sorted", got, err, writeCapsOf(g))
	}
}

// A346: the escalation set (REQ-TRU-010) and the authorized set are one set: for every capability of the pack the Record's set allows it exactly when Authorized lists it, and
// Authorized lists nothing else, so the two cannot drift.
func TestAuthorizedOfTheNamedGraphIsTheEscalationSet(t *testing.T) {
	g := escalateGraph(t)
	pack := loadReal(t)
	set, err := trustpack.EscalationSetFromPack(pack, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := newCaps(t, pack, &remedySpy{id: nodeCap(g, g.Roots[0]), present: true})
	got, err := c.Authorized(ctx(t), admitted("n"))
	if err != nil || len(got) == 0 {
		t.Fatalf("Authorized = %v, %v", got, err)
	}
	inSet := 0
	for _, rc := range readRawPack(t).Caps {
		if set.Contains(rc.ID) {
			inSet++
		}
		if set.Contains(rc.ID) != contains(got, rc.ID) {
			t.Errorf("%s: the escalation set allows it = %v, Authorized lists it = %v", rc.ID, set.Contains(rc.ID), contains(got, rc.ID))
		}
	}
	if inSet != len(got) {
		t.Errorf("the escalation set holds %d of the pack's capabilities, Authorized lists %d", inSet, len(got))
	}
}

// A346: the graph ID decides. Naming each graph with write nodes in turn, the root of that graph gets exactly that graph's writes. In a variant with two declared roots, the second
// root is a root of the named escalation graph and gets all its writes (the set is the graph's, not the root's downstream).
func TestAuthorizedFollowsTheGraphIDTheCompositionRootNames(t *testing.T) {
	for _, g := range readRawGraphs(t) {
		w := writeCapsOf(g)
		if len(w) == 0 {
			continue
		}
		c := newCapsFor(t, loadReal(t), &remedySpy{id: nodeCap(g, g.Roots[0]), present: true}, g.ID)
		got, err := c.Authorized(ctx(t), admitted("n"))
		if err != nil || !equal(got, w) {
			t.Errorf("named graph %s: Authorized = %v, %v; want its writes %v", g.ID, got, err, w)
		}
	}
	m := files(t)
	path := escalateGraphPath(t)
	g := escalateGraph(t)
	second := g.Nodes[1].ID
	m[path] = edit(t, m[path], func(x map[string]any) { x["roots"] = []any{g.Roots[0], second} })
	c := newCaps(t, mustLoadFiles(t, m), &remedySpy{id: nodeCap(g, second), present: true})
	if got, err := c.Authorized(ctx(t), admitted("n")); err != nil || !equal(got, writeCapsOf(g)) {
		t.Fatalf("second root of the named graph: Authorized = %v, %v; want %v", got, err, writeCapsOf(g))
	}
}

// A346: an intent whose capability roots the named graph gets the graph's writes, not a union with another graph that the same capability roots (a variant of the real pack with a
// second graph rooted at the escalation root and holding a read).
func TestAuthorizedOfTheNamedGraphIsNotUnionedWithAnotherGraphOfTheSameRoot(t *testing.T) {
	m := files(t)
	path := escalateGraphPath(t)
	g := escalateGraph(t)
	read := investigateGraph(t).Nodes[0].Capability
	m["extra_graph.json"] = edit(t, m[path], func(x map[string]any) {
		x["graph_id"] = "x.graph.extra"
		x["nodes"] = []any{
			map[string]any{"node_id": g.Roots[0], "kind": "WRITE", "capability_id": nodeCap(g, g.Roots[0]), "preconditions": []any{}, "postconditions": []any{}, "on_unknown": "VERIFY_BY_IDEMPOTENCY_KEY", "on_failure": "ESCALATE", "recovery_ref": "VERIFY_BY_IDEMPOTENCY_KEY", "escalation_ref": "HANDOFF"},
			map[string]any{"node_id": "extra_read", "kind": "READ", "capability_id": read, "preconditions": []any{}, "postconditions": []any{}, "on_unknown": "VERIFY_BY_IDEMPOTENCY_KEY", "on_failure": "ESCALATE", "recovery_ref": nil, "escalation_ref": nil},
		}
		x["edges"] = []any{map[string]any{"node_id": "extra_read", "depends_on": g.Roots[0], "condition": nil}}
	})
	m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
		p["action_graph_files"] = append(p["action_graph_files"].([]any), "extra_graph.json")
	})
	c := newCaps(t, mustLoadFiles(t, m), &remedySpy{id: nodeCap(g, g.Roots[0]), present: true})
	got, err := c.Authorized(ctx(t), admitted("n"))
	if err != nil || !equal(got, writeCapsOf(g)) || contains(got, read) {
		t.Fatalf("Authorized = %v, %v; want only the named graph's writes %v", got, err, writeCapsOf(g))
	}
}
