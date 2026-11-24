package main

import (
	"errors"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
)

var errListStub = errors.New("cmd/core: not built")

// listEntitiesFromDescriptors is the red stub: it gives no list entity.
func listEntitiesFromDescriptors(pack []byte, m intakesources.ListMapper) (map[string]intakesources.ListSpec, error) {
	return nil, errListStub
}
