package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/execution/signing"
	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// wire_gateway.go builds the Input Gateway (EDG-01, UND-02; REQ-API-001, REQ-INP-002; INV-12, INV-02, AGENT-10, AGENT-16, AGENT-13). It holds no product logic: it reads the
// file-mounted settings, makes one Recorder and one Index PER TENANT and hands them to inputgateway.New. The route itself is mounted by apiRoutes (compose.go) once
// Components.Gateway is set; withGateway sets it, and withGatewayReadiness makes /readyz name the gateway while it is not.

// The settings of the Input Gateway. All of them or none: a partial configuration is an error that names the missing one. The body bound is the edge's own (edge.EnvMaxBody),
// so the edge and the gateway cannot disagree. None has a default.
const (
	// EnvEdgeDSNDir names a directory with one file per tenant, named by the tenant ID, each holding that tenant's core-edge emitter login (migration 0011).
	EnvEdgeDSNDir = "NOEMA_EDGE_DSN_DIR"
	// EnvEdgeKeyDir names the directory that holds the signing keys of the identity core-edge (execution/signing.LoadRing).
	EnvEdgeKeyDir = "NOEMA_EDGE_KEY_DIR"
	// EnvEdgeKeyMaxActive is how many core-edge keys may be mounted at once: the rotation overlap (REQ-SEC-005). A positive integer.
	EnvEdgeKeyMaxActive = "NOEMA_EDGE_KEY_MAX_ACTIVE"
	// EnvInputHashKeyFile names the file that holds the core's secret for the hash the Record keeps of an input (REQ-SEC-003): at least 32 bytes, a trailing newline is dropped.
	EnvInputHashKeyFile = "NOEMA_INPUT_HASH_KEY_FILE"
	// EnvInputWindow is the window in which a request ID is a repeat (REQ-INP-003), a Go duration.
	EnvInputWindow = "NOEMA_INPUT_DEDUP_WINDOW"
	// EnvInputRecordTimeout is the deadline of each Record append and read of the gateway, a Go duration.
	EnvInputRecordTimeout = "NOEMA_INPUT_RECORD_TIMEOUT"

	edgeIdentity = "core-edge"
)

// ErrNoGatewayForTenant marks a tenant that has no core-edge login: nothing is asked of any database, and no other tenant's login is used (INV-12).
var ErrNoGatewayForTenant = errors.New("cmd/core: no gateway login is configured for this tenant")

// tenantRecorder is an inputgateway.Recorder over one recorder per tenant. The tenant is the identity's, which the edge took from the verified session.
type tenantRecorder struct {
	by map[string]inputgateway.Recorder
}

var _ inputgateway.Recorder = tenantRecorder{}

func (r tenantRecorder) For(tenantID string) (inputgateway.Recorder, error) {
	rec, ok := r.by[tenantID]
	if !ok || !tenantFileRe.MatchString(tenantID) {
		return nil, ErrNoGatewayForTenant
	}
	return rec, nil
}

func (r tenantRecorder) Received(ctx context.Context, id inputgateway.Identity, corr string, in inputgateway.Received) (inputgateway.Ref, error) {
	rec, err := r.For(id.TenantID)
	if err != nil {
		return inputgateway.Ref{}, err
	}
	return rec.Received(ctx, id, corr, in)
}

func (r tenantRecorder) Deduplicated(ctx context.Context, id inputgateway.Identity, corr, requestID string, original inputgateway.Ref) error {
	rec, err := r.For(id.TenantID)
	if err != nil {
		return err
	}
	return rec.Deduplicated(ctx, id, corr, requestID, original)
}

func (r tenantRecorder) Reused(ctx context.Context, id inputgateway.Identity, corr, requestID, inputHash string, original inputgateway.Ref) error {
	rec, err := r.For(id.TenantID)
	if err != nil {
		return err
	}
	return rec.Reused(ctx, id, corr, requestID, inputHash, original)
}

// tenantIndex is an inputgateway.Index over one index per tenant, with the same rule.
type tenantIndex struct{ by map[string]inputgateway.Index }

var _ inputgateway.Index = tenantIndex{}

func (x tenantIndex) Find(ctx context.Context, id inputgateway.Identity, requestID string) (inputgateway.Prior, bool, error) {
	idx, ok := x.by[id.TenantID]
	if !ok || !tenantFileRe.MatchString(id.TenantID) {
		return inputgateway.Prior{}, false, ErrNoGatewayForTenant
	}
	return idx.Find(ctx, id, requestID)
}

// buildGateway builds the Input Gateway and returns it with a function that closes every pool it opened (safe to call more than once). With none of the settings it returns no
// gateway, a function that does nothing and no error, and /readyz says the gateway is missing. With some but not all, or any that cannot be applied, it returns an error naming
// the setting or the tenant, no gateway and no pool left open. Nothing is dialled here. records must be the Record reader of buildRecordReader (its per-tenant record_read pools
// serve the index, so a tenant's lookup is made by that tenant's own login); start is the orchestrator seam and must be idempotent per Turn.Input (inputgateway README).
func buildGateway(ctx context.Context, env Env, clk clock.Clock, ids chain.IDSource, records reconstruct.TenantDBs, start func(context.Context, inputgateway.Turn) error) (*inputgateway.Gateway, func(), error) {
	noop := func() {}
	settings := []string{EnvEdgeDSNDir, EnvEdgeKeyDir, EnvEdgeKeyMaxActive, EnvInputHashKeyFile, EnvInputWindow, EnvInputRecordTimeout, edge.EnvMaxBody}
	if env.Getenv(EnvEdgeDSNDir) == "" && env.Getenv(EnvEdgeKeyDir) == "" && env.Getenv(EnvEdgeKeyMaxActive) == "" && env.Getenv(EnvInputHashKeyFile) == "" &&
		env.Getenv(EnvInputWindow) == "" && env.Getenv(EnvInputRecordTimeout) == "" {
		return nil, noop, nil // the body bound alone is the edge's setting: it does not configure a gateway
	}
	for _, s := range settings {
		if env.Getenv(s) == "" {
			return nil, nil, fmt.Errorf("cmd/core: the Input Gateway needs %s as well: all its settings or none", s)
		}
	}
	maxActive, err := strconv.Atoi(env.Getenv(EnvEdgeKeyMaxActive))
	if err != nil || maxActive < 1 {
		return nil, nil, fmt.Errorf("cmd/core: %s must be a positive integer", EnvEdgeKeyMaxActive)
	}
	maxBody, err := strconv.ParseInt(env.Getenv(edge.EnvMaxBody), 10, 64)
	if err != nil || maxBody < 1 {
		return nil, nil, fmt.Errorf("cmd/core: %s must be a positive integer", edge.EnvMaxBody)
	}
	window, err := positiveDuration(env, EnvInputWindow)
	if err != nil {
		return nil, nil, err
	}
	timeout, err := positiveDuration(env, EnvInputRecordTimeout)
	if err != nil {
		return nil, nil, err
	}
	readers, ok := records.(*recordPools)
	if !ok || readers == nil {
		return nil, nil, errors.New("cmd/core: the Input Gateway looks up an earlier input as the tenant and needs the Record reader (" + EnvRecordReadDSNDir + ")")
	}
	if start == nil {
		return nil, nil, errors.New("cmd/core: the Input Gateway needs the orchestrator seam")
	}
	if env.ReadFile == nil {
		return nil, nil, fmt.Errorf("cmd/core: %s cannot be read: the process has no file access wired", EnvInputHashKeyFile)
	}
	raw, err := env.ReadFile(env.Getenv(EnvInputHashKeyFile))
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: %s: the file cannot be read", EnvInputHashKeyFile)
	}
	hashKey := bytes.TrimRight(raw, "\r\n")
	if len(hashKey) < 32 {
		return nil, nil, fmt.Errorf("cmd/core: %s: the key is shorter than 32 bytes", EnvInputHashKeyFile) // never repeats the key
	}
	ring, err := signing.LoadRing(edgeIdentity, env.Getenv(EnvEdgeKeyDir), signing.Options{MaxActive: maxActive}) // its errors never repeat a key's contents
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: %s: the signing keys cannot be loaded: %w", EnvEdgeKeyDir, err)
	}
	pools, closer, err := loginPools(ctx, env, EnvEdgeDSNDir)
	if err != nil {
		return nil, nil, err
	}
	recorders := make(map[string]inputgateway.Recorder, len(pools))
	indexes := make(map[string]inputgateway.Index, len(pools))
	appender := &chain.Appender{Clock: clk, IDs: ids, Signers: map[string]chain.Signer{edgeIdentity: ring}}
	for tenant, pool := range pools {
		read, ok := readers.pools[tenant]
		if !ok {
			closer()
			return nil, nil, fmt.Errorf("cmd/core: %s: tenant %s has a gateway login and no Record reader login", EnvEdgeDSNDir, tenant)
		}
		recorders[tenant] = &inputgateway.RecordRecorder{Pool: pool, Appender: appender, Timeout: timeout}
		indexes[tenant] = &inputgateway.RecordIndex{Pool: read, Clock: clk, Window: window, Timeout: timeout}
	}
	g, err := inputgateway.New(inputgateway.Config{Recorder: tenantRecorder{by: recorders}, Index: tenantIndex{by: indexes}, Start: start, IDs: ids, MaxBody: maxBody, HashKey: hashKey})
	if err != nil {
		closer()
		return nil, nil, fmt.Errorf("cmd/core: the Input Gateway: %w", err)
	}
	return g, closer, nil
}

func positiveDuration(env Env, setting string) (time.Duration, error) {
	d, err := time.ParseDuration(env.Getenv(setting))
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("cmd/core: %s must be a positive duration", setting)
	}
	return d, nil
}

// withGateway returns c with the gateway set, which is what mounts POST /v1/conversations in apiRoutes (behind the verified session and the customer role gate; identity
// from the session only, gatewayIdentity). A nil gateway leaves c as it is: no route.
func withGateway(c Components, g *inputgateway.Gateway) Components {
	if g != nil {
		c.Gateway = g
	}
	return c
}

// withGatewayReadiness is the /readyz check of inner plus the gateway: fail closed, and it names the gateway when it is not wired. inner's own reason comes first.
func withGatewayReadiness(inner readiness, c Components) readiness {
	return func() (bool, string) {
		if ok, reason := inner(); !ok {
			return false, reason
		}
		if c.Gateway == nil {
			return false, "gateway is not configured"
		}
		return true, ""
	}
}
