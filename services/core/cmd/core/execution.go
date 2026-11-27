package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/signing"
)

// execution.go: the settings and the pools of the Execution side (LANE-5 slice 1, A547 and A554, interim; status/LANE-5.md; REQ-SEC-004, REQ-SEC-005, REQ-EXE-001; INV-12, INV-29,
// AGENT-13, AGENT-16). It is built the way the orchestrator's Recorder and the edge are: file-mounted settings, all four or none, one lazily connecting pool for each tenant, no default
// for any value, and no message that carries a DSN, a login or key material. The database, not this code, binds each login to its tenant (DEBT-04); this code refuses a directory that
// holds a login of another power or another tenant. Nothing is dialled here.

const (
	// EnvExecutionDSNDir names a directory with one file for each tenant, named by the tenant ID, each holding that tenant's execution_rw__<tenant> login.
	EnvExecutionDSNDir = "NOEMA_EXECUTION_DSN_DIR"
	// EnvOutcomeDSNDir is the same for the outcome_rw__<tenant> login.
	EnvOutcomeDSNDir = "NOEMA_OUTCOME_DSN_DIR"
	// EnvExecutionKeyDir names the directory that holds the receipt signer's keys of the identity core-execution (execution/signing.LoadRing).
	EnvExecutionKeyDir = "NOEMA_EXECUTION_KEY_DIR"
	// EnvExecutionKeyMaxActive is how many keys of that identity may be mounted at once: the rotation overlap (REQ-SEC-005). A positive integer with no default.
	EnvExecutionKeyMaxActive = "NOEMA_EXECUTION_KEY_MAX_ACTIVE"

	// executionIdentity is the Record identity of the receipt signer (the key announcement of migration 0004: purpose receipt, identity core-execution).
	executionIdentity = "core-execution"
	// the login names of the two powers: <prefix><tenant>
	executionLoginPrefix = "execution_rw__"
	outcomeLoginPrefix   = "outcome_rw__"
)

// executionParts is what slice 1 builds: the receipt signer's ring and one pool of each kind for each tenant.
type executionParts struct {
	Receipts       *signing.Ring
	ExecutionPools map[string]*pgxpool.Pool
	OutcomePools   map[string]*pgxpool.Pool
}

// buildExecution builds the parts from the four settings and returns a function that closes every pool (safe to call more than once). With none of the settings it returns no parts,
// a function that does nothing and no error: the Execution side is not composed and /readyz says so. With some but not all, or with any that cannot be applied safely, it returns an
// error that names the setting or the tenant, with no parts and no pool left open.
func buildExecution(ctx context.Context, env Env) (*executionParts, func(), error) {
	noop := func() {}
	settings := []string{EnvExecutionDSNDir, EnvOutcomeDSNDir, EnvExecutionKeyDir, EnvExecutionKeyMaxActive}
	given := 0
	for _, s := range settings {
		if env.Getenv(s) != "" {
			given++
		}
	}
	if given == 0 {
		return nil, noop, nil
	}
	for _, s := range settings {
		if env.Getenv(s) == "" {
			return nil, nil, fmt.Errorf("cmd/core: the Execution side needs %s as well: all four settings or none", s)
		}
	}
	maxActive, err := parseKeyOverlap(EnvExecutionKeyMaxActive, env.Getenv(EnvExecutionKeyMaxActive))
	if err != nil {
		return nil, nil, err
	}
	ring, err := signing.LoadRing(executionIdentity, env.Getenv(EnvExecutionKeyDir), signing.Options{MaxActive: maxActive}) // its errors never repeat a key's contents
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: %s: the signing keys cannot be loaded: %w", EnvExecutionKeyDir, err)
	}
	execPools, closeExec, err := loginPools(ctx, env, EnvExecutionDSNDir)
	if err != nil {
		return nil, nil, err
	}
	outPools, closeOut, err := loginPools(ctx, env, EnvOutcomeDSNDir)
	if err != nil {
		closeExec()
		return nil, nil, err
	}
	var once sync.Once
	closeAll := func() { once.Do(func() { closeExec(); closeOut() }) }
	for tenant := range execPools {
		if _, ok := outPools[tenant]; !ok {
			closeAll()
			return nil, nil, fmt.Errorf("cmd/core: %s: tenant %s has an execution login but no outcome login", EnvOutcomeDSNDir, tenant)
		}
	}
	for tenant := range outPools {
		if _, ok := execPools[tenant]; !ok {
			closeAll()
			return nil, nil, fmt.Errorf("cmd/core: %s: tenant %s has an outcome login but no execution login", EnvExecutionDSNDir, tenant)
		}
	}
	for _, c := range []struct {
		pools   map[string]*pgxpool.Pool
		prefix  string
		setting string
	}{{execPools, executionLoginPrefix, EnvExecutionDSNDir}, {outPools, outcomeLoginPrefix, EnvOutcomeDSNDir}} {
		for tenant, pool := range c.pools {
			// the login of this power, for this tenant: a login of another power would give this side another power's grants of the database; the message names the setting and the tenant
			if pool.Config().ConnConfig.User != c.prefix+tenant {
				closeAll()
				return nil, nil, fmt.Errorf("cmd/core: %s: the login of tenant %s is not the login of this power for that tenant", c.setting, tenant)
			}
		}
	}
	return &executionParts{Receipts: ring, ExecutionPools: execPools, OutcomePools: outPools}, closeAll, nil
}
