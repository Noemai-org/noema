package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/grants/keyfiles"
	"github.com/Noemai-org/noema/services/core/workflows"
)

// wire_worker_ports.go: the real activity ports of the escalation workflow (STM-04; REQ-EXE-006, REQ-EXE-005, REQ-SEC-005; INV-03, INV-30, AGENT-16). The executor is the bridge's over an
// Execution Controller that verifies grants under the key ring of NOEMA_GRANT_KEY_DIR; the issuer asks Trust and returns the grant Trust recorded. Nothing here mints a grant: the route
// step is its own action decision (PERD 23.1, A335), made by Trust (INV-03).

// The settings. None has a default (AGENT-16, A330).
const (
	// envWorkerShutdownDrain is how long Stop waits for in-flight activities before it stops the worker anyway, in milliseconds.
	envWorkerShutdownDrain = "NOEMA_TEMPORAL_SHUTDOWN_DRAIN_MS"
	// envWorkerGrantKeyDir is the directory of the grant key files (SEC-09); envWorkerGrantKeyMax is the rotation overlap, one or two keys.
	envWorkerGrantKeyDir = "NOEMA_GRANT_KEY_DIR"
	envWorkerGrantKeyMax = "NOEMA_GRANT_KEY_MAX_ACTIVE"
	// workerGrantKeysCeiling mirrors the most keys a grants.Keys ring holds (one, or two during a rotation).
	workerGrantKeysCeiling = 2
)

var (
	// ErrWorkerDrainTimeout is returned by Stop when in-flight activities did not finish within the drain bound.
	ErrWorkerDrainTimeout = errors.New("worker: in-flight activities did not finish within the drain bound")
	// ErrWorkerStopping is what an activity gets when it starts after Stop began: nothing is written once the worker is going down.
	ErrWorkerStopping = errors.New("worker: the worker is stopping")
	// ErrRouteGrantRefused is what the route issuer returns when Trust did not allow the route, or answered outside its contract.
	ErrRouteGrantRefused = errors.New("worker: Trust did not issue the route grant")
)

// RouteDecision is the ask to Trust for the route step as its own action decision on one capability, bound to the case and the version token that were read back.
type RouteDecision struct {
	TenantID, CorrelationID, CaseID, VersionToken, OwnerQueue, CreateReceiptID, CapabilityID string
}

// RouteDecisionResult is Trust's answer: the outcome, the Decision Record Trust wrote before it answered and the grants it issued with it.
type RouteDecisionResult struct {
	Outcome    string
	DecisionID string
	GrantIDs   []string
}

// RouteDecider is Trust as the issuer sees it. The composition supplies the implementation over the intake (trust/intake) and its Planner and Recorder adapters; the issuer imports
// none of them.
type RouteDecider interface {
	DecideRoute(ctx context.Context, d RouteDecision) (RouteDecisionResult, error)
}

type routeGrantIssuer struct{ trust RouteDecider }

// newRouteGrantIssuer returns the workflow's RouteGrantIssuer over Trust. It refuses to be built without Trust.
func newRouteGrantIssuer(d RouteDecider) (workflows.RouteGrantIssuer, error) {
	if d == nil {
		return nil, errors.New("cmd/core: the route grant issuer needs Trust (a RouteDecider)")
	}
	return routeGrantIssuer{trust: d}, nil
}

// IssueRouteGrant asks Trust to decide the route capability for the case and returns the one grant that decision carries. Anything but an ALLOW with a decision and exactly one grant is
// no grant (INV-01: the answer is Trust's, never guessed). A request that does not name what the grant binds is refused before Trust is asked.
func (i routeGrantIssuer) IssueRouteGrant(ctx context.Context, req workflows.RouteGrantRequest) (string, error) {
	for name, v := range map[string]string{"tenant": req.TenantID, "correlation": req.CorrelationID, "case": req.CaseID, "version token": req.VersionToken, "owner queue": req.PlannedQueue} {
		if v == "" {
			return "", fmt.Errorf("%w: the request does not name its %s", ErrRouteGrantRefused, name)
		}
	}
	res, err := i.trust.DecideRoute(ctx, RouteDecision{TenantID: req.TenantID, CorrelationID: req.CorrelationID, CaseID: req.CaseID, VersionToken: req.VersionToken,
		OwnerQueue: req.PlannedQueue, CreateReceiptID: req.CreateReceiptID, CapabilityID: bridge.CapRouteToInternalOwner})
	if err != nil {
		return "", fmt.Errorf("worker: Trust route decision: %w", err)
	}
	switch {
	case res.Outcome != string(contracts.EnumsV1DecisionOutcomeALLOW):
		return "", fmt.Errorf("%w: the outcome is not ALLOW", ErrRouteGrantRefused)
	case res.DecisionID == "":
		return "", fmt.Errorf("%w: an ALLOW with no recorded decision", ErrRouteGrantRefused)
	case len(res.GrantIDs) != 1 || !strings.HasPrefix(res.GrantIDs[0], "grt_") || len(res.GrantIDs[0]) == len("grt_"):
		return "", fmt.Errorf("%w: an ALLOW must carry exactly one grant", ErrRouteGrantRefused)
	}
	return res.GrantIDs[0], nil
}

// grantKeyVerifier adapts the key ring to the Controller's KeyVerifier (E1): the signature is checked under the key it names, which must be active.
type grantKeyVerifier struct{ keys *grants.Keys }

func (v grantKeyVerifier) VerifyWith(keyID string, msg []byte, sigHex string) bool {
	return v.keys.VerifyBytes(contracts.CommonV1Sig{KeyId: keyID, Value: sigHex}, msg)
}

// loadWorkerGrantKeys reads the grant key ring from NOEMA_GRANT_KEY_DIR. Both settings are required and have no default; the overlap is one key or two. An error never holds a key.
func loadWorkerGrantKeys(env Env) (*grants.Keys, error) {
	dir := env.Getenv(envWorkerGrantKeyDir)
	if dir == "" {
		return nil, fmt.Errorf("cmd/core: %s is required", envWorkerGrantKeyDir)
	}
	n, err := strconv.Atoi(env.Getenv(envWorkerGrantKeyMax))
	if err != nil || n < 1 || n > workerGrantKeysCeiling {
		return nil, fmt.Errorf("cmd/core: %s must be 1 or 2", envWorkerGrantKeyMax)
	}
	keys, err := keyfiles.Load(dir, keyfiles.Options{MaxActive: n})
	if err != nil {
		return nil, fmt.Errorf("cmd/core: %s: %w", envWorkerGrantKeyDir, err)
	}
	return keys, nil
}

// escalateDeps are what the composition supplies: the Execution Controller's own parts (clock, receipt signer, grant source, binder, revalidator, ledger, dispatcher, IDs), the case
// read-back (bridge.CaseReadBack over the case adapter: cmd may not import adapters, so it is built where that is allowed) and Trust.
type escalateDeps struct {
	Clock    controller.Clock
	Receipts controller.ReceiptSigner
	Grants   controller.GrantSource
	Bind     controller.Binder
	Reval    controller.Revalidator
	Ledger   controller.Ledger
	Out      controller.Dispatcher
	IDs      controller.IDs
	Reader   workflows.CaseReader
	Decider  RouteDecider
}

// buildEscalatePorts builds the three activity ports: the bridge's executor over a Controller whose grant keys come from the key files, the supplied case read-back, and the route
// issuer over Trust. A missing piece or a bad key setting is an error that names it, and no port comes back (a half worker would stop a case half way).
func buildEscalatePorts(env Env, d escalateDeps) (workerPorts, error) {
	missing := ""
	for _, c := range []struct {
		name string
		nil_ bool
	}{
		{"Clock", d.Clock == nil}, {"Receipts", d.Receipts == nil}, {"Grants", d.Grants == nil}, {"Bind", d.Bind == nil}, {"Reval", d.Reval == nil},
		{"Ledger", d.Ledger == nil}, {"Out", d.Out == nil}, {"IDs", d.IDs == nil}, {"Reader", d.Reader == nil}, {"Decider", d.Decider == nil},
	} {
		if c.nil_ {
			missing = c.name
			break
		}
	}
	if missing != "" {
		return workerPorts{}, fmt.Errorf("cmd/core: the escalation ports need %s", missing)
	}
	keys, err := loadWorkerGrantKeys(env)
	if err != nil {
		return workerPorts{}, err
	}
	issuer, err := newRouteGrantIssuer(d.Decider)
	if err != nil {
		return workerPorts{}, err
	}
	ctl := &controller.Controller{Clock: d.Clock, Keys: grantKeyVerifier{keys: keys}, Receipts: d.Receipts, Grants: d.Grants, Bind: d.Bind, Reval: d.Reval, Ledger: d.Ledger,
		Out: d.Out, IDs: d.IDs}
	return workerPorts{Executor: bridge.GrantExecutor{Controller: ctl}, Reader: d.Reader, Issuer: issuer}, nil
}

// fatalReporter is optionally implemented by a Connection: Fatal delivers the error of a worker that died on its own (the SDK's OnFatalError). The handle then turns not ready.
type fatalReporter interface{ Fatal() <-chan error }
