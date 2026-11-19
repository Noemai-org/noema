package events_test

// EDG-02, review of noema#726 (Codex P2, interim, relayed, not yet committed): the route never mutates what the Feed returned. Feed.Since does not transfer ownership of its
// payload pointers, so the route's own members (event, schema_version) are set on a copy, and the copy is what is encoded. REQ-API-001, INV-12, AGENT-13.
// Every payload here is SYNTHETIC (AGENT-25).

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/events"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// sharedFeed returns the same Batch, with the same payload pointers, to every caller, as a caching Feed would.
type sharedFeed struct{ batch events.Batch }

func (f *sharedFeed) Since(context.Context, session.Principal, string, int64) (events.Batch, error) {
	return f.batch, nil
}

func confirmationEv(cursor int64) events.Event {
	return events.Event{Cursor: cursor, Confirmation: &contracts.SseEventV1Confirmation{At: stamp(cursor), Card: contracts.OverlayCardV1ConfirmationCard{CardType: "CONFIRMATION", SchemaVersion: "1.0",
		ConfirmationRequestId: "cfr_AB12", Action: "Freeze the card", Target: "Card ending 1234", EvidenceSummary: "Checked.", PolicyVersion: "v1", ExpiresAt: stamp(cursor + 60)}}}
}

func errorEv(cursor int64) events.Event {
	return events.Event{Cursor: cursor, Error: &contracts.SseEventV1Error{At: stamp(cursor), Error: contracts.ErrorsV3ErrorBody{Code: contracts.ErrorsV3ErrorCodeINTERNALSAFETYBLOCK,
		Message: "The request cannot be served.", CorrelationId: "corr_AB12"}}}
}

func newRigShared(t *testing.T, f events.Feed) rig {
	t.Helper()
	return newRigWith(t, &fakeFeed{}, nil, func(o *events.Options) { o.Feed = f })
}

// fiveKinds builds the five event kinds afresh, so two calls give two equal, unshared sets.
func fiveKinds() []events.Event {
	return []events.Event{ev(1, kindProgress), clarification(2, "Which card?", "clr_AB12"), confirmationEv(3), ev(4, kindAnswer), errorEv(5)}
}

func TestTheRouteDoesNotMutateThePayloadsTheFeedReturned(t *testing.T) { // REQ-API-001, INV-12
	fed, want := fiveKinds(), fiveKinds()
	f := &sharedFeed{batch: events.Batch{Events: fed, Done: true}}
	body := newRigShared(t, f).get(path, edgetest.Tenant, session.RoleCustomer, nil)
	if body.Code != http.StatusOK || body.Body.Len() == 0 {
		t.Fatalf("%d %q", body.Code, body.Body.String())
	}
	for i := range fed {
		if !reflect.DeepEqual(fed[i], want[i]) {
			t.Errorf("event %d: the Feed's payload was changed by the route", i+1)
		}
	}
}

func TestEveryKindIsStillEncodedWithItsOwnEventAndSchemaVersion(t *testing.T) { // REQ-API-001
	f := &sharedFeed{batch: events.Batch{Events: fiveKinds(), Done: true}}
	got := newRigShared(t, f).get(path, edgetest.Tenant, session.RoleCustomer, nil).Body.String()
	for _, k := range []string{"progress", "clarification", "confirmation", "answer", "error"} {
		if want := "event: " + k + "\ndata: {\"event\":\"" + k + "\",\"schema_version\":\"1.0\""; !strings.Contains(got, want) {
			t.Errorf("%s: %q", k, got)
		}
	}
}

func TestTwoSubscribersOfOneFeedOwnedPayloadAreRaceFree(t *testing.T) { // REQ-API-001, run under -race
	f := &sharedFeed{batch: events.Batch{Events: fiveKinds(), Done: true}}
	r := newRigShared(t, f)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if rec := r.get(path, edgetest.Tenant, session.RoleCustomer, nil); rec.Code != http.StatusOK {
					t.Errorf("%d", rec.Code)
					return
				}
			}
		}()
	}
	wg.Wait()
}
