package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	admissionsport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/admissions"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/redaction"
	"github.com/Noemai-org/noema/services/core/record/admission"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"github.com/Noemai-org/noema/services/core/workflows"
)

// compose.go is the composition root of the G1 slice (EDG-01 slice 3c). It is the only place that plugs the components other tasks build into the orchestrator,
// and the only file that does: the people who build a component give a constructor, and the Engineering Lead plugs it in here (the Architect's order of 11 Nov).
// It holds no product logic. A component that is missing means no composition and a /readyz that names every one that is, so the process never serves half a slice.

// Components is everything the slice needs. Each field is built by another task and handed in:
//   - Ports.Interpreter: the model service client (UND), behind the input gateway;
//   - Ports.Evidence: the Evidence Layer over the stand-in read port (EVD-01, ENV-04, noema#441);
//   - Ports.Trust: the Trust gate and the Decision Record writer (TRU-05, noema#237);
//   - Ports.Explainer: the template explanation renderer (EXP-01);
//   - Ports.Recorder: the Record-backed recorder of state changes (STM-01 slice 2);
//   - Ports.Admissions: the Record read that confirms a turn's INPUT_RECEIVED event (A161; a read as the session's tenant, STM-01 slice 2);
//   - Records: the Record reader, one pool per tenant, each connecting as that tenant's record_read login (REC-03, DEBT-04); the reconstruction route is mounted only
//     with it;
//   - the protected /v1 handler is not a field: buildAPI makes it from the session settings of the environment (EDG-01 slice 3b, edge.Load) and compose takes it as an argument.
type Components struct {
	Ports   orchestrator.Ports
	Records reconstruct.TenantDBs
	// Gateway is the Input Gateway (UND-02); POST /v1/conversations is mounted only with it.
	Gateway *inputgateway.Gateway
	// Investigations reads the caller's own investigation (the read-only store method of A243); GET /v1/investigations/{id} is mounted only with it.
	Investigations InvestigationReader
	// ServedTenants is every tenant this deployment serves, as the session settings name them (NOEMA_SESSION_TENANTS). It is not a component: the Record reader is checked against
	// it, and a served tenant with no Record login makes /readyz name "record logins" (A434).
	ServedTenants []string
	// AccessLog is where the RECORD_ACCESSED lines of Record reads go (REQ-AUD-003). It is required whenever Records is set: a Record reader that logs nothing is a startup error.
	AccessLog *slog.Logger
	// Confirmations is the confirmation route's ports (POST /v1/confirmations/{confirmation_request_id}, A341, noema#754); the route is mounted only with it.
	Confirmations *confirmationRouteConfig
}

// Env is the process's configuration source, injected so that tests do not touch the real environment. Nothing read through it has a default (AGENT-16).
type Env struct {
	Getenv   func(string) string
	ReadFile func(string) ([]byte, error)
	ReadDir  func(string) ([]os.DirEntry, error)
}

// App is the composed slice. Today it holds the runner; the HTTP handlers that sit behind the protected API are added in buildAPI as they are built.
type App struct {
	Runner *orchestrator.Runner
}

// rolesAudit may read a reconstruction: audit only (PERD 39.4; A110, DEC-2026-11-08: "ops has no path to them"; ruling A20: audit reads only through the tenant-bound Record
// read functions). Widening it needs a decision record that names the class and the tenant rule.
var rolesAudit = []session.Role{session.RoleAudit}

// apiRoutes is the route table of the public API: each route with the roles that may call it. A route exists only when the component behind it does.
func apiRoutes(c Components, ids session.IDs) []edge.Route {
	var routes []edge.Route
	if c.Gateway != nil {
		routes = append(routes, edge.Route{Pattern: inputgateway.Route, Roles: rolesCustomer, Handler: startConversation(c.Gateway.Handler(gatewayIdentity))})
	}
	if c.Investigations != nil {
		routes = append(routes, investigationRoute(c.Investigations))
	}
	if c.Confirmations != nil {
		cfg := *c.Confirmations
		if cfg.Logger == nil {
			cfg.Logger = c.AccessLog // the one redacted process logger (redactedLog); a route with its own logger keeps it
		}
		routes = append(routes, confirmationRoute(cfg))
	}
	if c.Records != nil {
		routes = append(routes, edge.Route{
			Pattern: reconstruct.Route,
			Roles:   rolesAudit,
			Handler: &reconstruct.Handler{DBs: c.Records, Auth: reconstruct.SessionAuthorizer{}, IDs: ids, Log: c.AccessLog},
		})
	}
	return routes
}

// buildAPI makes the protected /v1 handler (EDG-01 slice 3b): the verified session, the role gate of each route (deny by default) and the identity-free body guard, then the
// route's handler. It returns nil and no error when none of the session settings is given, and the core then serves health only; with some of them, or any wrong, it returns
// an error and the process does not start. A Record reader without session settings is an error too: its route would be unreachable. Routes are mounted only for the
// components that exist.
func buildAPI(c Components, e Env, clk clock.Clock, ids session.IDs) (http.Handler, error) {
	if c.Records != nil && c.AccessLog == nil {
		return nil, errors.New("the Record reader is configured without an access logger: its reads would not be logged (REQ-AUD-003)")
	}
	if c.Confirmations != nil && c.AccessLog == nil {
		return nil, errors.New("the confirmation route is configured without a logger: its refusals would not be logged (A341, AGENT-13)")
	}
	routes := apiRoutes(c, ids)
	h, err := edge.Load(e.Getenv, e.ReadFile, clk, ids, routes)
	if err != nil {
		return nil, fmt.Errorf("session configuration: %w", err)
	}
	if h == nil && len(routes) > 0 {
		return nil, errors.New("the Record reader is configured but the session settings are not: its route would be unreachable")
	}
	return h, nil
}

// missing names every component the composition does not have, in the one order of the table below (the ports first, then the rest), or nil when none is missing. It is the
// only place that says what a complete slice is: compose refuses and /readyz is not green exactly when it returns a name, and every field of Components has a line here (a
// test fails for a field without one). A component is one line, so a lane that plugs in a new adapter adds one.
func missing(c Components, api http.Handler) []string {
	parts := []struct {
		name  string
		built bool
	}{
		{"interpreter", c.Ports.Interpreter != nil},
		{"evidence", c.Ports.Evidence != nil},
		{"trust", c.Ports.Trust != nil},
		{"explainer", c.Ports.Explainer != nil},
		{"recorder", c.Ports.Recorder != nil},
		{"admissions", c.Ports.Admissions != nil},
		{"investigations", c.Investigations != nil},
		{"edge chain", api != nil},
		{"record reader", c.Records != nil},
		{"record logins", recordLoginsPresent(c)},
		{"input gateway", c.Gateway != nil},
		{"confirmation route", c.Confirmations != nil},
	}
	var names []string
	for _, p := range parts {
		if !p.built {
			names = append(names, p.name)
		}
	}
	return names
}

// recordLoginsPresent is false when the deployment serves a tenant that the Record reader has no login for (A434, DEC-2026-11-20-architect-rulings-a358-a453: a deployment defect,
// so /readyz is not green). It asks the reader what the reconstruction handler asks it, For, so the two cannot disagree; For dials nothing. With no reader there is nothing to
// check here: "record reader" names that. Nothing about the tenant is returned: the name is fixed.
func recordLoginsPresent(c Components) bool {
	if c.Records == nil {
		return true
	}
	for _, tenant := range c.ServedTenants {
		if db, err := c.Records.For(tenant); err != nil || db == nil {
			return false
		}
	}
	return true
}

// servedTenants is the tenants the session settings name, split as edge.Load splits them. Unset means none are served.
func servedTenants(env Env) []string {
	v := env.Getenv(edge.EnvTenants)
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

// notConfigured is the sentence of a refusal and of /readyz for the names missing returned: the names and nothing else, never a setting's value, a path or a driver's text.
func notConfigured(names []string) string {
	return "not configured: " + strings.Join(names, ", ")
}

// compose builds the runner from the real transition table, or refuses when a component is missing.
func compose(c Components, api http.Handler) (*App, error) {
	if names := missing(c, api); len(names) > 0 {
		return nil, fmt.Errorf("compose: %s", notConfigured(names))
	}
	tab, err := orchestrator.LoadTable()
	if err != nil {
		return nil, fmt.Errorf("compose: the transition table: %w", err)
	}
	r, err := orchestrator.NewRunner(&orchestrator.Machine{Table: tab}, c.Ports)
	if err != nil {
		return nil, fmt.Errorf("compose: %w", err)
	}
	return &App{Runner: r}, nil
}

// composeReadiness is the /readyz check for a composition: ready only when compose would succeed, and otherwise it names every component that is missing.
func composeReadiness(c Components, api http.Handler) readiness {
	return func() (bool, string) {
		if names := missing(c, api); len(names) > 0 {
			return false, notConfigured(names)
		}
		return true, ""
	}
}

// redactedLog is the access log: the process logger behind the mandatory redactor (REQ-SEC-003, AGENT-13, ruling A154), so that a RECORD_ACCESSED line never carries a digit run of
// more than four, whatever a caller wrote in the correlation it asked for. A nil logger stays nil: a Record reader with no access log is a startup error (buildAPI).
func redactedLog(l *slog.Logger) *slog.Logger {
	if l == nil {
		return nil
	}
	return slog.New(redaction.New().Handler(l.Handler()))
}

// buildComponents builds what main composes, in one place, and returns it with one function that closes every pool opened (safe to call more than once, and returned with an error
// too). Only adapters that are merged are plugged in: the Record reader, the Admissions port over it (one reader, so a turn's input is confirmed as the session's tenant) and the
// orchestrator's Recorder and, over the same per-tenant stores, the investigation reader (A243), which needs NOEMA_READ_TIMEOUT_MS as well (A319). With none of the settings there is nothing and no error; any setting that cannot be applied is an error that names it and leaves no pool open. The ports
// whose adapters are not built stay nil, so /readyz names them (missing). Nothing is dialled here.
func buildComponents(ctx context.Context, env Env, clk clock.Clock, ids chain.IDSource, logger *slog.Logger) (Components, func(), error) {
	c, _, closer, err := buildComponentsWith(ctx, env, clk, ids, logger, productionDeps())
	return c, closer, err
}

// buildComponentsWith is buildComponents with its seams, and it also returns the Trust parts the write path needs later (the Bindings, the grant keys, the intake service). Trust is
// built here, once, with buildTrust, and only when one of the Trust settings is given (trustRequested): with none there is nothing and no error, and /readyz names "trust"; with
// any, buildTrust requires the rest and names the one that is missing (A330, AGENT-16). The grant keys are loaded by buildTrust from NOEMA_GRANT_KEY_DIR and nowhere else (INV-29).
func buildComponentsWith(ctx context.Context, env Env, clk clock.Clock, ids chain.IDSource, logger *slog.Logger, d composeDeps) (Components, TrustParts, func(), error) {
	var closers []func()
	closeAll := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	records, closeRecords, err := buildRecordReader(ctx, env)
	if err != nil {
		return Components{}, TrustParts{}, closeAll, err
	}
	closers = append(closers, closeRecords)
	recorder, closeRecorder, err := buildRecorder(ctx, env, clk, ids, records)
	if err != nil {
		closeAll()
		return Components{}, TrustParts{}, closeAll, err
	}
	closers = append(closers, closeRecorder)
	c := Components{Records: records, ServedTenants: servedTenants(env), AccessLog: redactedLog(logger)}
	if records != nil {
		c.Ports.Admissions = admissionsport.Port{Reader: &admission.Reader{DBs: records}}
	}
	if recorder != nil {
		reader, err := buildInvestigationReader(env, recorder) // reads over the same per-tenant orchestrator logins; the bound is NOEMA_READ_TIMEOUT_MS (A243, A319, noema#715)
		if err != nil {
			closeAll()
			return Components{}, TrustParts{}, closeAll, err
		}
		c.Ports.Recorder = recorder
		c.Investigations = reader
	}
	var parts TrustParts
	if trustRequested(env) {
		if d.TrustAdapters == nil {
			closeAll()
			return Components{}, TrustParts{}, closeAll, errors.New("cmd/core: the Trust adapters are not configured")
		}
		adapters, err := d.TrustAdapters(env, records, clk)
		if err != nil {
			closeAll()
			return Components{}, TrustParts{}, closeAll, err
		}
		var closeTrust func()
		parts, closeTrust, err = buildTrust(ctx, env, records, adapters, d.LoadKeys)
		if err != nil {
			closeAll()
			return Components{}, TrustParts{}, closeAll, err
		}
		closers = append(closers, closeTrust)
		c.Ports.Trust = parts.Trust
	}
	return c, parts, closeAll, nil
}

// trustRequested says whether any setting that only Trust uses is given. NOEMA_TRUST_DSN_DIR is not one: the payload reader reads it too, so it alone does not ask for Trust.
func trustRequested(env Env) bool {
	for _, n := range []string{EnvTrustDBTimeout, EnvGrantKeyDir, EnvGrantKeyMaxActive} {
		if env.Getenv(n) != "" {
			return true
		}
	}
	return false
}

// composeDeps are the seams of the process composition (STM-01 main): what the production build gets from adapter packages and the real loaders, and what a test replaces.
type composeDeps struct {
	LoadKeys         KeyLoader
	TrustAdapters    func(env Env, records reconstruct.TenantDBs, clk clock.Clock) (TrustAdapters, error)
	WorkerDial       func(context.Context, *WorkerConfig) (Connection, error)
	WorkerActivities func() *workflows.EscalateActivities
}

// productionDeps is the real build: the grant keys through keyfiles.Load (INV-29) and the Temporal dial of worker_dial.go. The intake adapters other tasks build, and the worker's
// activities, are added here as each merges and never from a fake; until then the Trust adapter source gives only the clock, so a process asked for Trust names the first adapter it
// lacks, and a configured worker without activities is not started (A442). Nothing is dialled here.
func productionDeps() composeDeps {
	return composeDeps{
		LoadKeys:         loadGrantKeys,
		TrustAdapters:    trustAdaptersSource(productionPack),
		WorkerDial:       dialTemporal,
		WorkerActivities: func() *workflows.EscalateActivities { return nil },
	}
}

// workerStartTimeout bounds the dial and the start of the worker. It is structural (a process must not wait for Temporal for ever), not a safety number.
const workerStartTimeout = 30 * time.Second

// buildWorker starts the Temporal worker through startWorker when it is configured (LoadWorkerConfig). With no Temporal setting there is no worker and no error. A configured worker
// that cannot start is an error and the process does not run without it: no silent degraded mode (A442).
func buildWorker(ctx context.Context, env Env, d composeDeps) (*RunningWorker, error) {
	cfg, err := LoadWorkerConfig(env)
	if err != nil || cfg == nil {
		return nil, err
	}
	var acts *workflows.EscalateActivities
	if d.WorkerActivities != nil {
		acts = d.WorkerActivities()
	}
	ctx, cancel := context.WithTimeout(ctx, workerStartTimeout)
	defer cancel()
	return startWorker(ctx, cfg, d.WorkerDial, acts)
}

// Process is the composed core: the redacting logger every component received, the trace attribute filter (the core has no tracer yet; the filter is the one a tracer must use), what
// buildComponentsWith made, the protected API, the handler of the health routes and the API, and the worker. Close stops the worker and closes every pool, safe to call more than once.
type Process struct {
	Logger      *slog.Logger
	TraceFilter func(map[string]string) map[string]string
	Components  Components
	Trust       TrustParts
	API         http.Handler
	Handler     http.Handler
	Worker      *RunningWorker
	Close       func()
}

// newProcessLogger makes the one logger of the process: JSON to w through the redactor (SEC-03; REQ-SEC-003, AGENT-13). No message or attribute reaches w with a secret-looking
// value or a digit run longer than four.
func newProcessLogger(w io.Writer) (*slog.Logger, *redaction.Redactor) {
	red := redaction.New()
	return slog.New(red.Handler(slog.NewJSONHandler(w, nil))), red
}

// processReadiness is the /readyz check of the process: the composition's, plus a "worker" line while no worker runs. Liveness of a running worker is the next commit (noema#794).
func processReadiness(c Components, api http.Handler, rw *RunningWorker) readiness {
	base := composeReadiness(c, api)
	return func() (bool, string) {
		ok, reason := base()
		switch {
		case rw != nil:
			return ok, reason
		case ok:
			return false, notConfigured([]string{"worker"})
		}
		return false, reason + ", worker"
	}
}

// newProcess composes the process. The Process is never nil: on an error it still holds the logger (the caller logs the error through it) and a Close that releases what was opened.
func newProcess(ctx context.Context, env Env, clk clock.Clock, ids chain.IDSource, sink io.Writer, d composeDeps) (*Process, error) {
	logger, red := newProcessLogger(sink)
	p := &Process{Logger: logger, TraceFilter: red.FilterAttributes, Close: func() {}}
	c, parts, closeComponents, err := buildComponentsWith(ctx, env, clk, ids, logger, d)
	p.Close = closeComponents
	if err != nil {
		return p, err
	}
	rw, err := buildWorker(ctx, env, d)
	if err != nil {
		closeComponents()
		return p, err
	}
	api, err := buildAPI(c, env, clk, ids)
	if err != nil {
		rw.Stop()
		closeComponents()
		return p, err
	}
	p.Components, p.Trust, p.Worker, p.API = c, parts, rw, api
	p.Handler = newMux(processReadiness(c, api, rw), api)
	p.Close = func() {
		rw.Stop()
		closeComponents()
	}
	return p, nil
}
