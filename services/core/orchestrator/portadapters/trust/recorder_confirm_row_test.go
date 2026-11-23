package trustport

// A552 (interim, relayed, not yet committed; REQ-TRU-009, REQ-TRU-003; EC-E16): a CONFIRM through the real adapter and the real Writer leaves exactly one open request that the confirmation
// store reads back as the request the result named: one transaction, no second creator. All data is SYNTHETIC. Real PostgreSQL (CI; also local with the same server).

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg2"
)

func TestAConfirmThroughTheRealAdapterLeavesExactlyOneOpenRequest(t *testing.T) {
	w := newRecWorld(t)
	got, err := w.a.Record(recCtx(t), rnaRequest(t, "CONFIRM", true))
	if err != nil || got.DecisionID == "" || len(got.GrantIDs) != 0 || got.ConfirmationRequestID == "" {
		t.Fatalf("%+v %v", got, err)
	}
	if n := w.count(t, `SELECT count(*) FROM confirmation_request`); n != 1 {
		t.Fatalf("%d request rows after one CONFIRM, want exactly 1", n)
	}
	store := trustpg2.Requests{Pool: w.trust}
	rec, found, err := store.Latest(recCtx(t), recTenant, "usr_01J9X3ABCDEFGHJKMNPQRSTVWX", "int_01J9X3", 2)
	if err != nil || !found {
		t.Fatalf("the store does not find the request: %v %v", found, err)
	}
	var open bool // not expired on the database's clock
	if err := w.admin.QueryRow(recCtx(t), `SELECT expires_at > now() FROM confirmation_request WHERE confirmation_request_id = $1`, got.ConfirmationRequestID).Scan(&open); err != nil || !open {
		t.Errorf("the request is not open on the database clock: %v %v", open, err)
	}
	if rec.Request.ID != got.ConfirmationRequestID || rec.Answer != nil {
		t.Errorf("the stored request %+v (answered: %v), want the open request %s", rec.Request, rec.Answer != nil, got.ConfirmationRequestID)
	}
	// the decision and its request commit together: the replay returns the stored decision and creates no second request
	again, err := w.a.Record(recCtx(t), rnaRequest(t, "CONFIRM", true))
	if err != nil || again.DecisionID != got.DecisionID {
		t.Fatalf("the replay: %+v %v, want the stored decision %s", again, err, got.DecisionID)
	}
	if n := w.count(t, `SELECT count(*) FROM confirmation_request`); n != 1 {
		t.Errorf("%d request rows after a replay, want 1", n)
	}
}
