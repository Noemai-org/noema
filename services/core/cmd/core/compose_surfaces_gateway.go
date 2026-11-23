package main

import (
	"context"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// buildGatewaySurface builds the Input Gateway and its investigation workers over a runner (STM-01 surfaces; REQ-INP-002, REQ-INP-003, REQ-API-001; INV-12, AGENT-16; A469, A486).
// Not built yet.
func buildGatewaySurface(context.Context, Env, clock.Clock, chain.IDSource, reconstruct.TenantDBs, *orchestratorRecorder, turnRunner, func(string, error)) (*gatewayService, []string, error) {
	return nil, nil, nil
}
