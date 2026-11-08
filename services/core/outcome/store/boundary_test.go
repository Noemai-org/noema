package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

// INV-11, INV-15, EC-E16, DEBT-04 option A: Outcome writes its Record events through the tenant-bound function on the tenant's own
// login, as the power that login is mapped to. The emitter on every row is Outcome's, and the chain verifies.
func TestOutcomeEventsAreWrittenOnTheTenantsOwnLoginAsOutcome(t *testing.T) {
	f := newFlow(t, verifier.AuthValid, 0)
	if _, err := f.v.Verify(ctx(t), f.req); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT count(*) FROM record_event WHERE tenant_id = $1`, tenant); n == 0 {
		t.Fatal("no Record event was written")
	}
	if n := f.count(t, `SELECT count(*) FROM record_event WHERE tenant_id = $1 AND written_by <> $2`, tenant, "core-outcome"); n != 0 {
		t.Fatalf("%d events were written by an emitter other than core-outcome", n)
	}
	f.verifyChain(t)
}

// EC-E16, DEBT-04 option A: a login that holds outcome_rw but is not mapped to a tenant cannot append to the Record, so the whole
// verdict fails closed: no verdict, no recovery request, no Record event.
func TestAnOutcomeLoginThatIsNotMappedToATenantStoresNothing(t *testing.T) {
	unmapped := func(w *world, t *testing.T) *pgxpool.Pool { return w.loginWith(t, "outcome_rw", "outcome_rw") }
	f := newFlowOn(t, verifier.AuthValid, 0, unmapped)
	if res, err := f.v.Verify(ctx(t), f.req); err == nil {
		t.Fatalf("an unmapped login stored a verdict: %+v", res)
	}
	for _, q := range []string{`SELECT count(*) FROM outcome_verdict`, `SELECT count(*) FROM record_event`, `SELECT count(*) FROM outbox_event`} {
		if n := f.count(t, q); n != 0 {
			t.Fatalf("%s = %d after a refused append", q, n)
		}
	}
}
