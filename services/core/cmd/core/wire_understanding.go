package main

import (
	"context"
	"errors"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	interpreterport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/interpreter"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

// Stub of the red commit: the names the tests use, and a builder that builds nothing.

const (
	EnvPackDir          = "NOEMA_PACK_DIR"
	EnvPackManifest     = "NOEMA_PACK_MANIFEST"
	EnvPackMaxBytes     = "NOEMA_PACK_MAX_BYTES"
	EnvPackMaxDepth     = "NOEMA_PACK_MAX_DEPTH"
	EnvStoreTimeoutMS   = "NOEMA_STORE_TIMEOUT_MS"
	EnvCompileTimeoutMS = "NOEMA_COMPILE_TIMEOUT_MS"
	EnvSealTimeoutMS    = "NOEMA_SEAL_TIMEOUT_MS"
)

var (
	ErrNoInterpreterForTenant = errors.New("cmd/core: no Interpreter is configured for this tenant")
	ErrSessionUnavailable     = errors.New("cmd/core: the verified session is not available to the Interpreter")
)

// UnderstandingParts is what buildUnderstanding makes.
type UnderstandingParts struct {
	Interpreter orchestrator.Interpreter
	RuleSet     *reconcile.RuleSet
	Mapping     *mapping.Mapping
	PackRef     string
	SealTimeout time.Duration
}

func buildUnderstanding(env Env, clk clock.Clock, recorder *orchestratorRecorder) (UnderstandingParts, error) {
	return UnderstandingParts{}, errors.New("cmd/core: not implemented")
}

// edgeSessions is the stub of the session resolver: it resolves nothing.
type edgeSessions struct{}

func (edgeSessions) Resolve(context.Context, orchestrator.Session) (interpreterport.Resolved, error) {
	return interpreterport.Resolved{}, nil
}
