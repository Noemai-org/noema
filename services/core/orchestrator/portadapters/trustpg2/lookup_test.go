package trustpg2_test

// A550 (interim, `DEC-2026-11-23-architect-rulings-a542-a553`; REQ-TRU-009, REQ-TRU-003, REQ-SEC-004; INV-12): the confirmation request-by-id read. The confirmation route finds the request its path
// names, for the session's tenant only; the answer for another tenant's request and for an unknown one is the same, not found. Real PostgreSQL, real tenant logins. All data is SYNTHETIC.

import (
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg2"
)

// A stored request is read back by its ID with its eight bound values, requirement, requester and expiry, whether or not it was answered; no event is written by the read.
func TestLookupReadsTheRequestTheIDNames(t *testing.T) {
	w := newReqWorld(t)
	n := w.newReq("sha256:l1", 2*time.Second, "") // short, so that it has expired when the next link of the chain is opened below
	id, err := w.a.Open(reqCtx(t), n)
	if err != nil {
		t.Fatal(err)
	}
	events := len(w.sink.requests) + len(w.sink.received) + len(w.sink.expired)
	got, ok, err := w.a.Lookup(reqCtx(t), "ten_aa", id)
	if err != nil || !ok {
		t.Fatalf("Lookup: ok=%v err=%v, want the stored request", ok, err)
	}
	if got.ID != id || got.Binding != n.Binding || got.Requirement != "MAKER_CHECKER" || got.RequesterID != "prn_1" || got.ExpiresAt == 0 {
		t.Errorf("Lookup = %+v, want the stored request %s", got, id)
	}
	if after := len(w.sink.requests) + len(w.sink.received) + len(w.sink.expired); after != events {
		t.Errorf("the read wrote %d Record events", after-events)
	}
	// the request of an older link of the chain is still found by its own ID
	w.waitExpired(t, id)
	second, err := w.a.Open(reqCtx(t), w.newReq("sha256:l2", time.Hour, id))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{id, second} {
		if got, ok, err := w.a.Lookup(reqCtx(t), "ten_aa", want); err != nil || !ok || got.ID != want {
			t.Errorf("Lookup(%s) = %+v ok=%v err=%v", want, got, ok, err)
		}
	}
}

// Another tenant's request, an unknown ID and the same ID read through the other tenant's login are one answer: not found, no error that tells them apart.
func TestLookupOfAnotherTenantsOrAnUnknownRequestIsNotFound(t *testing.T) {
	w := newReqWorld(t)
	id, err := w.a.Open(reqCtx(t), w.newReq("sha256:l1", time.Hour, ""))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		store  trustpg2.Requests
		tenant string
		id     string
	}{
		"tenant B's login asks tenant A's ID": {w.b, "ten_bb", id},
		"tenant B's name on tenant A's login": {w.a, "ten_bb", id},
		"an unknown ID":                       {w.a, "ten_aa", "cfr_NOSUCHONE"},
		"tenant A's name on tenant B's login": {w.b, "ten_aa", id},
	} {
		got, ok, err := c.store.Lookup(reqCtx(t), c.tenant, c.id)
		if ok || err != nil || got.ID != "" {
			t.Errorf("%s: %+v ok=%v err=%v, want not found and no error", name, got, ok, err)
		}
	}
}

// A request or a tenant that is not in the contract's form, and a context with no deadline, are ErrRequestForm: the database is not asked.
func TestLookupRefusesWhatIsNotInTheContractsForm(t *testing.T) {
	w := newReqWorld(t)
	for name, c := range map[string]struct{ tenant, id string }{
		"an ID that is not a cfr_ ID": {"ten_aa", "dec_01ABC"},
		"an empty ID":                 {"ten_aa", ""},
		"an empty tenant":             {"", "cfr_01ABCDEF"},
		"a tenant with a quote":       {"ten_aa'--", "cfr_01ABCDEF"},
	} {
		if _, ok, err := w.a.Lookup(reqCtx(t), c.tenant, c.id); ok || !errors.Is(err, trustpg2.ErrRequestForm) {
			t.Errorf("%s: ok=%v err=%v, want ErrRequestForm", name, ok, err)
		}
	}
}
