package main

import (
	"errors"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/platform/trustlimits"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Stub of the red commit: it compiles and builds nothing.

var errClocksUnbuilt = errors.New("cmd/core: clocks: not built")

func trustLimitsOf(packload.Pack) (trustlimits.Limits, contracts.TrustLimitsV2, error) {
	return trustlimits.Limits{}, contracts.TrustLimitsV2{}, errClocksUnbuilt
}

type clocksParts struct {
	Pack  []byte
	Limit trustlimits.Limits
	Doc   contracts.TrustLimitsV2
	Lists intakesources.EvidenceLists
}

func newClocksAdapter(clocksParts) (intake.Clocks, error) { return nil, nil }

func buildClocksInput(trustInputsIn) (intake.Clocks, error) { return nil, nil }

func checkPackLimits(Env) error { return nil }
