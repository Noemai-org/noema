package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// wire_trust.go builds the orchestrator's Trust and Bindings (STM-01 follow-up; REQ-TRU-009, REQ-RCV-003; INV-03, INV-12, INV-25, INV-29): the Trust adapter of noema#710 over the
// Postgres readers of trustpg, each reading as the tenant's own trust_rw login (the files of EnvTrustDSNDir, the same directory the payload reader uses), and the dispatch count
// over the Record reader. The Decision Grant keys come only from the directory EnvGrantKeyDir names, through the key loader the caller passes (keyfiles.Load once noema#708 is on
// main): never from the environment, never with a default, never logged. Every item is required: a missing one is an error that names it, and nothing is left open. Nothing is
// dialled here.
//
// The intake service itself is not built here: the ports it needs besides the dispatch count have no adapter on main. The caller passes the Evaluator; nil is an error.

const (
	// EnvTrustDBTimeout is how long one read of the Trust readers may take (a Go duration). Required, no default (AGENT-16).
	EnvTrustDBTimeout = "NOEMA_TRUST_DB_TIMEOUT"
	// EnvGrantKeyDir names the directory of the Decision Grant HMAC key files (INV-29). The loader reads it; this code never reads a key.
	EnvGrantKeyDir = "NOEMA_GRANT_KEY_DIR"
	// EnvGrantKeyMaxActive is how many grant keys may be mounted at once: the rotation overlap (REQ-SEC-005). A positive integer with no default.
	EnvGrantKeyMaxActive = "NOEMA_GRANT_KEY_MAX_ACTIVE"
)

// KeyLoader loads the grant key ring from a directory (keyfiles.Load of SEC-09). Its errors never hold key material.
type KeyLoader func(dir string, maxActive int) (*grants.Keys, error)

// TrustParts is what buildTrust makes. Dispatches is for the intake's Ports; Keys is for the Decision Record writer's issuer and signer. It is all or nothing: on an error every
// field is zero.
type TrustParts struct {
	Trust      orchestrator.Trust
	Bindings   orchestrator.Bindings
	Dispatches intake.Dispatches
	Keys       *grants.Keys
}

// buildTrust builds the parts and returns them with a function that closes every pool opened (safe to call more than once, and returned with an error too).
func buildTrust(ctx context.Context, env Env, records reconstruct.TenantDBs, evaluator trustport.Evaluator, loadKeys KeyLoader) (TrustParts, func(), error) {
	noop := func() {}
	switch {
	case evaluator == nil:
		return TrustParts{}, noop, errors.New("cmd/core: the Trust adapter needs the intake (the Evaluator): it is not configured")
	case records == nil:
		return TrustParts{}, noop, errors.New("cmd/core: the Trust adapter counts dispatches from the Record and needs the Record reader (" + EnvRecordReadDSNDir + ")")
	case loadKeys == nil:
		return TrustParts{}, noop, errors.New("cmd/core: the Trust adapter needs the grant key loader: it is not configured")
	case env.Getenv(EnvTrustDSNDir) == "":
		return TrustParts{}, noop, errors.New("cmd/core: the Trust adapter needs " + EnvTrustDSNDir)
	case env.Getenv(EnvGrantKeyDir) == "":
		return TrustParts{}, noop, errors.New("cmd/core: the Trust adapter needs " + EnvGrantKeyDir)
	}
	timeout, err := time.ParseDuration(env.Getenv(EnvTrustDBTimeout))
	if err != nil || timeout <= 0 {
		return TrustParts{}, noop, fmt.Errorf("cmd/core: %s must be a positive duration", EnvTrustDBTimeout)
	}
	maxActive, err := strconv.Atoi(env.Getenv(EnvGrantKeyMaxActive))
	if err != nil || maxActive < 1 {
		return TrustParts{}, noop, fmt.Errorf("cmd/core: %s must be a positive integer", EnvGrantKeyMaxActive)
	}
	keys, err := loadKeys(env.Getenv(EnvGrantKeyDir), maxActive) // its errors never repeat a key's contents
	if err != nil {
		return TrustParts{}, noop, fmt.Errorf("cmd/core: %s: the grant keys cannot be loaded: %w", EnvGrantKeyDir, err)
	}
	if keys == nil {
		return TrustParts{}, noop, fmt.Errorf("cmd/core: %s: the loader returned no keys", EnvGrantKeyDir)
	}
	loginPool, closer, err := loginPools(ctx, env, EnvTrustDSNDir)
	if err != nil {
		return TrustParts{}, noop, err
	}
	pools, err := trustpg.NewPools(loginPool)
	if err != nil {
		closer()
		return TrustParts{}, noop, fmt.Errorf("cmd/core: %s: %w", EnvTrustDSNDir, err)
	}
	intents, err := trustpg.NewIntents(pools, timeout)
	if err != nil {
		closer()
		return TrustParts{}, noop, err
	}
	recs, err := trustpg.NewRecords(pools, timeout)
	if err != nil {
		closer()
		return TrustParts{}, noop, err
	}
	keyDispatches, err := trustpg.NewKeyDispatches(recordDBs{records})
	if err != nil {
		closer()
		return TrustParts{}, noop, err
	}
	dispatches, err := trustport.NewDispatches(keyDispatches, orchestrator.PERDMaxDispatchesPerKey)
	if err != nil {
		closer()
		return TrustParts{}, noop, err
	}
	port := trustport.Port{Evaluator: evaluator, Intents: intents, Records: recs}
	return TrustParts{Trust: port, Bindings: port, Dispatches: dispatches, Keys: keys}, closer, nil
}

// recordDBs adapts the Record reader's pools to trustpg.RecordDBs: the same pools, so a count is read as the session's tenant.
type recordDBs struct{ r reconstruct.TenantDBs }

func (d recordDBs) For(tenantID string) (trustpg.TxBeginner, error) {
	b, err := d.r.For(tenantID)
	if err != nil {
		return nil, err
	}
	return b, nil
}
