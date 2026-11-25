package main

import (
	"encoding/json"
	"errors"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

// compose_listentities.go builds the list entities the Policies adapter reads (STM-01; A545, interim, relayed, not yet committed: "ListEntities needs no new decision; EL builds it from C2's
// mapping descriptors"; REQ-STM-001, REQ-TRU-006, REQ-CTX-001; INV-14, INV-16, INV-19). A list entity is an entity the evidence mapping lists as one (the mapping says so, by answering
// ListRows for it); its source is the one its evidence descriptor gives; and the scope prefix of the snapshot item that holds it is the resource kind the evidence read gives that item,
// `<pack id>.<entity>`, then a colon (portread: ResourceScope is ResourceKind + ":" + the reference). The pack and the mapping name every id; this file names none.

// listEntitiesFromDescriptors derives the list entities of the verified pack from its descriptors and the evidence mapping the Understanding half built (the Reconciler's own, never a
// second mapping). A pack whose descriptors cannot be read, or that repeats an entity, is an error with fixed text and no pack content; a pack with no list entity gives an empty map.
func listEntitiesFromDescriptors(pack []byte, m intakesources.ListMapper) (map[string]intakesources.ListSpec, error) {
	if isNilValue(m) {
		return nil, inputsFail("the evidence mapping is needed to find the list entities", nil)
	}
	var f struct {
		PackID      string `json:"pack_id"`
		Descriptors []struct {
			Entity string `json:"entity"`
			Source string `json:"source"`
		} `json:"evidence_descriptors"`
	}
	if err := json.Unmarshal(pack, &f); err != nil || f.PackID == "" || len(f.Descriptors) == 0 {
		return nil, inputsFail("the pack's evidence descriptors cannot be read", err)
	}
	out := map[string]intakesources.ListSpec{}
	seen := map[string]bool{}
	for _, d := range f.Descriptors {
		if d.Entity == "" || d.Source == "" || seen[d.Entity] {
			return nil, inputsFail("an evidence descriptor has no entity or source, or repeats an entity", nil)
		}
		seen[d.Entity] = true
		// The probe has no payload, so a real list answers ErrIncomplete (the payload's marker is not true) and one the mapping does not list answers ErrNoSuchList. Any other error
		// (ErrListShape, a failure) says nothing about whether the entity is a list: the mapping cannot be read, and the derivation fails with fixed text (INV-16, INV-19, AGENT-13).
		_, err := m.ListRows(d.Source, d.Entity, nil)
		switch {
		case errors.Is(err, mapping.ErrNoSuchList):
			continue // the mapping reads no list under this entity and source
		case err != nil && !errors.Is(err, mapping.ErrIncomplete):
			return nil, inputsFail("the evidence mapping cannot be read for an evidence descriptor", nil)
		}
		out[d.Entity] = intakesources.ListSpec{Source: d.Source, ScopePrefix: f.PackID + "." + d.Entity + ":"}
	}
	return out, nil
}
