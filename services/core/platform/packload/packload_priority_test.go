package packload_test

// PCK-09 (ruling A644, interim, relayed, not yet committed; A333; INV-14, INV-21): the manifest may list one file of kind `priority_routing`. It is hash-pinned like the others, a second
// one beside it is refused, and a refused pack returns none of it. A manifest that lists none still loads (the member is optional to the loader; packcheck decides what the committed pack lists).

import (
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/packload"
)

const (
	prioRel  = "demo/routing/priority_routing.v1.json"
	prioBody = `{"table_id":"demo.priority_routing","version":1,"label":"SYNTHETIC","phases":{"RUNNING":{"priority":"LOW","owner_queue":"QUEUE_A"}}}`
)

func withPriority(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.write(prioRel, prioBody)
	w.list = append(w.list, entry{"priority_routing", prioRel, sum(prioBody)})
	w.seal()
	return w
}

func noContent(t *testing.T, what string, p packload.Pack) {
	t.Helper()
	if len(p.Pack)+len(p.EvidenceMapping)+len(p.ReconRules)+len(p.PriorityRouting) != 0 {
		t.Errorf("%s: a refused pack returned content", what)
	}
}

func TestAListedPriorityRoutingMemberLoadsWithItsVerifiedBytes(t *testing.T) {
	p, err := withPriority(t).load()
	if err != nil {
		t.Fatalf("a good pack with a priority routing member was refused: %v", err)
	}
	if string(p.PriorityRouting) != prioBody {
		t.Errorf("PriorityRouting = %q; want the pinned bytes", p.PriorityRouting)
	}
}

func TestAManifestWithNoPriorityRoutingMemberStillLoadsAndReturnsNone(t *testing.T) {
	p, err := newWorld(t).load()
	if err != nil {
		t.Fatal(err)
	}
	if p.PriorityRouting != nil {
		t.Errorf("PriorityRouting = %q; want nil", p.PriorityRouting)
	}
}

func TestAPriorityRoutingMemberChangedAfterTheManifestIsRefused(t *testing.T) {
	w := withPriority(t)
	w.write(prioRel, strings.Replace(prioBody, "LOW", "HIGH", 1))
	p, err := w.load()
	if !errors.Is(err, packload.ErrHash) {
		t.Fatalf("got %v; want ErrHash", err)
	}
	noContent(t, "changed member", p)
	var pe *packload.Error
	if !errors.As(err, &pe) || pe.Kind != "priority_routing" || !strings.Contains(err.Error(), "(priority_routing)") {
		t.Errorf("the refusal does not name the kind: %v", err)
	}
}

func TestASecondFileBesideThePriorityRoutingMemberIsRefused(t *testing.T) {
	w := withPriority(t)
	w.write("demo/routing/priority_routing.v2.json", prioBody)
	p, err := w.load()
	if !errors.Is(err, packload.ErrUnlisted) {
		t.Fatalf("got %v; want ErrUnlisted", err)
	}
	noContent(t, "second member", p)
}

func TestTwoPriorityRoutingEntriesInTheManifestAreRefused(t *testing.T) {
	w := withPriority(t)
	w.write("demo/routing/other.json", prioBody)
	w.list = append(w.list, entry{"priority_routing", "demo/routing/other.json", sum(prioBody)})
	w.seal()
	p, err := w.load()
	if !errors.Is(err, packload.ErrManifest) {
		t.Fatalf("got %v; want ErrManifest", err)
	}
	noContent(t, "two entries", p)
}
