package trustpack

import (
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// EscalationSetFromPack builds the Record writer's escalation set (recorder.Writer.Escalation, REQ-TRU-010) from the capabilities of the WRITE nodes of the pack graph `graphID`:
// the pack's escalation plan, the only writes an ESCALATE disposition authorises. The pack has no field that names the escalation graph (A319: a `kind` field is a later minor
// change, DEBT-48), so the composition root passes the graph ID from configuration; no ID is written here (INV-14). READ and INTERNAL nodes are not writes and are not in the set.
//
// It fails closed: a nil pack is ErrConfig, and an empty graph ID, a graph the pack does not have and a graph with no WRITE node are ErrEscalationGraph, each with the empty set
// (which allows no step).
func EscalationSetFromPack(p *Pack, graphID string) (recorder.EscalationSet, error) {
	if !p.usable() {
		return recorder.EscalationSet{}, ErrConfig
	}
	writes, err := p.writeCapabilities(graphID)
	if err != nil {
		return recorder.EscalationSet{}, err
	}
	return recorder.NewEscalationSet(writes...), nil
}
