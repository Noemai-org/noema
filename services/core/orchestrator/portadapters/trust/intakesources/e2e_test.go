package intakesources_test

// The readers behind the real Policies adapter of trust/intakeadapters, the shipped fintech pack, the real engine and a real PostgreSQL, each tenant read as its own Trust login.
// Proves REQ-TRU-004, REQ-TRU-005, REQ-TRU-007, A306, INV-09, INV-16, INV-19: a write capability whose rules read only facts of the stored intent is evaluated; one whose rules read
// a case row, an epoch or a case reference is an error (UNKNOWN at the intake), never a REJECT or a PERMIT built on an empty set.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
	"github.com/Noemai-org/noema/services/core/trust/policy"
)

const packPath = "../../../../../../packs/fintech_v1.json"

func readPack() ([]byte, error) { return os.ReadFile(packPath) }

func publish(t *testing.T, w *world, tenant, version string, at time.Time) {
	t.Helper()
	if _, err := w.trust[tenant].Exec(context.Background(), `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by, published_at)
		VALUES ($1, 'fin.pol', $2, policy_version_key($2), 'adm_synth01', $3)`, tenant, version, at); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func policies(t *testing.T, w *world) *intakeadapters.Policies {
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
		Admissions: admissions(t, w), Facts: facts(t, w)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPoliciesEvaluateAWriteWhoseRulesReadOnlyTheStatusOfTheStoredIntent(t *testing.T) {
	w := newWorld(t)
	publish(t, w, tenantA, "3.1", admittedAt.Add(-time.Hour))
	w.intent(t, tenantA, "int_e2e1", 1, node, admittedAt, resolvedSlots)
	w.intent(t, tenantA, "int_e2e2", 1, node, admittedAt, `{"subject_resource":{"type":"Resource","status":"UNRESOLVED"}}`)
	p := policies(t, w)

	got, err := p.Evaluate(ctx(t), factQuery(tenantA, "int_e2e1", 1))
	if err != nil || got.Effect != policy.Permit || !got.PermitMatched || len(got.Versions) != 1 || got.Versions[0].Version != "3.1" {
		t.Errorf("a resolved subject: got %+v, %v; want PERMIT under fin.pol@3.1", got, err)
	}
	got, err = p.Evaluate(ctx(t), factQuery(tenantA, "int_e2e2", 1))
	if err != nil || got.Effect != policy.Reject || got.ReasonCode != "NO_PERMITTING_RULE" || got.PermitMatched {
		t.Errorf("an unresolved subject: got %+v, %v; want REJECT NO_PERMITTING_RULE", got, err)
	}
}

func TestPoliciesAReadCapabilityReadsNoFactAtAll(t *testing.T) {
	w := newWorld(t)
	publish(t, w, tenantA, "3.1", admittedAt.Add(-time.Hour))
	w.intent(t, tenantA, "int_e2e3", 1, node, admittedAt, `{"subject_resource":{"type":"Resource","status":"UNRESOLVED"}}`)
	q := factQuery(tenantA, "int_e2e3", 1)
	q.CapabilityID = "fin.cap.lookup_transaction"
	got, err := policies(t, w).Evaluate(ctx(t), q)
	if err != nil || got.Effect != policy.Permit {
		t.Errorf("got %+v, %v; want PERMIT", got, err)
	}
}

func TestPoliciesAWriteThatReadsACaseRowAnEpochOrACaseReferenceIsAnErrorNotAnAnswer(t *testing.T) {
	w := newWorld(t)
	publish(t, w, tenantA, "3.1", admittedAt.Add(-time.Hour))
	w.intent(t, tenantA, "int_e2e4", 1, node, admittedAt, resolvedSlots)
	p := policies(t, w)
	for _, capability := range []string{"fin.cap.create_escalation_case", "fin.cap.route_to_internal_owner"} {
		q := factQuery(tenantA, "int_e2e4", 1)
		q.CapabilityID = capability
		got, err := p.Evaluate(ctx(t), q)
		if !errors.Is(err, intakesources.ErrNoSource) || got.Effect != "" || got.PermitMatched || len(got.Rules) != 0 {
			t.Errorf("%s: got %+v, %v; want ErrNoSource and no answer", capability, got, err)
		}
	}
}

func TestPoliciesTheAdmissionTimeOfTheReaderIsTheOneTheAdapterChecksAgainstTheReading(t *testing.T) {
	w := newWorld(t)
	publish(t, w, tenantA, "3.1", admittedAt.Add(-time.Hour))
	w.intent(t, tenantA, "int_e2e5", 1, node, admittedAt, resolvedSlots)
	q := factQuery(tenantA, "int_e2e5", 1)
	q.Now = admittedAt.Unix() - 1 // a reading before the intent was admitted is not a reading of it
	if got, err := policies(t, w).Evaluate(ctx(t), q); !errors.Is(err, intakeadapters.ErrQuery) || got.Effect != "" {
		t.Errorf("got %+v, %v; want ErrQuery", got, err)
	}
	q.Now = admittedAt.Unix()
	if got, err := policies(t, w).Evaluate(ctx(t), q); err != nil || got.Effect != policy.Permit {
		t.Errorf("a reading at the admission second: got %+v, %v", got, err)
	}
}

func TestPoliciesTenantBCannotBeEvaluatedOnTenantAsIntent(t *testing.T) {
	w := newWorld(t)
	publish(t, w, tenantB, "3.1", admittedAt.Add(-time.Hour))
	w.intent(t, tenantA, "int_e2e6", 1, node, admittedAt, resolvedSlots)
	if got, err := policies(t, w).Evaluate(ctx(t), factQuery(tenantB, "int_e2e6", 1)); !errors.Is(err, intakesources.ErrNotFound) || got.Effect != "" {
		t.Errorf("got %+v, %v; want ErrNotFound", got, err)
	}
}
