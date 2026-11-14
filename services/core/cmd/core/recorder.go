package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/signing"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/admission"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// recorder.go builds the orchestrator's Recorder (STM-01, the G1 card; the Founder's order of 14 Nov; rulings A202 for the pools and A204 for the write path, both relayed,
// not yet committed). It is built the way the Record reader is (noema#535): file-mounted settings, one lazily connecting pool per tenant, no default for any value (AGENT-16),
// and no message that carries a DSN, a login or key material (AGENT-13). The database, not this code, binds each login to its tenant (DEBT-04, migration 0025).

// The settings of the orchestrator's Recorder. All three or none: a partial configuration is an error that names the missing one.
const (
	// EnvOrchestratorDSNDir names a directory with one file per tenant, named by the tenant ID, each holding that tenant's core-workflows login.
	EnvOrchestratorDSNDir = "NOEMA_ORCHESTRATOR_DSN_DIR"
	// EnvOrchestratorKeyDir names the directory that holds the signing keys of the identity core-workflows (execution/signing.LoadRing).
	EnvOrchestratorKeyDir = "NOEMA_ORCHESTRATOR_KEY_DIR"
	// EnvOrchestratorKeyMaxActive is how many keys of that identity may be mounted at once: the rotation overlap (REQ-SEC-005). A positive integer with no default.
	EnvOrchestratorKeyMaxActive = "NOEMA_ORCHESTRATOR_KEY_MAX_ACTIVE"

	// orchestratorIdentity is the Record identity of the orchestrator (migration 0004); investigations.Store appends as it.
	orchestratorIdentity = "core-workflows"
)

// orchestratorRecorder is an orchestrator.Recorder over one investigation store per tenant. A tenant with no store is an error and never another tenant's store (INV-12).
type orchestratorRecorder struct {
	stores map[string]*investigations.Store
}

var _ orchestrator.Recorder = (*orchestratorRecorder)(nil)

// ErrNoStoreForTenant marks a tenant that has no login file: nothing is asked of any database.
var ErrNoStoreForTenant = errors.New("cmd/core: no orchestrator login is configured for this tenant")

// For returns the tenant's own store, or an error. It never falls back to another tenant's store or to a default.
func (r *orchestratorRecorder) For(tenantID string) (*investigations.Store, error) {
	s, ok := r.stores[tenantID]
	if !ok || !tenantFileRe.MatchString(tenantID) {
		return nil, ErrNoStoreForTenant
	}
	return s, nil
}

// AppendChange appends one INVESTIGATION_STATE_CHANGED event and moves the investigation, as the session's tenant's store.
func (r *orchestratorRecorder) AppendChange(ctx context.Context, s orchestrator.Session, c orchestrator.Change) error {
	st, err := r.For(s.TenantID)
	if err != nil {
		return err
	}
	return st.AppendChange(ctx, s, c)
}

// lookuper turns the Record reader of noema#535 into the store's Lookuper: the event a turn names, read as the session's tenant.
type lookuper struct{ r *admission.Reader }

func (l lookuper) Lookup(ctx context.Context, tenantID, eventID string) (investigations.Recorded, error) {
	f, err := l.r.Lookup(ctx, tenantID, eventID)
	if err != nil {
		return investigations.Recorded{}, err
	}
	return investigations.Recorded{EventType: f.EventType, TenantID: f.TenantID, PrincipalID: f.PrincipalID, CorrelationID: f.CorrelationID}, nil
}

// buildRecorder builds the Recorder from the three settings and the Record reader, and returns a function that closes every pool (safe to call more than once). With none of
// the settings it returns no Recorder, a function that does nothing and no error. With some but not all, or with any that cannot be applied safely, it returns an error that
// names the setting or the tenant, with no Recorder and no pool left open. Nothing is dialled here.
func buildRecorder(ctx context.Context, env Env, clk clock.Clock, ids chain.IDSource, records reconstruct.TenantDBs) (*orchestratorRecorder, func(), error) {
	noop := func() {}
	settings := []string{EnvOrchestratorDSNDir, EnvOrchestratorKeyDir, EnvOrchestratorKeyMaxActive}
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
			return nil, nil, fmt.Errorf("cmd/core: the orchestrator's Recorder needs %s as well: all three settings or none", s)
		}
	}
	if records == nil {
		return nil, nil, errors.New("cmd/core: the orchestrator's Recorder confirms an input against the Record and needs the Record reader (" + EnvRecordReadDSNDir + ")")
	}
	maxActive, err := strconv.Atoi(env.Getenv(EnvOrchestratorKeyMaxActive))
	if err != nil || maxActive < 1 {
		return nil, nil, fmt.Errorf("cmd/core: %s must be a positive integer", EnvOrchestratorKeyMaxActive)
	}
	ring, err := signing.LoadRing(orchestratorIdentity, env.Getenv(EnvOrchestratorKeyDir), signing.Options{MaxActive: maxActive}) // its errors never repeat a key's contents
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: %s: the signing keys cannot be loaded: %w", EnvOrchestratorKeyDir, err)
	}
	pools, closer, err := loginPools(ctx, env, EnvOrchestratorDSNDir)
	if err != nil {
		return nil, nil, err
	}
	inputs := investigations.ConfirmWith(lookuper{&admission.Reader{DBs: records}})
	stores := make(map[string]*investigations.Store, len(pools))
	for tenant, pool := range pools {
		stores[tenant] = newStore(pool, clk, ids, ring, inputs)
	}
	return &orchestratorRecorder{stores: stores}, closer, nil
}

func newStore(pool *pgxpool.Pool, clk clock.Clock, ids chain.IDSource, ring *signing.Ring, inputs investigations.Inputs) *investigations.Store {
	return &investigations.Store{Pool: pool, Appender: &chain.Appender{Clock: clk, IDs: ids, Signers: map[string]chain.Signer{orchestratorIdentity: ring}}, Inputs: inputs}
}
