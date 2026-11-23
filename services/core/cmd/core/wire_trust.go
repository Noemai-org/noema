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
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/grants/keyfiles"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// wire_trust.go builds the orchestrator's Trust and Bindings (STM-01 follow-up; REQ-TRU-009, REQ-RCV-003; INV-03, INV-12, INV-25, INV-29): the Trust adapter of noema#710 over the
// Postgres readers of trustpg, each reading as the tenant's own trust_rw login (the files of EnvTrustDSNDir, the same directory the payload reader uses), and the dispatch count
// over the Record reader. The Decision Grant keys come only from the directory EnvGrantKeyDir names, through the key loader the caller passes (keyfiles.Load once noema#708 is on
// main): never from the environment, never with a default, never logged. Every item is required: a missing one is an error that names it, and nothing is left open. Nothing is
// dialled here.
//
// The intake service is built here with all 15 ports (REQ-TRU-002): Dispatches from the Record reader, the other 14 from TrustAdapters. Trust evaluates through that service.

const (
	// EnvTrustDBTimeout is how long one read of the Trust readers may take, in whole milliseconds (A330: the _MS suffix). Required, no default (AGENT-16).
	EnvTrustDBTimeout = "NOEMA_TRUST_DB_TIMEOUT_MS"
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
	Service    *intake.Service
}

// TrustAdapters are the intake ports buildTrust does not build itself (every port of intake.Ports but Dispatches, which it builds from the Record reader), the service clock
// and the pack version. Each is required: checkTrustAdapters names the first one that is missing, and a typed nil inside an interface counts as missing (INV-03, AGENT-16: no
// quiet nil, no default). The final main.go builds each from its adapter package and passes it here.
type TrustAdapters struct {
	Intents       intake.Intents
	Writes        intake.Writes
	Capabilities  intake.Capabilities
	Scope         intake.Scope
	Authority     intake.Authority
	Candidates    intake.Candidates
	Ownership     intake.Ownership
	Evidence      intake.Evidence
	Reconciler    intake.Reconciler
	Policies      intake.Policies
	Clocks        intake.Clocks
	Confirmations intake.Confirmations
	Planner       intake.Planner
	Recorder      intake.Recorder
	Clock         clock.Clock
	PackVersion   string
}

// loadGrantKeys is the production KeyLoader: keyfiles.Load (SEC-09, INV-29). The final main.go passes it.
func loadGrantKeys(dir string, maxActive int) (*grants.Keys, error) {
	return keyfiles.Load(dir, keyfiles.Options{MaxActive: maxActive})
}

// checkTrustAdapters returns an error that names the first missing adapter, in the order of the struct's fields (unsetAdapters has the rule).
func checkTrustAdapters(a TrustAdapters) error {
	if names := unsetAdapters(a); len(names) > 0 {
		return errors.New("cmd/core: the Trust service needs the " + names[0] + " adapter: it is not configured")
	}
	return nil
}

// buildTrust builds the parts and returns them with a function that closes every pool opened (safe to call more than once, and returned with an error too).
func buildTrust(ctx context.Context, env Env, records reconstruct.TenantDBs, adapters TrustAdapters, loadKeys KeyLoader) (TrustParts, func(), error) {
	noop := func() {}
	if err := checkTrustAdapters(adapters); err != nil {
		return TrustParts{}, noop, err
	}
	timeout, keys, err := trustSettings(env, records, loadKeys)
	if err != nil {
		return TrustParts{}, noop, err
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
	svc := &intake.Service{
		Ports: intake.Ports{
			Intents: adapters.Intents, Writes: adapters.Writes, Capabilities: adapters.Capabilities, Scope: adapters.Scope, Authority: adapters.Authority,
			Candidates: adapters.Candidates, Ownership: adapters.Ownership, Evidence: adapters.Evidence, Reconciler: adapters.Reconciler, Policies: adapters.Policies,
			Clocks: adapters.Clocks, Confirmations: adapters.Confirmations, Planner: adapters.Planner, Recorder: adapters.Recorder, Dispatches: dispatches,
		},
		Clock:       adapters.Clock,
		PackVersion: adapters.PackVersion,
	}
	port := trustport.Port{Evaluator: svc, Intents: intents, Records: recs}
	return TrustParts{Trust: port, Bindings: port, Dispatches: dispatches, Keys: keys, Service: svc}, closer, nil
}

// trustSettings checks every setting Trust needs and loads the grant keys: the Record reader, the key loader, the two directories and the two numbers, each required with no default
// (AGENT-16). A missing or invalid one is an error that names it, and nothing is dialled. buildTrust uses it, and so does the composition while the adapters are not yet built, so
// that a bad setting stops the process whether or not Trust can be plugged yet.
func trustSettings(env Env, records reconstruct.TenantDBs, loadKeys KeyLoader) (time.Duration, *grants.Keys, error) {
	switch {
	case records == nil:
		return 0, nil, errors.New("cmd/core: the Trust adapter counts dispatches from the Record and needs the Record reader (" + EnvRecordReadDSNDir + ")")
	case loadKeys == nil:
		return 0, nil, errors.New("cmd/core: the Trust adapter needs the grant key loader: it is not configured")
	case env.Getenv(EnvTrustDSNDir) == "":
		return 0, nil, errors.New("cmd/core: the Trust adapter needs " + EnvTrustDSNDir)
	case env.Getenv(EnvGrantKeyDir) == "":
		return 0, nil, errors.New("cmd/core: the Trust adapter needs " + EnvGrantKeyDir)
	}
	ms, err := strconv.Atoi(env.Getenv(EnvTrustDBTimeout))
	if err != nil || ms < 1 {
		return 0, nil, fmt.Errorf("cmd/core: %s must be a positive whole number of milliseconds", EnvTrustDBTimeout)
	}
	maxActive, err := strconv.Atoi(env.Getenv(EnvGrantKeyMaxActive))
	if err != nil || maxActive < 1 {
		return 0, nil, fmt.Errorf("cmd/core: %s must be a positive integer", EnvGrantKeyMaxActive)
	}
	keys, err := loadKeys(env.Getenv(EnvGrantKeyDir), maxActive) // its errors never repeat a key's contents
	if err != nil {
		return 0, nil, fmt.Errorf("cmd/core: %s: the grant keys cannot be loaded: %w", EnvGrantKeyDir, err)
	}
	if keys == nil {
		return 0, nil, fmt.Errorf("cmd/core: %s: the loader returned no keys", EnvGrantKeyDir)
	}
	return time.Duration(ms) * time.Millisecond, keys, nil
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
