package main

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// Settings of the Input Gateway (EDG-01). Red-first stub: behaviour is not built yet.
const (
	EnvEdgeDSNDir         = "NOEMA_EDGE_DSN_DIR"
	EnvEdgeKeyDir         = "NOEMA_EDGE_KEY_DIR"
	EnvEdgeKeyMaxActive   = "NOEMA_EDGE_KEY_MAX_ACTIVE"
	EnvInputHashKeyFile   = "NOEMA_INPUT_HASH_KEY_FILE"
	EnvInputWindow        = "NOEMA_INPUT_DEDUP_WINDOW"
	EnvInputRecordTimeout = "NOEMA_INPUT_RECORD_TIMEOUT"
)

func buildGateway(_ context.Context, _ Env, _ clock.Clock, _ chain.IDSource, _ reconstruct.TenantDBs, _ func(context.Context, inputgateway.Turn) error) (*inputgateway.Gateway, func(), error) {
	return nil, func() {}, errors.New("not built")
}

func withGateway(c Components, _ *inputgateway.Gateway) Components { return c }

func withGatewayReadiness(inner readiness, _ Components) readiness { return inner }

type tenantRecorder struct {
	by map[string]inputgateway.Recorder
}

func (tenantRecorder) Received(context.Context, inputgateway.Identity, string, inputgateway.Received) (inputgateway.Ref, error) {
	return inputgateway.Ref{}, errors.New("not built")
}
func (tenantRecorder) Deduplicated(context.Context, inputgateway.Identity, string, string, inputgateway.Ref) error {
	return errors.New("not built")
}
func (tenantRecorder) Reused(context.Context, inputgateway.Identity, string, string, string, inputgateway.Ref) error {
	return errors.New("not built")
}

type tenantIndex struct{ by map[string]inputgateway.Index }

func (tenantIndex) Find(context.Context, inputgateway.Identity, string) (inputgateway.Prior, bool, error) {
	return inputgateway.Prior{}, false, errors.New("not built")
}

// ErrNoGatewayForTenant marks a tenant with no core-edge login.
var ErrNoGatewayForTenant = errors.New("cmd/core: no gateway login is configured for this tenant")
