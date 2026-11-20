package trustpg_test

import (
	"errors"
	"testing"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// REQ-TRU-009, INV-03: the decision as Trust recorded it, with the intent reference int_<id>@<version> found from the admitted intent whose hash the decision names.
func TestTheDecisionIsReadWithItsOutcomeAndItsIntentReference(t *testing.T) {
	w := newWorld(t)
	r, err := trustpg.NewRecords(w.pools, timeout)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Decision(ctx(t), tenantA, string(w.allowed.Record.DecisionId))
	if err != nil {
		t.Fatal(err)
	}
	want := trustport.DecisionRecord{ID: string(w.allowed.Record.DecisionId), Outcome: "ALLOW", IntentRef: intentID + "@2"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// A decision on an intent the store never admitted has no intent reference: the Binding cannot match it (INV-03, the safer reading).
func TestADecisionWhoseIntentWasNeverAdmittedHasNoIntentReference(t *testing.T) {
	w := newWorld(t)
	other := w.write(t, "int_01J9ZZ", hash(7))
	r, err := trustpg.NewRecords(w.pools, timeout)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Decision(ctx(t), tenantA, string(other.Record.DecisionId))
	if err != nil {
		t.Fatal(err)
	}
	if got.IntentRef != "" || got.Outcome != "ALLOW" {
		t.Fatalf("got %+v, want an ALLOW with no intent reference", got)
	}
}

// REQ-TRU-009, INV-12: the grant names its decision, the correlation and the principal.
func TestTheGrantIsReadWithItsDecisionCorrelationAndPrincipal(t *testing.T) {
	w := newWorld(t)
	r, err := trustpg.NewRecords(w.pools, timeout)
	if err != nil {
		t.Fatal(err)
	}
	gid := string(w.allowed.Grants[0].GrantId)
	got, err := r.Grant(ctx(t), tenantA, gid)
	if err != nil {
		t.Fatal(err)
	}
	want := trustport.GrantRecord{ID: gid, DecisionID: string(w.allowed.Record.DecisionId), CorrelationID: corr, PrincipalID: principal}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// REQ-SEC-004: another tenant's decision and grant are ErrNoRecord, the same answer as one that does not exist.
func TestAnotherTenantsRecordsAreNotFound(t *testing.T) {
	w := newWorld(t)
	r, err := trustpg.NewRecords(w.pools, timeout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Decision(ctx(t), tenantB, string(w.allowed.Record.DecisionId)); !errors.Is(err, trustport.ErrNoRecord) {
		t.Fatalf("decision of another tenant: %v, want ErrNoRecord", err)
	}
	if _, err := r.Grant(ctx(t), tenantB, string(w.allowed.Grants[0].GrantId)); !errors.Is(err, trustport.ErrNoRecord) {
		t.Fatalf("grant of another tenant: %v, want ErrNoRecord", err)
	}
	if _, err := r.Decision(ctx(t), tenantA, "dec_missing"); !errors.Is(err, trustport.ErrNoRecord) {
		t.Fatalf("missing decision: %v, want ErrNoRecord", err)
	}
	if _, err := r.Grant(ctx(t), tenantA, "grt_missing"); !errors.Is(err, trustport.ErrNoRecord) {
		t.Fatalf("missing grant: %v, want ErrNoRecord", err)
	}
}

// A tenant with no login is an error that is not ErrNoRecord: "could not tell" is not a refusal, and never another tenant's pool (INV-12).
func TestATenantWithNoLoginIsAnErrorAndNeverAnotherTenantsPool(t *testing.T) {
	w := newWorld(t)
	r, err := trustpg.NewRecords(w.pools, timeout)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Decision(ctx(t), "ten_zz", string(w.allowed.Record.DecisionId))
	if !errors.Is(err, trustpg.ErrNoPool) || errors.Is(err, trustport.ErrNoRecord) {
		t.Fatalf("got %v, want ErrNoPool and not ErrNoRecord", err)
	}
}

func TestIntentsReturnTheCorrelationTheStoredIntentNames(t *testing.T) {
	w := newWorld(t)
	in, err := trustpg.NewIntents(w.pools, timeout)
	if err != nil {
		t.Fatal(err)
	}
	a, err := in.Get(ctx(t), tenantA, intentID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if a.CorrelationID != corr || a.TenantID != tenantA || a.IntentID != intentID || a.Version != 2 || a.IntentHash != string(hash(1)) {
		t.Fatalf("got %+v", a)
	}
	if _, err := in.Get(ctx(t), tenantA, intentID, 3); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("missing version: %v, want intake.ErrNotFound", err)
	}
	if _, err := in.Get(ctx(t), tenantB, intentID, 2); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("another tenant's intent: %v, want intake.ErrNotFound", err)
	}
}

func TestConstructorsRefuseWhatCannotWork(t *testing.T) {
	w := newWorld(t)
	if _, err := trustpg.NewPools(nil); !errors.Is(err, trustpg.ErrConfig) {
		t.Errorf("no pools: %v", err)
	}
	if _, err := trustpg.NewRecords(w.pools, 0); !errors.Is(err, trustpg.ErrConfig) {
		t.Errorf("records with no timeout: %v", err)
	}
	if _, err := trustpg.NewIntents(w.pools, 0); !errors.Is(err, trustpg.ErrConfig) {
		t.Errorf("intents with no timeout: %v", err)
	}
	if _, err := trustpg.NewKeyDispatches(nil); !errors.Is(err, trustpg.ErrConfig) {
		t.Errorf("dispatches with no Record reader: %v", err)
	}
}
