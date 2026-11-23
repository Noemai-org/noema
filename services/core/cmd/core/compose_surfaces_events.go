package main

import (
	"github.com/Noemai-org/noema/services/core/edge/events"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// buildEventsSurface builds the events route (STM-01 surfaces; REQ-API-001; INV-12, AGENT-16; A469, A486). Not built yet.
func buildEventsSurface(Env, clock.Clock, reconstruct.TenantDBs) (*events.Handler, []string, error) {
	return nil, nil, nil
}
