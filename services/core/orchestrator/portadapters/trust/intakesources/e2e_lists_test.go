package intakesources_test

// The list readers behind the REAL Policies and Clocks adapters of trust/intakeadapters, the shipped fintech pack, the real engine, the real evidence mapping and a real PostgreSQL with a
// sealed snapshot, each tenant read as its own login. Proves REQ-TRU-004, REQ-TRU-005, REQ-TRU-006, A345 S1 and S3, A438, INV-16, INV-19: a rule that reads the case rows is evaluated
// over the sealed snapshot's case list (a complete list that has the case rejects, a complete list that has none permits) and is an error, never a permit built on an empty list, when
// the list cannot be known; the complaint clock is anchored on the sealed complaint list, and NOT_STARTED only from a complete empty one.

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
	"github.com/Noemai-org/noema/services/core/trust/policy"
)

func policiesWithLists(t *testing.T, w *world) *intakeadapters.Policies {
	t.Helper()
	raw, err := readPack()
	if err != nil {
		t.Fatal(err)
	}
	book, err := intakeadapters.LoadPolicyBook(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := intakeadapters.NewPolicies(intakeadapters.PoliciesConfig{Book: book, Publications: intakeadapters.StorePublications{DBs: publications{w}},
		Admissions: admissions(t, w), Facts: factsWithLists(t, w)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func createCaseQuery() intake.PolicyQuery {
	q := listQuery(tenantA)
	q.IntentID = "int_lst1"
	q.CapabilityID = "fin.cap.create_escalation_case"
	return q
}

func TestPoliciesTheCaseDedupeRuleReadsTheSealedCaseList(t *testing.T) {
	for name, c := range map[string]struct {
		payload map[string]any
		effect  policy.Effect
		reason  string
	}{
		"an open case of the subject in the epoch": {page(true, caseRow("cas_1", subjectRef, "OPEN", 1)), policy.Reject, "fin.reason.open_case_exists"},
		"a routed case of the subject":             {page(true, caseRow("cas_1", subjectRef, "ROUTED", 1)), policy.Reject, "fin.reason.open_case_exists"},
		"a complete list with no case":             {page(true), policy.Permit, "fin.reason.permitted"},
		"a case of another subject":                {page(true, caseRow("cas_1", "fin.transaction:TX1", "OPEN", 1)), policy.Permit, "fin.reason.permitted"},
		"a case of the subject in another epoch":   {page(true, caseRow("cas_1", subjectRef, "OPEN", 2)), policy.Permit, "fin.reason.permitted"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			publish(t, w, tenantA, "3.1", admittedAt.Add(-3600e9))
			w.intent(t, tenantA, "int_lst1", 1, node, admittedAt, resolvedSlots)
			w.seal(t, tenantA, snapID, caseItem("evr_1", "KNOWN", c.payload))
			got, err := policiesWithLists(t, w).Evaluate(ctx(t), createCaseQuery())
			if err != nil || got.Effect != c.effect || got.ReasonCode != c.reason {
				t.Errorf("got %+v, %v; want %s %s", got, err, c.effect, c.reason)
			}
		})
	}
}

func TestPoliciesTheCaseDedupeRuleIsAnErrorWhenTheListCannotBeKnown(t *testing.T) {
	for name, c := range map[string]struct {
		item seed
		is   error
	}{
		"a list not marked complete": {caseItem("evr_1", "KNOWN", page(false)), intakesources.ErrList},
		"a stale item":               {caseItem("evr_1", "STALE", page(true)), intakesources.ErrListItem},
		"no case item":               {seed{id: "evr_1", source: ledgerSource, scope: "transaction:TX88213", status: "KNOWN", payload: map[string]any{"entries": []any{}}}, intakesources.ErrListItem},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			publish(t, w, tenantA, "3.1", admittedAt.Add(-3600e9))
			w.intent(t, tenantA, "int_lst1", 1, node, admittedAt, resolvedSlots)
			w.seal(t, tenantA, snapID, c.item)
			got, err := policiesWithLists(t, w).Evaluate(ctx(t), createCaseQuery())
			if !errors.Is(err, c.is) || got.Effect != "" || got.PermitMatched || len(got.Rules) != 0 {
				t.Errorf("got %+v, %v; want an error that is %v and no answer", got, err, c.is)
			}
		})
	}
}

func clocksWithAnchors(t *testing.T, w *world) *intakeadapters.Clocks {
	t.Helper()
	raw, err := readPack()
	if err != nil {
		t.Fatal(err)
	}
	book, err := intakeadapters.LoadClockBook(raw)
	if err != nil {
		t.Fatal(err)
	}
	c, err := intakeadapters.NewClocks(intakeadapters.ClocksConfig{Book: book, Anchors: anchorsOf(t, w), AtRisk: clocks.Ratio{Num: 2500, Den: 10000}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClocksTheComplaintClockIsAnchoredOnTheSealedComplaintList(t *testing.T) {
	q := intake.ClockQuery{TenantID: tenantA, IntentNode: node, SnapshotID: snapID}
	w := newWorld(t)
	w.seal(t, tenantA, snapID, complaintItem("evr_1", "KNOWN", page(true, complaintRow("2026-11-10T09:00:00Z"))))
	got, err := clocksWithAnchors(t, w).Applicable(ctx(t), q)
	if err != nil || len(got.Clocks) != 1 || got.Clocks[0].Anchor == nil || *got.Clocks[0].Anchor != unix(2026, 11, 10, 9) {
		t.Fatalf("a logged complaint: got %+v, %v", got, err)
	}

	w = newWorld(t)
	w.seal(t, tenantA, snapID, complaintItem("evr_1", "KNOWN", page(true)))
	got, err = clocksWithAnchors(t, w).Applicable(ctx(t), q)
	if err != nil || len(got.Clocks) != 1 || got.Clocks[0].Anchor != nil {
		t.Errorf("a complete list with no complaint is NOT_STARTED: got %+v, %v", got, err)
	}

	w = newWorld(t)
	w.seal(t, tenantA, snapID, complaintItem("evr_1", "KNOWN", page(false)))
	got, err = clocksWithAnchors(t, w).Applicable(ctx(t), q)
	if !errors.Is(err, intakesources.ErrList) || len(got.Clocks) != 0 {
		t.Errorf("a list not marked complete is an error, not NOT_STARTED: got %+v, %v", got, err)
	}
}
