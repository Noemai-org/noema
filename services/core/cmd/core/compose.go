package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
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
	// TrustUnbuilt names the Trust adapters that are not yet built when Trust is configured but cannot be plugged (the named, expected gaps of UnbuiltAdaptersError). It is not a
	// component: /readyz names each after "trust" as "trust.<Name>" (fixed text, no value). Empty when Trust is built or not asked for.
	TrustUnbuilt []string
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
		if p.built {
			continue
		}
		names = append(names, p.name)
		if p.name == "trust" {
			for _, n := range c.TrustUnbuilt {
				names = append(names, "trust."+n)
			}
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
// any, buildTrust requires the rest and names the one that is missing (A330, AGENT-16). When the adapters are not yet built (UnbuiltAdaptersError) Trust is not plugged and is no error:
// the settings are still checked, and /readyz names "trust" and each adapter (Components.TrustUnbuilt). The grant keys are loaded by buildTrust from NOEMA_GRANT_KEY_DIR and nowhere else (INV-29).
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
		var unbuilt *UnbuiltAdaptersError
		switch {
		case errors.As(err, &unbuilt) && len(unbuilt.Names) > 0:
			// A named, expected gap (interim, relayed, not yet committed: EL's decision of 20 Nov, with A445): the process starts, Trust is not plugged (the orchestrator's Trust
			// stays the one that refuses) and /readyz names it. The settings are still checked: an invalid one stops the process whether or not Trust can be plugged yet.
			if _, _, err := trustSettings(env, records, d.LoadKeys); err != nil {
				closeAll()
				return Components{}, TrustParts{}, closeAll, err
			}
			c.TrustUnbuilt = unbuilt.Names
		case err != nil && !errors.As(err, &unbuilt):
			closeAll()
			return Components{}, TrustParts{}, closeAll, err
		default:
			var closeTrust func()
			parts, closeTrust, err = buildTrust(ctx, env, records, adapters, d.LoadKeys)
			if err != nil {
				closeAll()
				return Components{}, TrustParts{}, closeAll, err
			}
			closers = append(closers, closeTrust)
			c.Ports.Trust = parts.Trust
		}
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
// activities, are added here as each merges and never from a fake; until then the Trust adapter source gives only the clock, so a process asked for Trust starts without it and /readyz names each adapter
// it lacks (UnbuiltAdaptersError), and a configured worker without activities is not started (A442). Nothing is dialled here.
func productionDeps() composeDeps {
	return composeDeps{
		LoadKeys:         loadGrantKeys,
		TrustAdapters:    trustAdaptersSource(productionPack),
		WorkerDial:       dialTemporal,
		WorkerActivities: func() *workflows.EscalateActivities { return nil },
	}
}

// EnvInvestigationStartTimeout is how long the dial and the start of the worker may take, in whole milliseconds (A472, interim, relayed, not yet committed). Required once a worker is configured;
// no default in code (A330, AGENT-16).
const EnvInvestigationStartTimeout = "NOEMA_INVESTIGATION_START_TIMEOUT_MS"

// EnvInvestigationRunTimeout is the bound of one investigation run, in whole milliseconds (A477, interim, relayed, not yet committed). The loader only compares it with the activity timeout.
const EnvInvestigationRunTimeout = "NOEMA_INVESTIGATION_RUN_TIMEOUT_MS"

// workerSettings are every setting that asks for a worker. Any one of them present means a worker is wanted; the settings that are then absent are a gap (A486).
var workerSettings = []string{EnvTemporalAddress, EnvTemporalNamespace, EnvTemporalTaskQueue, EnvTemporalActivityTimeout, EnvTemporalTLSCert, EnvTemporalTLSKey, EnvTemporalTLSCA, EnvInvestigationStartTimeout}

// workerMissing names the worker settings that are missing, in a fixed order, and nothing else (A486, interim, relayed, recorded in docs#216; it replaces the earlier reading that a
// configured worker with a missing setting is fatal). It is empty when no worker setting is present (the plain "worker" gap) and when the set is complete. A client certificate needs its
// key and the reverse, so the one that is absent is named.
func workerMissing(env Env) []string {
	found := false
	for _, n := range workerSettings {
		if env.Getenv(n) != "" {
			found = true
		}
	}
	if !found {
		return nil
	}
	var m []string
	for _, n := range []string{EnvTemporalAddress, EnvTemporalNamespace, EnvTemporalTaskQueue, EnvTemporalActivityTimeout, EnvTemporalTLSCA, EnvInvestigationStartTimeout} {
		if env.Getenv(n) == "" {
			m = append(m, n)
		}
	}
	cert, key := env.Getenv(EnvTemporalTLSCert), env.Getenv(EnvTemporalTLSKey)
	switch {
	case cert != "" && key == "":
		m = append(m, EnvTemporalTLSKey)
	case cert == "" && key != "":
		m = append(m, EnvTemporalTLSCert)
	}
	return m
}

// readMillis reads a present setting as a positive whole number of milliseconds. The error names the setting and never its value.
func readMillis(env Env, name string) (time.Duration, error) {
	ms, err := strconv.ParseInt(env.Getenv(name), 10, 64)
	if err != nil || ms <= 0 || ms > int64(1<<31) {
		return 0, fmt.Errorf("%s: a positive whole number of milliseconds is required", name)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// readStartTimeout reads the bound of the dial and the start of the worker: NOEMA_INVESTIGATION_START_TIMEOUT_MS, required, no default (A472, AGENT-16).
func readStartTimeout(env Env) (time.Duration, error) {
	return readMillis(env, EnvInvestigationStartTimeout)
}

// checkWorkerSettings refuses a present value that is invalid (A469), whatever else is missing: each present setting is checked by LoadWorkerConfig over an environment in which only the
// absent ones are filled with valid placeholders, so that a gap is never an error here and the loader's own checks stay the one place a worker setting is judged. It then refuses an
// activity timeout that is not above the run timeout when both are present, since a live run could be picked up twice (A477).
func checkWorkerSettings(env Env) error {
	filled := map[string]string{EnvTemporalAddress: "placeholder.invalid:1", EnvTemporalNamespace: "placeholder", EnvTemporalTaskQueue: "placeholder", EnvTemporalActivityTimeout: "1", EnvTemporalTLSCA: "placeholder"}
	over := env
	over.Getenv = func(n string) string {
		if v := env.Getenv(n); v != "" {
			return v
		}
		if n == EnvTemporalTLSCert || n == EnvTemporalTLSKey {
			if env.Getenv(EnvTemporalTLSCert) != "" || env.Getenv(EnvTemporalTLSKey) != "" {
				return "placeholder"
			}
			return ""
		}
		return filled[n]
	}
	if _, err := LoadWorkerConfig(over); err != nil {
		return err
	}
	if env.Getenv(EnvInvestigationStartTimeout) != "" {
		if _, err := readStartTimeout(env); err != nil {
			return err
		}
	}
	if env.Getenv(EnvInvestigationRunTimeout) == "" {
		return nil
	}
	run, err := readMillis(env, EnvInvestigationRunTimeout)
	if err != nil {
		return err
	}
	if env.Getenv(EnvTemporalActivityTimeout) == "" {
		return nil
	}
	act, err := readMillis(env, EnvTemporalActivityTimeout)
	if err != nil {
		return err
	}
	if act <= run {
		return fmt.Errorf("%s must be greater than %s", EnvTemporalActivityTimeout, EnvInvestigationRunTimeout)
	}
	return nil
}

// buildWorker starts the Temporal worker through startWorker. A worker setting that is missing (A486) or whose activities are not built (noema#812) is a gap, not an error: there is no
// worker, /readyz names it, and the process starts. A present value that is invalid, or an activity timeout not above the run timeout, is an error that names the settings and never a
// value (A469, A477); one that was asked to start and cannot is an error and the process does not run without it (A442).
func buildComposedWorker(ctx context.Context, env Env, d composeDeps) (*RunningWorker, error) {
	if err := checkWorkerSettings(env); err != nil {
		return nil, err
	}
	if len(workerMissing(env)) > 0 {
		return nil, nil
	}
	cfg, err := LoadWorkerConfig(env)
	if err != nil || cfg == nil {
		return nil, err
	}
	timeout, err := readStartTimeout(env)
	if err != nil {
		return nil, err
	}
	var acts *workflows.EscalateActivities
	if d.WorkerActivities != nil {
		acts = d.WorkerActivities()
	}
	if acts == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
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
	return processReadinessGap(c, api, rw, nil)
}

// processReadinessGap is processReadiness with the worker settings that are missing (A486): each is named as "worker: NAME missing", fixed text, never a value. With none, the gap is the
// plain "worker" (nothing configured, or the activities not built).
func processReadinessGap(c Components, api http.Handler, rw *RunningWorker, missingSettings []string) readiness {
	base := composeReadiness(c, api)
	gaps := []string{"worker"}
	if len(missingSettings) > 0 {
		gaps = nil
		for _, n := range missingSettings {
			gaps = append(gaps, "worker: "+n+" missing")
		}
	}
	return func() (bool, string) {
		ok, reason := base()
		switch {
		case rw != nil:
			return ok, reason
		case ok:
			return false, notConfigured(gaps)
		}
		return false, reason + ", " + strings.Join(gaps, ", ")
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
	rw, err := buildComposedWorker(ctx, env, d)
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
	p.Handler = newMux(processReadinessGap(c, api, rw, workerMissing(env)), api)
	p.Close = func() {
		rw.Stop()
		closeComponents()
	}
	return p, nil
}
