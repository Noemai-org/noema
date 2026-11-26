package trustpg_test

// A696 (REQ-RCV-003, REQ-SEC-004; INV-03, INV-12): the recovery reader's Trust-side half. The ALLOW decision a correlation holds, with its grants and its intent reference, is read as
// the tenant's own trust_rw login under forced row-level security. Read-only; it decides nothing and never spends a grant. All data is SYNTHETIC (AGENT-25).

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

func grantIDs(r recorder.Result) []string {
	var out []string
	for _, g := range r.Grants {
		out = append(out, string(g.GrantId))
	}
	return out
}

func decisions(t *testing.T, w *world) *trustpg.Decisions {
	t.Helper()
	d, err := trustpg.NewDecisions(w.pools, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The ALLOW the Record holds for the correlation comes back with its decision id, its one grant and the intent reference of the admitted intent it was made on.
func TestTheAllowOfACorrelationIsReadWithItsGrantAndIntentReference(t *testing.T) {
	w := newWorld(t)
	got, err := decisions(t, w).AllowOf(ctx(t), tenantA, corr)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != string(w.allowed.Record.DecisionId) || got.Outcome != contracts.EnumsV1DecisionOutcomeALLOW || got.Read || got.IntentRef != intentID+"@2" {
		t.Fatalf("got %+v; want the ALLOW %s on %s@2", got, string(w.allowed.Record.DecisionId), intentID)
	}
	if len(got.GrantIDs) != 1 || got.GrantIDs[0] != string(w.allowed.Grants[0].GrantId) {
		t.Fatalf("grants %v; want %v", got.GrantIDs, grantIDs(w.allowed))
	}
}

// With two ALLOW decisions on one correlation the newest is the one in flight; the older stays on the Record.
func TestTheNewestAllowOfACorrelationIsTheOneRead(t *testing.T) {
	w := newWorld(t)
	second := w.write(t, intentID, hash(10)) // intent version 1: another input tuple, so another decision
	if string(second.Record.DecisionId) == string(w.allowed.Record.DecisionId) {
		t.Fatal("test premise: two decisions")
	}
	got, err := decisions(t, w).AllowOf(ctx(t), tenantA, corr)
	if err != nil || got.ID != string(second.Record.DecisionId) {
		t.Fatalf("got %+v, %v; want the newest, %s", got, err, string(second.Record.DecisionId))
	}
}

// No ALLOW (no decision at all, another correlation, another tenant's correlation) is the typed not-found, never an empty decision with no error.
func TestACorrelationWithNoAllowOfTheTenantIsNotFound(t *testing.T) {
	w := newWorld(t)
	r := decisions(t, w)
	for name, c := range map[string][2]string{"unknown correlation": {tenantA, "corr_NOPE99"}, "another tenant's correlation": {tenantB, corr}} {
		got, err := r.AllowOf(ctx(t), c[0], c[1])
		if !errors.Is(err, trustport.ErrNoRecord) || got.ID != "" {
			t.Errorf("%s: got %+v, %v; want ErrNoRecord", name, got, err)
		}
	}
}

// A question that cannot be answered is an error that is not "none": an empty tenant or correlation, a tenant with no login.
func TestAQuestionThatCannotBeAnsweredIsNeverNone(t *testing.T) {
	w := newWorld(t)
	r := decisions(t, w)
	for name, c := range map[string][2]string{"no tenant": {"", corr}, "no correlation": {tenantA, ""}, "tenant with no login": {"ten_zz", corr}} {
		got, err := r.AllowOf(ctx(t), c[0], c[1])
		if err == nil || errors.Is(err, trustport.ErrNoRecord) || got.ID != "" {
			t.Errorf("%s: got %+v, %v; want an error that is not ErrNoRecord", name, got, err)
		}
	}
	var zero *trustpg.Decisions
	if _, err := zero.AllowOf(ctx(t), tenantA, corr); !errors.Is(err, trustpg.ErrConfig) {
		t.Errorf("a nil reader: got %v; want ErrConfig", err)
	}
}

// A read that fails is an error with fixed text: the driver's error can name the server and the login.
func TestARecoveryDecisionReadThatFailsIsAnErrorWithFixedText(t *testing.T) {
	w := newWorld(t)
	w.trustA.Close()
	_, err := decisions(t, w).AllowOf(ctx(t), tenantA, corr)
	if err == nil || errors.Is(err, trustport.ErrNoRecord) {
		t.Fatalf("got %v; want an error that is not ErrNoRecord", err)
	}
	if !strings.Contains(err.Error(), "trustpg:") || strings.Contains(strings.ToLower(err.Error()), "postgres") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("the error text must be fixed and name no server: %q", err.Error())
	}
}

// The reader writes nothing: its source has no statement that changes a row.
func TestTheDecisionReaderSourceWritesNothing(t *testing.T) {
	src, err := os.ReadFile("decisions.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"INSERT ", "UPDATE ", "DELETE ", "TRUNCATE", ".Exec("} {
		if strings.Contains(string(src), bad) {
			t.Errorf("decisions.go contains %q", bad)
		}
	}
}
