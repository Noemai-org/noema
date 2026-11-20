package trustpack_test

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

// Proves REQ-TRU-010, INV-25 (an ESCALATE disposition authorises the escalation plan's writes and nothing else), INV-14 (the graph is named by the caller, never by the code), A335 (relayed).

// The set is the write nodes of the graph the caller names, compared with the graph file read independently: for every capability of the pack, in the set exactly when the graph
// has a WRITE node for it.
func TestTheEscalationSetIsTheWriteCapabilitiesOfTheNamedGraph(t *testing.T) {
	g := escalateGraph(t)
	want := writeCapsOf(g)
	if len(want) != 3 {
		t.Fatalf("the escalation graph has %d write nodes, want 3 (create case, record evidence, route)", len(want))
	}
	set, err := trustpack.EscalationSetFromPack(loadReal(t), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, rc := range readRawPack(t).Caps {
		if got := set.Contains(rc.ID); got != contains(want, rc.ID) {
			t.Errorf("Contains(%s) = %v; want %v", rc.ID, got, contains(want, rc.ID))
		}
	}
	for _, id := range []string{"", "x.cap.unknown"} {
		if set.Contains(id) {
			t.Errorf("Contains(%q) = true", id)
		}
	}
}

// The graph is a parameter: another graph gives its own writes. The one-node write graph gives one, and the escalation writes are then not in it.
func TestTheEscalationSetFollowsTheGraphIDItIsGiven(t *testing.T) {
	g := complaintGraph(t)
	set, err := trustpack.EscalationSetFromPack(loadReal(t), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, rc := range readRawPack(t).Caps {
		if got := set.Contains(rc.ID); got != contains(writeCapsOf(g), rc.ID) {
			t.Errorf("Contains(%s) = %v", rc.ID, got)
		}
	}
}

// READ and INTERNAL nodes of the graph are not writes: a variant of the escalation graph with a read node added still gives the three writes.
func TestOnlyWriteNodesAreInTheEscalationSet(t *testing.T) {
	m := files(t)
	path := escalateGraphPath(t)
	g := escalateGraph(t)
	read := investigateGraph(t).Nodes[0].Capability
	m[path] = edit(t, m[path], func(x map[string]any) {
		x["nodes"] = append(x["nodes"].([]any), map[string]any{"node_id": "extra_read", "kind": "READ", "capability_id": read, "preconditions": []any{}, "postconditions": []any{}, "on_unknown": "VERIFY_BY_IDEMPOTENCY_KEY", "on_failure": "ESCALATE", "recovery_ref": nil, "escalation_ref": nil})
		x["edges"] = append(x["edges"].([]any), map[string]any{"node_id": "extra_read", "depends_on": g.Roots[0], "condition": nil})
	})
	set, err := trustpack.EscalationSetFromPack(mustLoadFiles(t, m), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if set.Contains(read) {
		t.Fatalf("the read capability %s is in the escalation set", read)
	}
	for _, id := range writeCapsOf(g) {
		if !set.Contains(id) {
			t.Errorf("the write %s is not in the set", id)
		}
	}
}

// Fail closed: no pack, no graph ID, a graph the pack does not have, and a graph with no WRITE node each give an error and the empty set, never a set that allows something.
func TestTheEscalationSetFailsClosed(t *testing.T) {
	real := loadReal(t)
	cases := []struct {
		name string
		p    *trustpack.Pack
		id   string
		want error
	}{
		{"no pack", nil, escalateGraph(t).ID, trustpack.ErrConfig},
		{"no graph ID", real, "", trustpack.ErrEscalationGraph},
		{"a graph the pack does not have", real, "x.graph.missing", trustpack.ErrEscalationGraph},
		{"a graph with no write node", real, investigateGraph(t).ID, trustpack.ErrEscalationGraph},
		{"the graph ID with other case", real, upper(escalateGraph(t).ID), trustpack.ErrEscalationGraph},
	}
	for _, c := range cases {
		set, err := trustpack.EscalationSetFromPack(c.p, c.id)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v; want %v", c.name, err, c.want)
		}
		for _, rc := range readRawPack(t).Caps {
			if set.Contains(rc.ID) {
				t.Errorf("%s: the empty set allows %s", c.name, rc.ID)
			}
		}
	}
}

// A pack whose escalation graph lost its write nodes gives no set (a variant of the real pack with the writes made reads).
func TestAnEscalationGraphWithItsWritesMadeReadsGivesNoSet(t *testing.T) {
	m := files(t)
	path := escalateGraphPath(t)
	m[path] = edit(t, m[path], func(x map[string]any) {
		for _, n := range x["nodes"].([]any) {
			n.(map[string]any)["kind"] = "READ"
		}
	})
	if _, err := trustpack.EscalationSetFromPack(mustLoadFiles(t, m), escalateGraph(t).ID); !errors.Is(err, trustpack.ErrEscalationGraph) {
		t.Fatalf("err = %v; want ErrEscalationGraph", err)
	}
}
