package trustpack_test

// A684 (interim, relayed; the Handoffs gap of the G1 critical path), A531 (a handoff the pack routes), A551 (read-only accessor on Pack). REQ-TRU-004, REQ-AG-001; INV-14, INV-16.
// The expectation is read from the real fintech pack by code that does not use the package under test. SYNTHETIC.

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

type rawRoute struct {
	ReasonCode  string   `json:"reason_code"`
	OwnerQueue  string   `json:"owner_queue"`
	IntentNodes []string `json:"intent_nodes"`
}

type rawRoutes struct {
	Routes []rawRoute `json:"handoff_routes"`
}

func realRoutes(t testing.TB) []rawRoute {
	t.Helper()
	var r rawRoutes
	if err := jsonUnmarshal(readPackFile(t, packFileName), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Routes) == 0 {
		t.Fatal("the real pack lists no handoff route: the tests have nothing to read")
	}
	return r.Routes
}

func allNodeIDs(t testing.TB) []string {
	t.Helper()
	var out []string
	for _, n := range readRawPack(t).Nodes {
		out = append(out, n.ID)
	}
	return out
}

func newHandoffs(t testing.TB, p *trustpack.Pack) trustpack.Handoffs {
	t.Helper()
	h, err := trustpack.NewHandoffs(p)
	if err != nil {
		t.Fatalf("NewHandoffs: %v", err)
	}
	return h
}

func appliesTo(r rawRoute, node string) bool {
	if len(r.IntentNodes) == 0 {
		return true
	}
	for _, n := range r.IntentNodes {
		if n == node {
			return true
		}
	}
	return false
}

func hctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func routed(t testing.TB, h trustpack.Handoffs, node, reason string) bool {
	t.Helper()
	ok, err := h.Routed(hctx(t), intake.HandoffQuery{TenantID: tenant, IntentNode: node, ReasonCode: reason})
	if err != nil {
		t.Fatalf("Routed(%q, %q): %v", node, reason, err)
	}
	return ok
}

// REQ-TRU-004, A531: a reason the pack routes is routed for each node the route applies to, and only those.
func TestRoutedIsTrueForEveryRouteTheRealPackListsAndForNothingElse(t *testing.T) {
	h := newHandoffs(t, loadReal(t))
	routes := realRoutes(t)
	for _, node := range allNodeIDs(t) {
		for _, r := range routes {
			want := false
			for _, x := range routes {
				if x.ReasonCode == r.ReasonCode && appliesTo(x, node) {
					want = true
				}
			}
			if got := routed(t, h, node, r.ReasonCode); got != want {
				t.Errorf("node %q reason %q: routed = %v, want %v", node, r.ReasonCode, got, want)
			}
		}
	}
}

// A route that lists intent nodes applies to those nodes and to no other (pack.v1 handoff_routes.intent_nodes).
func TestARouteThatListsIntentNodesAppliesToThemOnly(t *testing.T) {
	nodes := allNodeIDs(t)
	if len(nodes) < 2 {
		t.Skip("the pack has fewer than two intent nodes")
	}
	m := files(t)
	m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
		p["handoff_routes"] = []any{map[string]any{"reason_code": realRoutes(t)[0].ReasonCode, "owner_queue": realRoutes(t)[0].OwnerQueue, "intent_nodes": []any{nodes[0]}}}
	})
	h := newHandoffs(t, mustLoadFiles(t, m))
	reason := realRoutes(t)[0].ReasonCode
	if !routed(t, h, nodes[0], reason) {
		t.Errorf("the listed node %q is not routed", nodes[0])
	}
	if routed(t, h, nodes[1], reason) {
		t.Errorf("the node %q is routed though the route does not list it", nodes[1])
	}
}

// A pack with no handoff_routes member routes nothing, and says so without an error (INV-16: the absence of a route is data, not a fault).
func TestAPackWithNoHandoffRoutesRoutesNothing(t *testing.T) {
	m := files(t)
	m[packFileName] = edit(t, m[packFileName], func(p map[string]any) { delete(p, "handoff_routes") })
	p := mustLoadFiles(t, m)
	h := newHandoffs(t, p)
	for _, r := range realRoutes(t) {
		if routed(t, h, allNodeIDs(t)[0], r.ReasonCode) {
			t.Errorf("reason %q is routed by a pack with no route", r.ReasonCode)
		}
	}
	got, err := p.HandoffRoutes()
	if err != nil || len(got) != 0 {
		t.Errorf("HandoffRoutes of a pack with none = %v, %v; want none and no error", got, err)
	}
}

// REQ-TRU-004: the red-first property. A reason the pack does not list is never routed and never an error, for any node and any tenant.
func TestAnUnlistedReasonIsNotRoutedAndNeverAnError(t *testing.T) {
	h := newHandoffs(t, loadReal(t))
	listed := map[string]bool{}
	for _, r := range realRoutes(t) {
		listed[r.ReasonCode] = true
	}
	nodes := allNodeIDs(t)
	rapid.Check(t, func(rt *rapid.T) {
		reason := rapid.StringMatching(`[A-Z][A-Z_]{0,40}`).Draw(rt, "reason")
		if listed[reason] {
			rt.Skip("a listed reason")
		}
		node := rapid.SampledFrom(nodes).Draw(rt, "node")
		ok, err := h.Routed(ctx(t), intake.HandoffQuery{TenantID: tenant, IntentNode: node, ReasonCode: reason})
		if err != nil || ok {
			rt.Fatalf("Routed(%q, %q) = %v, %v; want false and no error", node, reason, ok, err)
		}
	})
}

// The reason is matched exactly: no case folding and no trimming.
func TestTheReasonIsMatchedExactly(t *testing.T) {
	h := newHandoffs(t, loadReal(t))
	r := realRoutes(t)[0]
	node := allNodeIDs(t)[0]
	if !appliesTo(r, node) {
		t.Skip("the first route does not apply to the first node")
	}
	for _, v := range []string{lower(r.ReasonCode), " " + r.ReasonCode, r.ReasonCode + " ", r.ReasonCode + "\n"} {
		if routed(t, h, node, v) {
			t.Errorf("%q is routed", v)
		}
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// A question that cannot be answered is an error, never "routed" and never "not routed" (the contract of intake.Handoffs).
func TestAQuestionThatCannotBeAnsweredIsAnError(t *testing.T) {
	h := newHandoffs(t, loadReal(t))
	node, reason := allNodeIDs(t)[0], realRoutes(t)[0].ReasonCode
	cases := []struct {
		name string
		q    intake.HandoffQuery
		want error
	}{
		{"no tenant", intake.HandoffQuery{IntentNode: node, ReasonCode: reason}, trustpack.ErrSession},
		{"no node", intake.HandoffQuery{TenantID: tenant, ReasonCode: reason}, trustpack.ErrHandoffQuery},
		{"no reason", intake.HandoffQuery{TenantID: tenant, IntentNode: node}, trustpack.ErrHandoffQuery},
		{"a node the pack does not have", intake.HandoffQuery{TenantID: tenant, IntentNode: node + "_absent", ReasonCode: reason}, trustpack.ErrIntent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, err := h.Routed(ctx(t), c.q)
			if !errors.Is(err, c.want) || ok {
				t.Errorf("Routed = %v, %v; want false and %v", ok, err, c.want)
			}
		})
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, err := h.Routed(cancelled, intake.HandoffQuery{TenantID: tenant, IntentNode: node, ReasonCode: reason}); !errors.Is(err, context.Canceled) || ok {
		t.Errorf("Routed on a cancelled context = %v, %v; want false and context.Canceled", ok, err)
	}
}

// The zero value and a nil pack refuse (ErrConfig): nothing is ever answered from nothing.
func TestTheZeroAdapterAndANilPackRefuse(t *testing.T) {
	var zero trustpack.Handoffs
	if ok, err := zero.Routed(ctx(t), intake.HandoffQuery{TenantID: tenant, IntentNode: "n", ReasonCode: "r"}); !errors.Is(err, trustpack.ErrConfig) || ok {
		t.Errorf("zero Handoffs = %v, %v; want false and ErrConfig", ok, err)
	}
	if _, err := trustpack.NewHandoffs(nil); !errors.Is(err, trustpack.ErrConfig) {
		t.Errorf("NewHandoffs(nil) = %v; want ErrConfig", err)
	}
	var nilPack *trustpack.Pack
	if r, err := nilPack.HandoffRoutes(); !errors.Is(err, trustpack.ErrConfig) || r != nil {
		t.Errorf("(*Pack)(nil).HandoffRoutes() = %v, %v; want none and ErrConfig", r, err)
	}
}

// INV-16: a pack whose routes cannot give one answer is refused whole. The same reason code routed twice to the same node (or to every node) is ambiguous.
func TestAPackThatRoutesOneReasonTwiceToTheSameNodeIsRefused(t *testing.T) {
	r := realRoutes(t)[0]
	nodes := allNodeIDs(t)
	for name, routes := range map[string][]any{
		"twice to every node":      {map[string]any{"reason_code": r.ReasonCode, "owner_queue": "Q_ONE"}, map[string]any{"reason_code": r.ReasonCode, "owner_queue": "Q_TWO"}},
		"to every node and a node": {map[string]any{"reason_code": r.ReasonCode, "owner_queue": "Q_ONE"}, map[string]any{"reason_code": r.ReasonCode, "owner_queue": "Q_TWO", "intent_nodes": []any{nodes[0]}}},
	} {
		t.Run(name, func(t *testing.T) {
			m := files(t)
			m[packFileName] = edit(t, m[packFileName], func(p map[string]any) { p["handoff_routes"] = routes })
			if _, err := loadFiles(t, m); !errors.Is(err, trustpack.ErrPack) {
				t.Errorf("LoadPack = %v; want ErrPack", err)
			}
		})
	}
	if len(nodes) >= 2 {
		m := files(t)
		m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
			p["handoff_routes"] = []any{
				map[string]any{"reason_code": r.ReasonCode, "owner_queue": "Q_ONE", "intent_nodes": []any{nodes[0]}},
				map[string]any{"reason_code": r.ReasonCode, "owner_queue": "Q_TWO", "intent_nodes": []any{nodes[1]}},
			}
		})
		if _, err := loadFiles(t, m); err != nil {
			t.Errorf("one reason routed to two different nodes is refused: %v", err)
		}
	}
}

// A route that names an intent node the pack does not have is refused: it could never apply (INV-16).
func TestARouteThatNamesAnUnknownIntentNodeIsRefused(t *testing.T) {
	r := realRoutes(t)[0]
	m := files(t)
	m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
		p["handoff_routes"] = []any{map[string]any{"reason_code": r.ReasonCode, "owner_queue": r.OwnerQueue, "intent_nodes": []any{allNodeIDs(t)[0] + "_absent"}}}
	})
	if _, err := loadFiles(t, m); !errors.Is(err, trustpack.ErrPack) {
		t.Errorf("LoadPack = %v; want ErrPack", err)
	}
}

// A551: the accessor is read-only (the answer is a copy), derived from the pack bytes, sorted, and complete.
func TestHandoffRoutesIsTheRealPacksRoutesAsACopy(t *testing.T) {
	p := loadReal(t)
	got, err := p.HandoffRoutes()
	if err != nil {
		t.Fatal(err)
	}
	var want []trustpack.HandoffRoute
	for _, r := range realRoutes(t) {
		want = append(want, trustpack.HandoffRoute{ReasonCode: r.ReasonCode, OwnerQueue: r.OwnerQueue, IntentNodes: append([]string(nil), r.IntentNodes...)})
	}
	sort.Slice(want, func(i, j int) bool {
		if want[i].ReasonCode != want[j].ReasonCode {
			return want[i].ReasonCode < want[j].ReasonCode
		}
		return want[i].OwnerQueue < want[j].OwnerQueue
	})
	for i := range want {
		sort.Strings(want[i].IntentNodes)
		if len(want[i].IntentNodes) == 0 {
			want[i].IntentNodes = nil
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HandoffRoutes = %+v; want %+v", got, want)
	}
	got[0].ReasonCode, got[0].OwnerQueue = "CHANGED", "CHANGED"
	got[0].IntentNodes = append(got[0].IntentNodes, "CHANGED")
	again, err := p.HandoffRoutes()
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("changing the answer changed the pack: %+v, %v", again, err)
	}
}

// INV-14: the package names no reason code and no owner queue the pack lists. The scan reads every string literal of the non-test source.
func TestNoSourceFileNamesAReasonCodeOrAnOwnerQueueOfThePack(t *testing.T) {
	banned := map[string]bool{}
	for _, r := range realRoutes(t) {
		banned[r.ReasonCode], banned[r.OwnerQueue] = true, true
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if len(f) > 8 && f[len(f)-8:] == "_test.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(lit.Value); err == nil && banned[s] {
				t.Errorf("%s names %q", f, s)
			}
			return true
		})
	}
}
