package main

import (
	"context"
	"errors"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
)

// wire_investigations.go is the production adapter of the InvestigationReader port (investigation.go; STM-01, noema#601; ruling A243, interim; migration 0029). It reads through
// the orchestrator's investigation store, which the Recorder already holds one of per tenant over that tenant's own orchestrator login: the database binds the login to the tenant
// (row-level security with FORCE, migration 0025), so no setting of its own exists (AGENT-16) and the reader can never see another tenant's row. It adds the one rule the
// database cannot know: an investigation is served only to the principal that started it. The tenant and the principal are the arguments the route took from the verified session
// (INV-12); nothing here reads a request.

// investigationReadTimeout bounds one read when the request carries no earlier deadline. It is the value of record/reconstruct.DefaultTimeout, a [REC] there as it is here: it is
// not a safety threshold, and it is listed as an open question for the Engineering Lead (status/STM-01-wire-investigations.md).
const investigationReadTimeout = 5 * time.Second

// ErrInvestigationRead marks a read that failed: the store could not be reached or refused the read. It carries no driver text, which can name a server or a login (AGENT-13).
var ErrInvestigationRead = errors.New("cmd/core: the investigation could not be read")

// investigationStores gives the investigation store of one tenant, or an error: never another tenant's store (orchestratorRecorder.For).
type investigationStores interface {
	For(tenantID string) (*investigations.Store, error)
}

// investigationReader is the InvestigationReader over the per-tenant stores.
type investigationReader struct{ stores investigationStores }

var _ InvestigationReader = investigationReader{}

// newInvestigationReader makes the reader over the stores of the orchestrator's Recorder.
func newInvestigationReader(stores investigationStores) InvestigationReader {
	return investigationReader{stores: stores}
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
	ctx, cancel := context.WithTimeout(ctx, investigationReadTimeout) // the earlier of the request's deadline and this one
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
