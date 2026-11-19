package intakesources_test

// Proves REQ-TRU-004, REQ-TRU-005 and A306 (the facts the policy conditions read, from the stored intent of the query), REQ-SEC-004 and INV-11 (the tenant's own login), INV-19
// (a fact is given only when it was read: an evidence entity, an epoch or a case reference has no source here and is refused by name, never given as empty or null), INV-12
// (identity is never read), AGENT-13 (no row text), INV-14 (nothing here names an industry: a variable is sourced from the slot of the same name).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
)

var admittedAt = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func facts(t *testing.T, l intakesources.Logins) *intakesources.Facts {
	t.Helper()
	f, err := intakesources.NewFacts(l, readTimeout)
	if err != nil {
		t.Fatalf("NewFacts: %v", err)
	}
	return f
}

func factQuery(tenant, intent string, version int) intake.PolicyQuery {
	return intake.PolicyQuery{TenantID: tenant, IntentID: intent, IntentVersion: version, IntentNode: node, CapabilityID: "fin.cap.record_investigation_evidence", SnapshotID: "evs_1", Now: admittedAt.Unix() + 60}
}

func vars(names ...string) intakeadapters.FactNeed {
	return intakeadapters.FactNeed{Variables: names, Entities: []string{}}
}

func TestPolicyFactsGivesTheStatusOfTheSlotAndTheResolvedReference(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantA, "int_fac1", 1, node, admittedAt, resolvedSlots)
	got, err := facts(t, w).PolicyFacts(ctx(t), factQuery(tenantA, "int_fac1", 1), vars("subject_resource", "subject_resource.status"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]predicate.Value{"subject_resource": predicate.Ref("fin.transaction:TX88213"), "subject_resource.status": predicate.Enum("RESOLVED")}
	if len(got.Variables) != len(want) || len(got.Entities) != 0 {
		t.Fatalf("got %+v; want exactly %+v and no entity", got, want)
	}
	for name, v := range want {
		if got.Variables[name] != v {
			t.Errorf("%s: got %+v, want %+v", name, got.Variables[name], v)
		}
	}
}

func TestPolicyFactsGivesOnlyWhatWasAsked(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantA, "int_fac2", 1, node, admittedAt, resolvedSlots)
	got, err := facts(t, w).PolicyFacts(ctx(t), factQuery(tenantA, "int_fac2", 1), vars("subject_resource.status"))
	if err != nil || len(got.Variables) != 1 || got.Variables["subject_resource.status"] != predicate.Enum("RESOLVED") {
		t.Errorf("got %+v, %v", got, err)
	}
	if _, has := got.Variables["subject_resource"]; has {
		t.Error("a reference nobody asked for was given")
	}
}

func TestPolicyFactsNeedingNothingAsksNoLoginAndGivesAnEmptySet(t *testing.T) {
	w := newWorld(t)
	got, err := facts(t, w).PolicyFacts(ctx(t), factQuery(tenantA, "int_nothere", 1), intakeadapters.FactNeed{})
	if err != nil || got.Variables == nil || got.Entities == nil || len(got.Variables) != 0 || len(got.Entities) != 0 {
		t.Errorf("a read capability reads nothing: got %+v, %v", got, err)
	}
	if w.calls != 0 {
		t.Errorf("a login was asked for %d times to read nothing", w.calls)
	}
}

func TestPolicyFactsStatusIsTheStoredStatusWhateverItIs(t *testing.T) {
	w := newWorld(t)
	for i, status := range []string{"RESOLVED", "UNRESOLVED", "AMBIGUOUS", "ABSENT"} {
		id := []string{"int_st1", "int_st2", "int_st3", "int_st4"}[i]
		slots := `{"subject_resource":{"type":"Resource","status":"` + status + `"}}`
		if status == "RESOLVED" {
			slots = `{"subject_resource":{"type":"Resource","status":"RESOLVED","resolved_ref":"fin.transaction:TX1"}}`
		}
		w.intent(t, tenantA, id, 1, node, admittedAt, slots)
		got, err := facts(t, w).PolicyFacts(ctx(t), factQuery(tenantA, id, 1), vars("subject_resource.status"))
		if err != nil || got.Variables["subject_resource.status"] != predicate.Enum(status) {
			t.Errorf("%s: got %+v, %v", status, got, err)
		}
	}
}

func TestPolicyFactsAReferenceOfASlotThatIsNotResolvedIsRefusedButItsStatusIsGiven(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantA, "int_unr1", 1, node, admittedAt, `{"subject_resource":{"type":"Resource","status":"AMBIGUOUS","candidates_ref":null}}`)
	f := facts(t, w)
	if got, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_unr1", 1), vars("subject_resource.status")); err != nil || got.Variables["subject_resource.status"] != predicate.Enum("AMBIGUOUS") {
		t.Errorf("the status of an ambiguous slot is a fact: %+v, %v", got, err)
	}
	got, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_unr1", 1), vars("subject_resource", "subject_resource.status"))
	if !errors.Is(err, intakesources.ErrIntent) || len(got.Variables) != 0 {
		t.Errorf("a reference the slot does not carry is no fact, and no half answer: %+v, %v", got, err)
	}
}

func TestPolicyFactsReadsTheVersionAsked(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantA, "int_ver1", 1, node, admittedAt, `{"subject_resource":{"type":"Resource","status":"UNRESOLVED"}}`)
	w.intent(t, tenantA, "int_ver1", 2, node, admittedAt.Add(time.Minute), resolvedSlots)
	f := facts(t, w)
	for version, want := range map[int]string{1: "UNRESOLVED", 2: "RESOLVED"} {
		got, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_ver1", version), vars("subject_resource.status"))
		if err != nil || got.Variables["subject_resource.status"] != predicate.Enum(want) {
			t.Errorf("version %d: got %+v, %v; want %s", version, got, err, want)
		}
	}
	if _, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_ver1", 3), vars("subject_resource.status")); !errors.Is(err, intakesources.ErrNotFound) {
		t.Errorf("a version that does not exist: %v", err)
	}
}

func TestPolicyFactsOfAnotherTenantsIntentAreNotFoundAndNotRead(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantB, "int_oth1", 1, node, admittedAt, resolvedSlots)
	f := facts(t, w)
	_, foreign := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_oth1", 1), vars("subject_resource.status"))
	_, absent := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_nothere", 1), vars("subject_resource.status"))
	if !errors.Is(foreign, intakesources.ErrNotFound) || !errors.Is(absent, intakesources.ErrNotFound) || foreign.Error() != absent.Error() {
		t.Errorf("a foreign intent reads as an absent one: %v / %v", foreign, absent)
	}
	if got, err := f.PolicyFacts(ctx(t), factQuery(tenantB, "int_oth1", 1), vars("subject_resource.status")); err != nil || got.Variables["subject_resource.status"] != predicate.Enum("RESOLVED") {
		t.Errorf("the owner reads its own: %+v, %v", got, err)
	}
}

func TestPolicyFactsAnEvidenceEntityHasNoSourceAndNothingIsReadForIt(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantA, "int_ent1", 1, node, admittedAt, resolvedSlots)
	need := intakeadapters.FactNeed{Variables: []string{"subject_resource", "subject_resource.status"}, Entities: []string{"case"}}
	got, err := facts(t, w).PolicyFacts(ctx(t), factQuery(tenantA, "int_ent1", 1), need)
	if !errors.Is(err, intakesources.ErrNoSource) || !strings.Contains(err.Error(), "case") || len(got.Variables) != 0 || len(got.Entities) != 0 {
		t.Errorf("got %+v, %v; want ErrNoSource naming the entity and no half answer", got, err)
	}
	if w.calls != 0 {
		t.Errorf("a login was asked for %d times for a fact that has no source", w.calls)
	}
}

func TestPolicyFactsAVariableNoSlotCarriesHasNoSource(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantA, "int_nos1", 1, node, admittedAt, resolvedSlots)
	f := facts(t, w)
	for _, name := range []string{"logical_resolution_epoch", "case_id", "planned_queue", "evidence_packet_hash", "principal_id", "a.b.status", "Subject", "subject_resource.other", "other_slot.status"} {
		got, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_nos1", 1), vars("subject_resource.status", name))
		if !errors.Is(err, intakesources.ErrNoSource) || !strings.Contains(err.Error(), name) || len(got.Variables) != 0 {
			t.Errorf("%s: got %+v, %v; want ErrNoSource naming it and no half answer", name, got, err)
		}
	}
}

func TestPolicyFactsRefusesAStoredIntentThatCannotGiveTheFact(t *testing.T) {
	w := newWorld(t)
	for id, slots := range map[string]string{
		"int_bad1": `{"subject_resource":{"type":"Resource"}}`,
		"int_bad2": `{"subject_resource":{"type":"Resource","status":"BOGUS"}}`,
		"int_bad3": `{"subject_resource":{"type":"Resource","status":"AMBIGUOUS","resolved_ref":"fin.transaction:TX1"}}`,
		"int_bad4": `{"subject_resource":{"type":"Resource","status":"RESOLVED"}}`,
		"int_bad5": `{"subject_resource":{"type":"Resource","status":"RESOLVED","resolved_ref":""}}`,
		"int_bad6": `{"subject_resource":{"type":"Resource","status":7}}`,
		"int_bad7": `{"subject_resource":{"type":"Party","status":"RESOLVED","surface_form":"Tunde"}}`,
		"int_bad8": `{"subject_resource":"RESOLVED"}`,
	} {
		w.intent(t, tenantA, id, 1, node, admittedAt, slots)
		for _, need := range []intakeadapters.FactNeed{vars("subject_resource.status"), vars("subject_resource")} {
			got, err := facts(t, w).PolicyFacts(ctx(t), factQuery(tenantA, id, 1), need)
			if id == "int_bad7" && need.Variables[0] == "subject_resource.status" {
				if err != nil || got.Variables["subject_resource.status"] != predicate.Enum("RESOLVED") {
					t.Errorf("%s: a slot of another type with a status gives its status: %+v, %v", id, got, err)
				}
				continue
			}
			if !errors.Is(err, intakesources.ErrIntent) || len(got.Variables) != 0 {
				t.Errorf("%s %v: got %+v, %v; want ErrIntent", id, need.Variables, got, err)
			}
		}
	}
}

func TestPolicyFactsRefusesAnIntentNodeOtherThanTheQuerys(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantA, "int_nod1", 1, "fin.meta.out_of_scope", admittedAt, resolvedSlots)
	if got, err := facts(t, w).PolicyFacts(ctx(t), factQuery(tenantA, "int_nod1", 1), vars("subject_resource.status")); !errors.Is(err, intakesources.ErrIntent) || len(got.Variables) != 0 {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestPolicyFactsRefusesAQueryOutsideItsShapeBeforeAskingAnyLogin(t *testing.T) {
	w := newWorld(t)
	f := facts(t, w)
	good := factQuery(tenantA, "int_fac1", 1)
	for name, mutate := range map[string]func(q *intake.PolicyQuery){
		"no tenant": func(q *intake.PolicyQuery) { q.TenantID = "" }, "bad tenant": func(q *intake.PolicyQuery) { q.TenantID = "tenant_a" },
		"no intent": func(q *intake.PolicyQuery) { q.IntentID = "" }, "bad intent": func(q *intake.PolicyQuery) { q.IntentID = "intent_1" },
		"version zero": func(q *intake.PolicyQuery) { q.IntentVersion = 0 }, "no node": func(q *intake.PolicyQuery) { q.IntentNode = "" },
	} {
		q := good
		mutate(&q)
		if got, err := f.PolicyFacts(ctx(t), q, vars("subject_resource.status")); !errors.Is(err, intakesources.ErrQuery) || len(got.Variables) != 0 {
			t.Errorf("%s: got %+v, %v; want ErrQuery", name, got, err)
		}
	}
	if w.calls != 0 {
		t.Errorf("a login was asked for %d times for queries that are refused", w.calls)
	}
}

func TestPolicyFactsNeedsADeadlineAndAConfiguredReader(t *testing.T) {
	w := newWorld(t)
	if _, err := facts(t, w).PolicyFacts(context.Background(), factQuery(tenantA, "int_fac1", 1), vars("subject_resource.status")); !errors.Is(err, intakesources.ErrNoDeadline) {
		t.Errorf("no deadline: %v", err)
	}
	if _, err := intakesources.NewFacts(nil, readTimeout); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("no logins: %v", err)
	}
	if _, err := intakesources.NewFacts(w, 0); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("a timeout of zero: %v", err)
	}
	var none *intakesources.Facts
	if _, err := none.PolicyFacts(ctx(t), factQuery(tenantA, "int_fac1", 1), vars("subject_resource.status")); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("a nil reader: %v", err)
	}
}

func TestPolicyFactsHidesTheDriversTextAndKeepsTheCause(t *testing.T) {
	boom := errors.New("connection refused by db-host-17.internal as login trust_rw__ten_aa")
	f := facts(t, loginFunc(func(string) (intakesources.Querier, error) { return failingLogin{boom}, nil }))
	_, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_fac1", 1), vars("subject_resource.status"))
	if !errors.Is(err, intakesources.ErrStore) || !errors.Is(err, boom) || strings.Contains(err.Error(), "db-host-17") {
		t.Errorf("got %v; want ErrStore with the cause reachable and no driver text", err)
	}
	noLogin := loginFunc(func(string) (intakesources.Querier, error) { return nil, boom })
	if _, err := facts(t, noLogin).PolicyFacts(ctx(t), factQuery(tenantA, "int_fac1", 1), vars("subject_resource.status")); !errors.Is(err, intakesources.ErrStore) || strings.Contains(err.Error(), "db-host-17") {
		t.Errorf("a login that cannot be had: %v", err)
	}
}

func TestPolicyFactsIsBoundedByTheConfiguredTimeout(t *testing.T) {
	var seen time.Time
	f, err := intakesources.NewFacts(loginFunc(func(string) (intakesources.Querier, error) { return blockingLogin{&seen}, nil }), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = f.PolicyFacts(c, factQuery(tenantA, "int_fac1", 1), vars("subject_resource.status"))
	if !errors.Is(err, intakesources.ErrStore) || !errors.Is(err, context.DeadlineExceeded) || !boundedBy(c, seen) {
		t.Errorf("a read that hangs must end at the timeout, inside the caller's deadline: %v", err)
	}
}

func TestPolicyFactsTheQueryNamesTheTenantEvenWhenTheLoginDoesNotBindIt(t *testing.T) {
	w := newWorld(t)
	w.intent(t, tenantB, "int_oth2", 1, node, admittedAt, resolvedSlots)
	got, err := facts(t, superuser{w}).PolicyFacts(ctx(t), factQuery(tenantA, "int_oth2", 1), vars("subject_resource.status"))
	if !errors.Is(err, intakesources.ErrNotFound) || len(got.Variables) != 0 {
		t.Errorf("tenant A through a login that sees every row: got %+v, %v; want ErrNotFound", got, err)
	}
}

func TestPolicyFactsAVariableThatCannotNameASlotIsRefusedBeforeAnyLoginIsAsked(t *testing.T) {
	w := newWorld(t)
	f := facts(t, w)
	for _, name := range []string{"a.b.status", "Subject", "subject_resource.other", ".status", "", "9slot", "slot-name.status"} {
		if _, err := f.PolicyFacts(ctx(t), factQuery(tenantA, "int_fac1", 1), vars(name)); !errors.Is(err, intakesources.ErrNoSource) {
			t.Errorf("%q: %v", name, err)
		}
	}
	if w.calls != 0 {
		t.Errorf("a login was asked for %d times for variables that name no slot", w.calls)
	}
}

func TestPolicyFactsAZeroReaderIsNotConfigured(t *testing.T) {
	if _, err := new(intakesources.Facts).PolicyFacts(ctx(t), factQuery(tenantA, "int_fac1", 1), vars("subject_resource.status")); !errors.Is(err, intakesources.ErrConfig) {
		t.Errorf("a reader that was not built: %v", err)
	}
}
