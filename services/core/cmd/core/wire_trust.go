package main

// wire_trust.go is the red stub of buildTrust (STM-01 follow-up): nothing is built yet.

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const (
	EnvTrustDBTimeout    = "NOEMA_TRUST_DB_TIMEOUT"
	EnvGrantKeyDir       = "NOEMA_GRANT_KEY_DIR"
	EnvGrantKeyMaxActive = "NOEMA_GRANT_KEY_MAX_ACTIVE"
)

type KeyLoader func(dir string, maxActive int) (*grants.Keys, error)

type TrustParts struct {
	Trust      orchestrator.Trust
	Bindings   orchestrator.Bindings
	Dispatches intake.Dispatches
	Keys       *grants.Keys
}

func buildTrust(context.Context, Env, reconstruct.TenantDBs, trustport.Evaluator, KeyLoader) (TrustParts, func(), error) {
	return TrustParts{}, func() {}, errors.New("not built")
}
