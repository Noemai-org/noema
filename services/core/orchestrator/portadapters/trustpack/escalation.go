package trustpack

import "github.com/Noemai-org/noema/services/core/trust/recorder"

// EscalationSetFromPack builds the Record writer's escalation set from the WRITE nodes of the pack graph `graphID`.
func EscalationSetFromPack(p *Pack, graphID string) (recorder.EscalationSet, error) {
	return recorder.EscalationSet{}, errNotBuilt
}
