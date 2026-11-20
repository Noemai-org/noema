package main

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/workflows"
)

// wire_worker_ports.go: the real activity ports of the escalation workflow (STM-04; REQ-EXE-006, REQ-EXE-005; INV-03, INV-30, AGENT-16). STUB of the red commit.

const (
	envWorkerShutdownDrain = "NOEMA_TEMPORAL_SHUTDOWN_DRAIN_MS"
	envWorkerGrantKeyDir   = "NOEMA_GRANT_KEY_DIR"
	envWorkerGrantKeyMax   = "NOEMA_GRANT_KEY_MAX_ACTIVE"
)

var (
	// ErrWorkerDrainTimeout is returned by Stop when in-flight activities did not finish within the drain bound.
	ErrWorkerDrainTimeout = errors.New("worker: in-flight activities did not finish within the drain bound")
	// ErrWorkerStopping is what an activity gets when it starts after Stop began.
	ErrWorkerStopping = errors.New("worker: the worker is stopping")
	// ErrRouteGrantRefused is what the route issuer returns when Trust did not allow the route.
	ErrRouteGrantRefused = errors.New("worker: Trust did not issue the route grant")
)

// RouteDecision is the ask to Trust for the route step as its own action decision.
type RouteDecision struct {
	TenantID, CorrelationID, CaseID, VersionToken, OwnerQueue, CreateReceiptID, CapabilityID string
}

// RouteDecisionResult is Trust's answer.
type RouteDecisionResult struct {
	Outcome    string
	DecisionID string
	GrantIDs   []string
}

// RouteDecider is Trust, as the issuer sees it.
type RouteDecider interface {
	DecideRoute(ctx context.Context, d RouteDecision) (RouteDecisionResult, error)
}

type routeIssuerStub struct{}

func (routeIssuerStub) IssueRouteGrant(context.Context, workflows.RouteGrantRequest) (string, error) {
	return "", errors.New("not built")
}

func newRouteGrantIssuer(RouteDecider) (workflows.RouteGrantIssuer, error) {
	return routeIssuerStub{}, nil
}

type grantKeyVerifier struct{ keys *grants.Keys }

func (grantKeyVerifier) VerifyWith(string, []byte, string) bool { return true }

func loadWorkerGrantKeys(Env) (*grants.Keys, error) { return nil, errors.New("not built") }

// escalateDeps are the pieces of the Execution Controller and the case reader that the composition supplies.
type escalateDeps struct {
	Clock    controller.Clock
	Receipts controller.ReceiptSigner
	Grants   controller.GrantSource
	Bind     controller.Binder
	Reval    controller.Revalidator
	Ledger   controller.Ledger
	Out      controller.Dispatcher
	IDs      controller.IDs
	Reader   workflows.CaseReader // bridge.CaseReadBack over the case adapter; built where the adapters may be imported (lint import-adapters: cmd may not)
	Decider  RouteDecider
}

func buildEscalatePorts(Env, escalateDeps) (workerPorts, error) {
	return workerPorts{}, errors.New("not built")
}

// fatalReporter is optionally implemented by a Connection: it reports a worker that died on its own.
type fatalReporter interface{ Fatal() <-chan error }
