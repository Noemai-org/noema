package ledger_test

// TRU-08 follow-up (DEBT-23, REQ-TRU-005, REQ-EXE-001, INV-09, INV-12): the database half of the production controller.Binder. As the tenant's Execution login it
// reads (1) the active policy version of the grant's policy_id from the publication store and (2) whether the grant's decision is still an ALLOW under that active
// version. It fails closed: an unknown decision, a missing publication, another version, another tenant's rows and an unwired source all answer "no". The intent
// hash and the stored plan come from sources other tasks build, so they are ports here. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

const (
	policy     = "pol_escalate"
	intentHash = "sha256:0000000000000000000000000000000000000000000000000000000000000001"
)

// trustLogins holds one Trust login per world: testpg creates the role on each call, so a test that publishes twice must reuse it.
var trustLogins sync.Map

func (w *world) publish(t *testing.T, version string) {
	t.Helper()
	// Only Trust's own tenant login publishes (the table's guard); the Execution login only reads.
	v, ok := trustLogins.Load(w)
	if !ok {
		v = testpg.TenantLogin(t, w.admin, w.admin.Config().ConnString(), "trust_rw", tenant)
		trustLogins.Store(w, v)
		t.Cleanup(func() { trustLogins.Delete(w) })
	}
	trust := v.(*pgxpool.Pool)
	if _, err := trust.Exec(ctx(t), `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by) VALUES ($1, $2, $3, policy_version_key($3), 'adm_synth01')`,
		tenant, policy, version); err != nil {
		t.Fatal(err)
	}
}

func (w *world) storeBinder() *ledger.StoreBinder {
	return &ledger.StoreBinder{Pool: w.exec, Ledger: w.led}
}

func grantAt(w *world, t *testing.T, n int, version string) controller.Grant {
	t.Helper()
	// A grant carries the policy version of its decision record (migration 0012), so each version has its own recorded decision.
	return w.issue(t, n, func(g *controller.Grant) {
		g.PolicyVersion = policy + "@" + version
		g.DecisionID = "dec_v" + strings.ReplaceAll(version, ".", "")
	})
}

func TestTheActivePolicyVersionIsTheHighestPublishedOne(t *testing.T) {
	w := newWorld(t)
	b := w.storeBinder()
	g := grantAt(w, t, 1, "1.0")
	if got, err := b.ActivePolicyVersion(ctx(t), g); err != nil || got != "" {
		t.Fatalf("no publication: %q, %v; want \"\" (fail closed)", got, err)
	}
	w.publish(t, "1.0")
	w.publish(t, "2.0")
	w.publish(t, "10.0") // numeric, not text, order
	if got, err := b.ActivePolicyVersion(ctx(t), g); err != nil || got != policy+"@10.0" {
		t.Fatalf("got %q, %v; want %s@10.0", got, err, policy)
	}
	if got, err := b.ActivePolicyVersion(ctx(t), controller.Grant{TenantID: tenant, PolicyVersion: "no-at-sign"}); err != nil || got != "" {
		t.Fatalf("a policy version with no @: %q, %v; want \"\"", got, err)
	}
}

func TestADecisionIsStillAllowedOnlyWhileItsPolicyVersionIsActive(t *testing.T) {
	w := newWorld(t)
	b := w.storeBinder()
	w.publish(t, "1.0")
	g := grantAt(w, t, 1, "1.0")
	if ok, err := b.DecisionStillAllowed(ctx(t), g); err != nil || !ok {
		t.Fatalf("the ALLOW decision under the active version: %v, %v; want allowed", ok, err)
	}
	w.publish(t, "2.0")
	if ok, err := b.DecisionStillAllowed(ctx(t), g); err != nil || ok {
		t.Fatalf("after 2.0 was published: %v, %v; want not allowed (POLICY_SUPERSEDED)", ok, err)
	}
	g2 := grantAt(w, t, 2, "2.0")
	if ok, err := b.DecisionStillAllowed(ctx(t), g2); err != nil || !ok {
		t.Fatalf("a decision under the new active version: %v, %v; want allowed", ok, err)
	}
}

func TestAnUnknownDecisionOrAnUnpublishedPolicyIsNeverAllowed(t *testing.T) {
	w := newWorld(t)
	b := w.storeBinder()
	g := grantAt(w, t, 1, "1.0") // a recorded ALLOW decision, but the policy was never published
	if ok, err := b.DecisionStillAllowed(ctx(t), g); err != nil || ok {
		t.Fatalf("an unpublished policy: %v, %v; want not allowed", ok, err)
	}
	w.publish(t, "1.0")
	g.DecisionID = "dec_neverrecorded"
	if ok, err := b.DecisionStillAllowed(ctx(t), g); err != nil || ok {
		t.Fatalf("a decision that is not in the Record: %v, %v; want not allowed", ok, err)
	}
}

// E4: a decision that is recorded but is not an ALLOW (here a REJECT) never allows execution, whatever the active policy version is.
func TestARecordedDecisionThatIsNotAnAllowIsNeverAllowed(t *testing.T) {
	w := newWorld(t)
	b := w.storeBinder()
	w.publish(t, "1.0")
	g := grantAt(w, t, 1, "1.0")
	g.DecisionID = "dec_rejected01"
	doc := testpg.DecisionDocJSON(g.DecisionID, tenant, g.IntentHash, g.PolicyVersion, g.CorrelationID)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		VALUES ($1, $2, jsonb_set($3::jsonb, '{outcome}', '"REJECT"'), 'REJECT', $4, '[]', 'k1', 'aa')`, g.DecisionID, tenant, doc, g.IntentHash); err != nil {
		t.Fatal(err)
	}
	if ok, err := b.DecisionStillAllowed(ctx(t), g); err != nil || ok {
		t.Fatalf("a REJECT decision under the active version: %v, %v; want not allowed", ok, err)
	}
}

// INV-12 / REQ-SEC-004: the tenant is the login's. A grant that names another tenant finds neither the decision nor the publication, through this tenant's login.
func TestAnotherTenantsGrantIsNeverAllowedThroughThisTenantsLogin(t *testing.T) {
	w := newWorld(t)
	b := w.storeBinder()
	w.publish(t, "1.0")
	g := grantAt(w, t, 1, "1.0")
	g.TenantID = "ten_bb"
	if ok, err := b.DecisionStillAllowed(ctx(t), g); err != nil || ok {
		t.Fatalf("another tenant's grant: %v, %v; want not allowed", ok, err)
	}
	if got, err := b.ActivePolicyVersion(ctx(t), g); err != nil || got != "" {
		t.Fatalf("another tenant's policy: %q, %v; want \"\"", got, err)
	}
}

func TestTheBinderNeedsADeadlineAndItsSources(t *testing.T) {
	w := newWorld(t)
	b := w.storeBinder()
	g := grantAt(w, t, 1, "1.0")
	if _, err := b.ActivePolicyVersion(context.Background(), g); err == nil {
		t.Error("a read without a context deadline was made")
	}
	if _, err := b.DecisionStillAllowed(context.Background(), g); err == nil {
		t.Error("a decision read without a context deadline was made")
	}
	if _, err := b.LatestIntentHash(ctx(t), g); !errors.Is(err, ledger.ErrBinderNotWired) {
		t.Errorf("no intent source: %v; want ErrBinderNotWired", err)
	}
	if _, err := b.Plan(ctx(t), g); !errors.Is(err, ledger.ErrBinderNotWired) {
		t.Errorf("no plan source: %v; want ErrBinderNotWired", err)
	}
	if _, err := (&ledger.StoreBinder{}).WritesEnabled(ctx(t), tenant); !errors.Is(err, ledger.ErrBinderNotWired) {
		t.Errorf("no ledger: %v; want ErrBinderNotWired", err)
	}
	if ok, err := b.WritesEnabled(ctx(t), tenant); err != nil || !ok {
		t.Errorf("the kill switch read through the ledger: %v, %v; want writes enabled", ok, err)
	}
}

type fixedSources struct{}

func (fixedSources) LatestIntentHash(context.Context, controller.Grant) (string, error) {
	return intentHash, nil
}
func (fixedSources) Plan(context.Context, controller.Grant) (controller.PlanStep, error) {
	return controller.PlanStep{PlanHash: "sha256:p", Step: 1}, nil
}

func TestTheBinderPassesTheIntentAndPlanReadsThroughToTheirSources(t *testing.T) {
	w := newWorld(t)
	b := w.storeBinder()
	b.Intents, b.Plans = fixedSources{}, fixedSources{}
	g := grantAt(w, t, 1, "1.0")
	if h, err := b.LatestIntentHash(ctx(t), g); err != nil || h != intentHash {
		t.Errorf("intent hash %q, %v", h, err)
	}
	if p, err := b.Plan(ctx(t), g); err != nil || p.PlanHash != "sha256:p" {
		t.Errorf("plan %+v, %v", p, err)
	}
	var _ controller.Binder = b
}
