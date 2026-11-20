package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
)

// wire_investigations.go is the production adapter of the InvestigationReader port (investigation.go; STM-01, noema#601; ruling A243, interim; migration 0029). It reads through
// the orchestrator's investigation store, which the Recorder already holds one of per tenant over that tenant's own orchestrator login: the database binds the login to the tenant
// (row-level security with FORCE, migration 0025), so it needs no login setting of its own and the reader can never see another tenant's row. It adds the one rule the database
// cannot know: an investigation is served only to the principal that started it. The tenant and the principal are the arguments the route took from the verified session (INV-12);
// nothing here reads a request. The one setting it has is the bound of a read (NOEMA_READ_TIMEOUT_MS, A319), with no default in code (AGENT-16).

// EnvReadTimeoutMS names the bound of one investigation read, in whole milliseconds. It is required whenever the reader is built (that is, whenever the orchestrator's settings are
// given) and has no default: the number is a policy of the Architect (A319 sets 2000) and deployment sets it. A missing or invalid value builds no reader.
const EnvReadTimeoutMS = "NOEMA_READ_TIMEOUT_MS"

// maxReadTimeoutMS keeps the bound a representable duration; it is structural, not a policy number.
const maxReadTimeoutMS = int64(1<<63-1) / int64(time.Millisecond)

// readTimeoutRe is a whole positive number: no sign, no space, no leading zero, ASCII digits only.
var readTimeoutRe = regexp.MustCompile(`\A[1-9][0-9]*\z`)

// parseReadTimeout reads the bound from the setting, strictly. The error names the setting and never echoes the value.
func parseReadTimeout(env Env) (time.Duration, error) {
	raw := env.Getenv(EnvReadTimeoutMS)
	if !readTimeoutRe.MatchString(raw) {
		return 0, fmt.Errorf("cmd/core: %s must be a whole positive number of milliseconds", EnvReadTimeoutMS)
	}
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || ms < 1 || ms > maxReadTimeoutMS {
		return 0, fmt.Errorf("cmd/core: %s must be a whole positive number of milliseconds", EnvReadTimeoutMS)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// ErrInvestigationRead marks a read that failed: the store could not be reached or refused the read. It carries no driver text, which can name a server or a login (AGENT-13).
var ErrInvestigationRead = errors.New("cmd/core: the investigation could not be read")

// investigationStores gives the investigation store of one tenant, or an error: never another tenant's store (orchestratorRecorder.For).
type investigationStores interface {
	For(tenantID string) (*investigations.Store, error)
}

// investigationReader is the InvestigationReader over the per-tenant stores. timeout bounds one read when the request carries no earlier deadline.
type investigationReader struct {
	stores  investigationStores
	timeout time.Duration
}

var _ InvestigationReader = investigationReader{}

// buildInvestigationReader makes the reader over the stores of the orchestrator's Recorder, with the bound of NOEMA_READ_TIMEOUT_MS. With a missing or invalid setting it returns no
// reader and an error that names the setting.
func buildInvestigationReader(env Env, stores investigationStores) (InvestigationReader, error) {
	timeout, err := parseReadTimeout(env)
	if err != nil {
		return nil, err
	}
	return investigationReader{stores: stores, timeout: timeout}, nil
}

// Read returns the internal state of the investigation id when it belongs to this tenant and this principal, and found=false in every other case: absent, another principal's,
// another tenant's, malformed, and a row from before migration 0029 (no principal: it belongs to no one, so it is served to no one, fail closed). A tenant with no store, a store
// that cannot be reached and anything unexpected are an error and never found=false: a failure must not read as an absence.
func (r investigationReader) Read(ctx context.Context, tenant, principal, id string) (orchestrator.State, bool, error) {
	if principal == "" || !investigationPath.MatchString(id) {
		return "", false, nil
	}
	store, err := r.stores.For(tenant)
	if err != nil {
		return "", false, ErrInvestigationRead
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout) // the earlier of the request's deadline and the configured bound
	defer cancel()
	snap, err := store.Read(ctx, orchestrator.Session{TenantID: tenant, PrincipalID: principal}, id)
	switch {
	case errors.Is(err, investigations.ErrNoInvestigation):
		return "", false, nil
	case err != nil:
		return "", false, ErrInvestigationRead
	case snap.Principal == "" || snap.Principal != principal:
		return "", false, nil
	}
	return snap.State, true, nil
}
