package main

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	journalport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/journal"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// tenantJournal is the orchestrator's Journal over the Record reader's per-tenant pools (STM-01 surfaces; REQ-STM-001, REQ-RCV-003; INV-12, INV-21).
type tenantJournal struct{ dbs reconstruct.TenantDBs }

var _ orchestrator.Journal = tenantJournal{}

// Progress reads what the Record holds for the grant as the session's own tenant: through that tenant's pool, never another's, and an error for a tenant that has none (INV-12).
func (j tenantJournal) Progress(ctx context.Context, s orchestrator.Session, investigationID, grantID string) (orchestrator.Progress, error) {
	if j.dbs == nil {
		return orchestrator.Progress{}, errors.New("cmd/core: the Journal needs the Record reader")
	}
	db, err := j.dbs.For(s.TenantID)
	if err != nil {
		return orchestrator.Progress{}, err
	}
	return journalport.Port{DB: db}.Progress(ctx, s, investigationID, grantID)
}
