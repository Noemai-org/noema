package main

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"time"
)

const (
	EnvInvestigationWorkers        = "NOEMA_INVESTIGATION_WORKERS"
	EnvInvestigationQueue          = "NOEMA_INVESTIGATION_QUEUE"
	EnvInvestigationRunTimeoutMS   = "NOEMA_INVESTIGATION_RUN_TIMEOUT_MS"
	EnvInvestigationStartTimeoutMS = "NOEMA_INVESTIGATION_START_TIMEOUT_MS"
)

var (
	ErrStartBusy   = errors.New("cmd/core: the investigation queue is full")
	ErrStartClosed = errors.New("cmd/core: the investigation workers are shut down")
)

type turnRunner interface {
	RunWrite(ctx context.Context, s orchestrator.Session, t orchestrator.Turn) (orchestrator.Result, error)
}

type investigationStarter interface {
	Start(ctx context.Context, sess orchestrator.Session, r investigations.StartRequest) (orchestrator.Investigation, bool, error)
}

type startStores interface {
	StarterFor(tenantID string) (investigationStarter, error)
}

type startConfig struct {
	Workers, Queue int
	Run, Start     time.Duration
}

func parseStartConfig(env Env) (startConfig, bool, error) {
	return startConfig{}, false, errors.New("stub")
}

type turnStarter struct{}

func newTurnStarter(ctx context.Context, cfg startConfig, stores startStores, runner turnRunner, failed func(string, error)) (*turnStarter, error) {
	return nil, errors.New("stub")
}

func (s *turnStarter) Start(ctx context.Context, t inputgateway.Turn) error {
	return errors.New("stub")
}
func (s *turnStarter) Shutdown() {}

func closeInReverse(closers ...func()) func() { return func() {} }

type gatewayService struct {
	Gateway *inputgateway.Gateway
}

func (g *gatewayService) Close()                {}
func (g *gatewayService) Ready() (bool, string) { return true, "" }

func buildGatewayService(ctx context.Context, env Env, clk clock.Clock, ids chain.IDSource, records reconstruct.TenantDBs, stores startStores, runner turnRunner, failed func(string, error)) (*gatewayService, error) {
	return nil, errors.New("stub")
}

func withGatewayServiceReadiness(inner readiness, g *gatewayService) readiness { return inner }
