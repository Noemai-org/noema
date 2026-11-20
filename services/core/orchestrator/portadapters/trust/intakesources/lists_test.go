package intakesources_test

// Proves A345 S1 and A438 (the case rows come from the sealed snapshot's list; "no such case" only from a list marked complete, otherwise UNKNOWN, never none), INV-19 (absence is never a
// negative fact: a missing, stale or unknown item, an incomplete or misshapen list, an unreadable or unsealed payload is an error and an empty answer, never an empty list), INV-17 (the
// snapshot is exactly what was sealed), REQ-SEC-004 and INV-11 (each read is the tenant's own login; the query names the tenant too), AGENT-13 (fixed texts, the cause kept), REQ-TRU-004,
// REQ-TRU-005. Real PostgreSQL, the real mapping of the shipped pack, two tenants.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

func listQuery(tenant string) intake.PolicyQuery {
	q := factQuery(tenant, "int_lst1", 1)
	q.SnapshotID = snapID
	return q
}

func entities(names ...string) intakeadapters.FactNeed {
	return intakeadapters.FactNeed{Variables: []string{}, Entities: names}
}

func noAnswer(t *testing.T, got intakeadapters.FactSet) {
	t.Helper()
	if len(got.Variables) != 0 || len(got.Entities) != 0 {
		t.Errorf("an error must come with no answer at all, got %+v", got)
	}
}

func TestFactsGiveTheCaseAndComplaintRowsOfTheSealedLists(t *testing.T) {
	w := newWorld(t)
	w.seal(t, tenantA, snapID,
		caseItem("evr_1", "KNOWN", page(true, caseRow("cas_1", subjectRef, "OPEN", 1), caseRow("cas_2", "fin.transaction:TX1", "ROUTED", 2))),
		complaintItem("evr_2", "KNOWN", page(true, complaintRow("2026-11-10T09:00:00Z"))))
	got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantA), entities("case", "complaint_ref"))
	if err != nil {
		t.Fatal(err)
	}
	wantCase := []predicate.Row{
		{"id": predicate.Ref("cas_1"), "subject": predicate.Ref(subjectRef), "status": predicate.Enum("OPEN"), "created_by_intent": predicate.Ref("int_e2e1"), "owner_queue": predicate.Ref("PAYMENTS_OPERATIONS"), "epoch": predicate.Int(1)},
		{"id": predicate.Ref("cas_2"), "subject": predicate.Ref("fin.transaction:TX1"), "status": predicate.Enum("ROUTED"), "created_by_intent": predicate.Ref("int_e2e1"), "owner_queue": predicate.Ref("PAYMENTS_OPERATIONS"), "epoch": predicate.Int(2)},
	}
	rows := got.Entities["case"]
	if len(got.Entities) != 2 || len(rows) != 2 {
		t.Fatalf("got %+v; want the case rows and the complaint rows and nothing else", got)
	}
	for i, want := range wantCase {
		for field, v := range want {
			if rows[i][field] != v {
				t.Errorf("case row %d field %s: got %+v, want %+v", i, field, rows[i][field], v)
			}
		}
	}
	if c := got.Entities["complaint_ref"]; len(c) != 1 || c[0]["effective_at"] != predicate.Time(unix(2026, 11, 10, 9)) {
		t.Errorf("complaint rows: %+v", c)
	}
}

func TestFactsGiveVariablesAndEntitiesTogether(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantA, "int_lst1", 1, node, admittedAt, resolvedSlots)
	w.seal(t, tenantA, snapID, caseItem("evr_1", "KNOWN", page(true, caseRow("cas_1", subjectRef, "OPEN", 1))))
	need := intakeadapters.FactNeed{Variables: []string{"logical_resolution_epoch", "subject_resource"}, Entities: []string{"case"}}
	got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantA), need)
	if err != nil || got.Variables["subject_resource"] != predicate.Ref(subjectRef) || got.Variables["logical_resolution_epoch"] != predicate.Int(1) || len(got.Entities["case"]) != 1 {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestFactsACompleteEmptyListIsNoSuchCaseAndOnlyThat(t *testing.T) {
	w := newWorld(t)
	w.seal(t, tenantA, snapID, caseItem("evr_1", "KNOWN", page(true)), complaintItem("evr_2", "KNOWN", page(true)))
	got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantA), entities("case", "complaint_ref"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"case", "complaint_ref"} {
		rows, present := got.Entities[name]
		if !present || rows == nil || len(rows) != 0 {
			t.Errorf("%s: a complete empty list is an entity read with no row; got present=%v rows=%#v", name, present, rows)
		}
	}
}

func TestFactsAListNotMarkedCompleteIsUnknownNeverNone(t *testing.T) {
	for name, payload := range map[string]map[string]any{
		"empty, marker false":      page(false),
		"rows, marker false":       page(false, caseRow("cas_1", subjectRef, "OPEN", 1)),
		"empty, no marker":         {"items": []any{}},
		"rows, no marker":          {"items": []any{caseRow("cas_1", subjectRef, "OPEN", 1)}},
		"marker is a string":       page("true"),
		"marker is a number":       page(1),
		"marker is null":           page(nil),
		"empty, marker not a bool": page([]any{true}),
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.seal(t, tenantA, snapID, caseItem("evr_1", "KNOWN", payload))
			got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantA), entities("case"))
			if !errors.Is(err, intakesources.ErrList) || !errors.Is(err, mapping.ErrIncomplete) {
				t.Errorf("err = %v; want ErrList with the mapping's ErrIncomplete behind it", err)
			}
			noAnswer(t, got)
		})
	}
}

func TestFactsAPayloadThatIsNotTheNamedListIsRefused(t *testing.T) {
	for name, payload := range map[string]map[string]any{
		"items is not a list":      {"items": "none", "complete": true},
		"items is an object":       {"items": map[string]any{}, "complete": true},
		"no items member":          {"complete": true},
		"an element not an object": {"items": []any{"cas_1"}, "complete": true},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.seal(t, tenantA, snapID, caseItem("evr_1", "KNOWN", payload))
			got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantA), entities("case"))
			if !errors.Is(err, intakesources.ErrList) || !errors.Is(err, mapping.ErrListShape) {
				t.Errorf("err = %v; want ErrList with ErrListShape behind it", err)
			}
			noAnswer(t, got)
		})
	}
}

func TestFactsAMissingItemIsAnErrorAndNotNoCase(t *testing.T) {
	w := newWorld(t)
	other := seed{id: "evr_1", source: ledgerSource, scope: "transaction:TX88213", status: "KNOWN", payload: map[string]any{"entries": []any{}}}
	wrongScope := seed{id: "evr_2", source: caseSource, scope: "attachment:TX88213", status: "KNOWN", payload: page(true)}
	wrongSource := seed{id: "evr_3", source: "fin.src.switch", scope: caseScopeOf + "TX88213", status: "KNOWN", payload: page(true)}
	w.seal(t, tenantA, snapID, other, wrongScope, wrongSource)
	got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantA), entities("case"))
	if !errors.Is(err, intakesources.ErrListItem) {
		t.Errorf("err = %v; want ErrListItem", err)
	}
	noAnswer(t, got)
}

func TestFactsTheItemMustBeKnown(t *testing.T) {
	for _, status := range []string{"STALE", "UNAVAILABLE", "UNKNOWN", "INFERRED", "AMBIGUOUS", "CONTRADICTORY", "UNAUTHORIZED"} {
		for name, payload := range map[string]map[string]any{"complete and empty": page(true), "complete with a row": page(true, caseRow("cas_1", subjectRef, "OPEN", 1))} {
			t.Run(status+" "+name, func(t *testing.T) {
				w := newWorld(t)
				w.seal(t, tenantA, snapID, caseItem("evr_1", status, payload))
				got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantA), entities("case"))
				if !errors.Is(err, intakesources.ErrListItem) {
					t.Errorf("err = %v; want ErrListItem", err)
				}
				noAnswer(t, got)
			})
		}
	}
}

func TestFactsTwoItemsForTheOneListAreAmbiguousAndRefused(t *testing.T) {
	w := newWorld(t)
	w.seal(t, tenantA, snapID, caseItem("evr_1", "KNOWN", page(true)), caseItem("evr_2", "KNOWN", page(true, caseRow("cas_1", subjectRef, "OPEN", 1))))
	got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantA), entities("case"))
	if !errors.Is(err, intakesources.ErrListItem) {
		t.Errorf("err = %v; want ErrListItem", err)
	}
	noAnswer(t, got)
}

func TestFactsAnotherTenantsSnapshotIsNotFoundAndNotRead(t *testing.T) {
	w := newWorld(t)
	w.seal(t, tenantB, snapID, caseItem("evr_1", "KNOWN", page(true, caseRow("cas_1", subjectRef, "OPEN", 1))))
	for name, logins := range map[string]intakesources.Logins{"tenant A's own login": w, "a login that sees every row": superuser{w}} {
		f := factsWithLists(t, logins)
		got, foreign := f.PolicyFacts(ctx(t), listQuery(tenantA), entities("case"))
		_, absent := f.PolicyFacts(ctx(t), func() intake.PolicyQuery { q := listQuery(tenantA); q.SnapshotID = "evs_zz9"; return q }(), entities("case"))
		if !errors.Is(foreign, intakesources.ErrNoSnapshot) || !errors.Is(absent, intakesources.ErrNoSnapshot) || foreign.Error() != absent.Error() {
			t.Errorf("%s: a foreign snapshot reads as an absent one: %v / %v", name, foreign, absent)
		}
		noAnswer(t, got)
	}
	got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantB), entities("case"))
	if err != nil || len(got.Entities["case"]) != 1 {
		t.Errorf("the owner reads its own: %+v, %v", got, err)
	}
}

func TestFactsAnUnsealedSnapshotIsNotFound(t *testing.T) {
	w := newWorld(t)
	w.store(t, tenantA, []seed{caseItem("evr_1", "KNOWN", page(true))}) // items and payload stored, no snapshot sealed over them
	got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantA), entities("case"))
	if !errors.Is(err, intakesources.ErrNoSnapshot) {
		t.Errorf("err = %v; want ErrNoSnapshot", err)
	}
	noAnswer(t, got)
}

func TestFactsAPayloadThatIsNotTheSealedOneIsRefused(t *testing.T) {
	w := newWorld(t)
	it := caseItem("evr_1", "KNOWN", page(true))
	it.hash = "sha256:" + fillHex('b') // the sealed hash is not the hash of the stored payload: the stored payload is not the sealed one
	w.seal(t, tenantA, snapID, it)
	got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantA), entities("case"))
	if !errors.Is(err, intakesources.ErrListPayload) || !errors.Is(err, payloadread.ErrHashMismatch) {
		t.Errorf("err = %v; want ErrListPayload with ErrHashMismatch behind it", err)
	}
	noAnswer(t, got)
}

func TestFactsAPayloadThatCannotBeReadIsRefused(t *testing.T) {
	w := newWorld(t)
	w.seal(t, tenantA, snapID, caseItem("evr_1", "KNOWN", page(true)))
	boom := errors.New("connection refused by db-host-17.internal")
	l := listsOf(t, w)
	l.Payloads = failingPayloads{boom}
	f, err := intakesources.NewFacts(w, readTimeout, intakesources.WithLists(l))
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.PolicyFacts(ctx(t), listQuery(tenantA), entities("case"))
	if !errors.Is(err, intakesources.ErrListPayload) || !errors.Is(err, boom) {
		t.Errorf("err = %v; want ErrListPayload with the cause behind it", err)
	}
	noAnswer(t, got)
}

type failingPayloads struct{ err error }

func (f failingPayloads) Read(context.Context, string, string) ([]byte, error) { return nil, f.err }

func TestFactsASnapshotThatIsNotWhatWasSealedIsRefused(t *testing.T) {
	for name, tamper := range map[string]string{
		"a payload hash is changed": `UPDATE evidence_item SET payload_hash = 'sha256:` + fillHex('c') + `' WHERE evidence_id = 'evr_1'`,
		"an item is deleted":        `DELETE FROM evidence_item WHERE evidence_id = 'evr_2'`,
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.seal(t, tenantA, snapID, caseItem("evr_1", "KNOWN", page(true)), complaintItem("evr_2", "KNOWN", page(true)))
			w.tamper(t, tamper)
			got, err := factsWithLists(t, w).PolicyFacts(ctx(t), listQuery(tenantA), entities("case"))
			if !errors.Is(err, intakesources.ErrSnapshotIntegrity) {
				t.Errorf("err = %v; want ErrSnapshotIntegrity", err)
			}
			noAnswer(t, got)
		})
	}
}

func TestFactsEntitiesOtherThanTheListsAreRefusedByNameAndNothingIsRead(t *testing.T) {
	w := newWorld(t)
	w.seal(t, tenantA, snapID, caseItem("evr_1", "KNOWN", page(true)))
	f := factsWithLists(t, w)
	for _, name := range []string{"transaction", "beneficiary", "account", "switch_event", "settlement_record", "attachment", "Case", ""} {
		before := w.calls
		got, err := f.PolicyFacts(ctx(t), listQuery(tenantA), entities("case", name))
		if !errors.Is(err, intakesources.ErrNoSource) || !strings.Contains(err.Error(), name) || w.calls != before {
			t.Errorf("%q: got %+v, %v (logins asked %d times); want ErrNoSource naming it and nothing read", name, got, err, w.calls-before)
		}
		noAnswer(t, got)
	}
}

func TestFactsWithoutListsRefusesEveryEntityByName(t *testing.T) {
	w := newWorld(t)
	got, err := facts(t, w).PolicyFacts(ctx(t), listQuery(tenantA), entities("case"))
	if !errors.Is(err, intakesources.ErrNoSource) || !strings.Contains(err.Error(), "case") || w.calls != 0 {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestFactsAQueryWithoutASealedSnapshotIDIsRefusedBeforeAnyLogin(t *testing.T) {
	w := newWorld(t)
	f := factsWithLists(t, w)
	for _, id := range []string{"", "snap_1", "evs_", "evs_1;DROP", "EVS_1"} {
		q := listQuery(tenantA)
		q.SnapshotID = id
		got, err := f.PolicyFacts(ctx(t), q, entities("case"))
		if !errors.Is(err, intakesources.ErrQuery) {
			t.Errorf("%q: got %+v, %v; want ErrQuery", id, got, err)
		}
	}
	if w.calls != 0 {
		t.Errorf("a login was asked for %d times for queries that are refused", w.calls)
	}
}

func TestNewFactsRefusesListsThatAreNotUsable(t *testing.T) {
	w := newWorld(t)
	good := listsOf(t, w)
	for name, mutate := range map[string]func(*intakesources.EvidenceLists){
		"no mapping":         func(l *intakesources.EvidenceLists) { l.Mapping = nil },
		"no snapshot reader": func(l *intakesources.EvidenceLists) { l.Items = nil },
		"no payload reader":  func(l *intakesources.EvidenceLists) { l.Payloads = nil },
		"no entity":          func(l *intakesources.EvidenceLists) { l.Entities = nil },
		"an entity with no name": func(l *intakesources.EvidenceLists) {
			l.Entities = map[string]intakesources.ListSpec{"": {Source: caseSource, ScopePrefix: caseScopeOf}}
		},
		"an entity with no source": func(l *intakesources.EvidenceLists) {
			l.Entities = map[string]intakesources.ListSpec{"case": {ScopePrefix: caseScopeOf}}
		},
		"an entity with no scope prefix": func(l *intakesources.EvidenceLists) {
			l.Entities = map[string]intakesources.ListSpec{"case": {Source: caseSource}}
		},
		"two entities that one item could hold": func(l *intakesources.EvidenceLists) {
			l.Entities = map[string]intakesources.ListSpec{"case": {Source: caseSource, ScopePrefix: "c"}, "complaint_ref": {Source: caseSource, ScopePrefix: "complaint_ref:"}}
		},
	} {
		l := good
		mutate(&l)
		if _, err := intakesources.NewFacts(w, readTimeout, intakesources.WithLists(l)); !errors.Is(err, intakesources.ErrConfig) {
			t.Errorf("%s: %v; want ErrConfig", name, err)
		}
	}
}
