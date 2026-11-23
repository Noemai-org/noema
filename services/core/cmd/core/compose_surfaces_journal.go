package main

import (
	"context"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// tenantJournal is the orchestrator's Journal over the Record reader's per-tenant pools (STM-01 surfaces; REQ-STM-001, REQ-RCV-003; INV-12, INV-21).
type tenantJournal struct{ dbs reconstruct.TenantDBs }

var _ orchestrator.Journal = tenantJournal{}

// Progress is not built yet.
func (j tenantJournal) Progress(context.Context, orchestrator.Session, string, string) (orchestrator.Progress, error) {
	return orchestrator.Progress{}, nil
}
