package intakesources_test

// Proves A345 S3 and A438 (the clock anchor is read from the sealed snapshot's complaint list, field effective_at; absence is NOT_STARTED, and only from a list marked complete), INV-19 (an
// anchor that cannot be told is an error, never "has not happened"), REQ-TRU-006 (a clock is never assumed started or not started), REQ-SEC-004 and INV-11 (the tenant's own login),
// AGENT-13. Real PostgreSQL and the real mapping of the shipped pack.

import (
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

const anchorEvent = "complaint_logged"

func TestAnchorIsTheEarliestEffectiveAtOfTheCompleteComplaintList(t *testing.T) {
	w := newWorld(t)
	w.seal(t, tenantA, snapID, caseItem("evr_1", "KNOWN", page(true)),
		complaintItem("evr_2", "KNOWN", page(true, complaintRow("2026-11-12T09:00:00Z"), complaintRow("2026-11-10T09:00:00Z"), complaintRow("2026-11-11T09:00:00Z"))))
	at, happened, err := anchorsOf(t, w).AnchorTime(ctx(t), tenantA, snapID, anchorEvent)
	if err != nil || !happened || at != unix(2026, 11, 10, 9) {
		t.Errorf("got %d, %v, %v; want the earliest, 2026-11-10T09:00:00Z, and happened", at, happened, err)
	}
}

func TestAnchorOfOneComplaintIsItsEffectiveAtInWholeSeconds(t *testing.T) {
	w := newWorld(t)
	w.seal(t, tenantA, snapID, complaintItem("evr_2", "KNOWN", page(true, complaintRow("2026-11-10T10:30:15+01:00"))))
	at, happened, err := anchorsOf(t, w).AnchorTime(ctx(t), tenantA, snapID, anchorEvent)
	if err != nil || !happened || at != unix(2026, 11, 10, 9)+30*60+15 {
		t.Errorf("got %d, %v, %v", at, happened, err)
	}
}

func TestAnchorOfACompleteEmptyComplaintListIsNotStarted(t *testing.T) {
	w := newWorld(t)
	w.seal(t, tenantA, snapID, complaintItem("evr_2", "KNOWN", page(true)))
	at, happened, err := anchorsOf(t, w).AnchorTime(ctx(t), tenantA, snapID, anchorEvent)
	if err != nil || happened || at != 0 {
		t.Errorf("got %d, %v, %v; want NOT_STARTED: no time, not happened, no error (A345 S3)", at, happened, err)
	}
}

func TestAnchorThatCannotBeToldIsAnErrorAndNeverNotStarted(t *testing.T) {
	missingItem := seed{id: "evr_1", source: caseSource, scope: caseScopeOf + "TX88213", status: "KNOWN", payload: page(true)}
	cases := map[string]struct {
		items []seed
		is    error
	}{
		"no complaint item":              {[]seed{missingItem}, intakesources.ErrListItem},
		"marker false, empty":            {[]seed{complaintItem("evr_2", "KNOWN", page(false))}, mapping.ErrIncomplete},
		"marker false, rows":             {[]seed{complaintItem("evr_2", "KNOWN", page(false, complaintRow("2026-11-10T09:00:00Z")))}, mapping.ErrIncomplete},
		"no marker, empty":               {[]seed{complaintItem("evr_2", "KNOWN", map[string]any{"items": []any{}})}, mapping.ErrIncomplete},
		"misshapen list":                 {[]seed{complaintItem("evr_2", "KNOWN", map[string]any{"items": "none", "complete": true})}, mapping.ErrListShape},
		"item STALE":                     {[]seed{complaintItem("evr_2", "STALE", page(true))}, intakesources.ErrListItem},
		"item UNAVAILABLE":               {[]seed{complaintItem("evr_2", "UNAVAILABLE", page(true, complaintRow("2026-11-10T09:00:00Z")))}, intakesources.ErrListItem},
		"item UNKNOWN":                   {[]seed{complaintItem("evr_2", "UNKNOWN", page(true))}, intakesources.ErrListItem},
		"two complaint items":            {[]seed{complaintItem("evr_2", "KNOWN", page(true)), complaintItem("evr_3", "KNOWN", page(true))}, intakesources.ErrListItem},
		"a row with no effective_at":     {[]seed{complaintItem("evr_2", "KNOWN", page(true, complaintRow("2026-11-10T09:00:00Z"), map[string]any{"complaint_ref": "cmp_2"}))}, intakesources.ErrBadAnswer},
		"a row with an instant, no zone": {[]seed{complaintItem("evr_2", "KNOWN", page(true, complaintRow("2026-11-10T09:00:00")))}, intakesources.ErrBadAnswer},
		"a row with a text that is none": {[]seed{complaintItem("evr_2", "KNOWN", page(true, complaintRow("soon")))}, intakesources.ErrBadAnswer},
		"an instant before second one":   {[]seed{complaintItem("evr_2", "KNOWN", page(true, complaintRow("1970-01-01T00:00:00Z")))}, intakesources.ErrBadAnswer},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.seal(t, tenantA, snapID, c.items...)
			at, happened, err := anchorsOf(t, w).AnchorTime(ctx(t), tenantA, snapID, anchorEvent)
			if !errors.Is(err, c.is) || happened || at != 0 {
				t.Errorf("got %d, %v, %v; want an error that is %v, no time and happened=false only as the zero value of an error", at, happened, err, c.is)
			}
		})
	}
}

func TestAnchorAnUnreadableOrForeignPayloadOrSnapshotIsAnError(t *testing.T) {
	w := newWorld(t)
	it := complaintItem("evr_2", "KNOWN", page(true, complaintRow("2026-11-10T09:00:00Z")))
	it.hash = "sha256:" + fillHex('b')
	w.seal(t, tenantA, snapID, it)
	if _, happened, err := anchorsOf(t, w).AnchorTime(ctx(t), tenantA, snapID, anchorEvent); !errors.Is(err, intakesources.ErrListPayload) || happened {
		t.Errorf("a payload that is not the sealed one: %v, %v", err, happened)
	}
	w.seal(t, tenantB, "evs_b1", complaintItem("evr_9", "KNOWN", page(true, complaintRow("2026-11-10T09:00:00Z"))))
	for name, logins := range map[string]intakesources.Logins{"tenant A's own login": w, "a login that sees every row": superuser{w}} {
		_, foreign, ferr := anchorsOf(t, logins).AnchorTime(ctx(t), tenantA, "evs_b1", anchorEvent)
		_, _, aerr := anchorsOf(t, logins).AnchorTime(ctx(t), tenantA, "evs_zz9", anchorEvent)
		if !errors.Is(ferr, intakesources.ErrNoSnapshot) || foreign || !errors.Is(aerr, intakesources.ErrNoSnapshot) || ferr.Error() != aerr.Error() {
			t.Errorf("%s: a foreign snapshot reads as an absent one: %v / %v", name, ferr, aerr)
		}
	}
	if at, happened, err := anchorsOf(t, w).AnchorTime(ctx(t), tenantB, "evs_b1", anchorEvent); err != nil || !happened || at != unix(2026, 11, 10, 9) {
		t.Errorf("the owner reads its own: %d, %v, %v", at, happened, err)
	}
}

func TestAnchorAnUnsealedSnapshotIsNotFound(t *testing.T) {
	w := newWorld(t)
	w.store(t, tenantA, []seed{complaintItem("evr_2", "KNOWN", page(true, complaintRow("2026-11-10T09:00:00Z")))})
	if _, happened, err := anchorsOf(t, w).AnchorTime(ctx(t), tenantA, snapID, anchorEvent); !errors.Is(err, intakesources.ErrNoSnapshot) || happened {
		t.Errorf("got %v, %v", err, happened)
	}
}

func TestAnchorOtherEventsAreRefusedByNameAndNothingIsRead(t *testing.T) {
	w := newWorld(t)
	w.seal(t, tenantA, snapID, complaintItem("evr_2", "KNOWN", page(true, complaintRow("2026-11-10T09:00:00Z"))))
	a := anchorsOf(t, w)
	for _, ev := range []string{"case_closed", "Complaint_Logged", "complaint_logged ", "", "case"} {
		before := w.calls
		at, happened, err := a.AnchorTime(ctx(t), tenantA, snapID, ev)
		if !errors.Is(err, intakesources.ErrNoSource) || !strings.Contains(err.Error(), ev) || happened || at != 0 || w.calls != before {
			t.Errorf("%q: got %d, %v, %v (logins asked %d times); want ErrNoSource naming it and nothing read", ev, at, happened, err, w.calls-before)
		}
	}
}

func TestAnchorQueryOutsideItsShapeIsRefusedBeforeAnyLogin(t *testing.T) {
	w := newWorld(t)
	a := anchorsOf(t, w)
	for name, args := range map[string][2]string{"no tenant": {"", snapID}, "bad tenant": {"tenant_a", snapID}, "no snapshot": {tenantA, ""}, "bad snapshot": {tenantA, "snap_1"}} {
		if _, happened, err := a.AnchorTime(ctx(t), args[0], args[1], anchorEvent); !errors.Is(err, intakesources.ErrQuery) || happened {
			t.Errorf("%s: %v, %v; want ErrQuery", name, err, happened)
		}
	}
	if w.calls != 0 {
		t.Errorf("a login was asked for %d times for queries that are refused", w.calls)
	}
}

func TestNewAnchorsRefusesAConfigurationThatIsNotUsable(t *testing.T) {
	w := newWorld(t)
	good := listsOf(t, w)
	ok := map[string]intakesources.AnchorEvent{anchorEvent: {Entity: "complaint_ref", Field: "effective_at"}}
	for name, c := range map[string]struct {
		lists  intakesources.EvidenceLists
		events map[string]intakesources.AnchorEvent
	}{
		"no events":                {good, nil},
		"an event with no name":    {good, map[string]intakesources.AnchorEvent{"": {Entity: "complaint_ref", Field: "effective_at"}}},
		"an event of no entity":    {good, map[string]intakesources.AnchorEvent{anchorEvent: {Field: "effective_at"}}},
		"an event of no field":     {good, map[string]intakesources.AnchorEvent{anchorEvent: {Entity: "complaint_ref"}}},
		"an entity the lists lack": {good, map[string]intakesources.AnchorEvent{anchorEvent: {Entity: "attachment", Field: "effective_at"}}},
		"lists with no mapping":    {intakesources.EvidenceLists{Items: good.Items, Payloads: good.Payloads, Entities: good.Entities}, ok},
	} {
		if _, err := intakesources.NewAnchors(c.lists, c.events); !errors.Is(err, intakesources.ErrConfig) {
			t.Errorf("%s: %v; want ErrConfig", name, err)
		}
	}
	var none *intakesources.Anchors
	if _, _, err := none.AnchorTime(ctx(t), tenantA, snapID, anchorEvent); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("a nil reader: %v", err)
	}
}
