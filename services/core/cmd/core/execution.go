package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/signing"
)

// execution.go: the settings and the pools of the Execution side (LANE-5, A547 and A554, interim; status/LANE-5.md).

const (
	// EnvExecutionDSNDir names a directory with one file for each tenant, named by the tenant ID, each holding that tenant's execution_rw__<tenant> login.
	EnvExecutionDSNDir = "NOEMA_EXECUTION_DSN_DIR"
	// EnvOutcomeDSNDir is the same for the outcome_rw__<tenant> login.
	EnvOutcomeDSNDir = "NOEMA_OUTCOME_DSN_DIR"
	// EnvExecutionKeyDir names the directory that holds the receipt signer's keys of the identity core-execution.
	EnvExecutionKeyDir = "NOEMA_EXECUTION_KEY_DIR"
	// EnvExecutionKeyMaxActive is how many keys of that identity may be mounted at once: the rotation overlap. A positive integer with no default.
	EnvExecutionKeyMaxActive = "NOEMA_EXECUTION_KEY_MAX_ACTIVE"
)

// executionParts is what slice 1 builds: the receipt signer's ring and one pool of each kind for each tenant.
type executionParts struct {
	Receipts       *signing.Ring
	ExecutionPools map[string]*pgxpool.Pool
	OutcomePools   map[string]*pgxpool.Pool
}

// buildExecution is the red-commit stub (LANE-5 slice 1): it builds nothing.
func buildExecution(ctx context.Context, env Env) (*executionParts, func(), error) {
	return nil, func() {}, errors.New("cmd/core: the Execution side is not built")
}
