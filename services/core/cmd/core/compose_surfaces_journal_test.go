package main

// STM-01 surfaces, the Journal (REQ-STM-001, REQ-RCV-003; INV-12, INV-21, AGENT-16): the orchestrator's Journal reads the Record as the session's own tenant, through that tenant's
// own pool, and never falls back to another tenant's. Every tenant, principal and ID is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

type sfBeginner struct{ begun int }

func (b *sfBeginner) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	b.begun++
	return nil, errors.New("synthetic: no database")
}

type sfTenantDBs map[string]*sfBeginner

func (m sfTenantDBs) For(tenant string) (reconstruct.TxBeginner, error) {
	b, ok := m[tenant]
	if !ok {
		return nil, errors.New("synthetic: no pool for this tenant")
	}
	return b, nil
}

func sfDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

var sfSession = orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_synth1"}

// The session's tenant's own pool is the one that is asked, and nobody else's.
func TestTheJournalReadsThroughTheSessionsOwnTenantPoolOnly(t *testing.T) {
	a, b := &sfBeginner{}, &sfBeginner{}
	_, err := tenantJournal{dbs: sfTenantDBs{"ten_aa": a, "ten_bb": b}}.Progress(sfDeadline(t), sfSession, "inv_AA11", "grt_AA11")
	if err == nil {
		t.Fatal("the read went through, though the pool has no database: the answer must be the pool's error")
	}
	if a.begun != 1 || b.begun != 0 {
		t.Fatalf("ten_aa begun %d, ten_bb begun %d; want 1 and 0", a.begun, b.begun)
	}
}

// A tenant with no pool is an error, never another tenant's pool (INV-12).
func TestTheJournalOfATenantWithNoPoolIsAnErrorAndAsksNoOtherPool(t *testing.T) {
	b := &sfBeginner{}
	prog, err := tenantJournal{dbs: sfTenantDBs{"ten_bb": b}}.Progress(sfDeadline(t), sfSession, "inv_AA11", "grt_AA11")
	if err == nil || prog != (orchestrator.Progress{}) || b.begun != 0 {
		t.Fatalf("progress %+v, error %v, ten_bb begun %d; want an error, nothing, and 0", prog, err, b.begun)
	}
}

// No reader at all is an error too, not a panic.
func TestTheJournalWithNoRecordReaderIsAnError(t *testing.T) {
	if _, err := (tenantJournal{}).Progress(sfDeadline(t), sfSession, "inv_AA11", "grt_AA11"); err == nil {
		t.Fatal("no error without a Record reader")
	}
}
