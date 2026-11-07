package outcomee2e_test

// TRU-08 slice 1 (ruling D1, REQ-TRU-005, REQ-EXE-001): Execution's E4 reads the active policy version from the real publication store, as the
// Execution login. A grant is bound to the exact policy_id@version it was decided under: when the active version of that policy_id is not the grant's
// the grant is voided before any write (POLICY_SUPERSEDED), a downgrade cannot be published to make it valid again, and a policy with no active
// version voids the grant (fail closed). All data is SYNTHETIC.

import (
	"context"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
	"github.com/jackc/pgx/v5/pgxpool"
)

// storeBinder answers the active-version question from the publication store, as the tenant's Execution login. The composite production binder is
// not built (DEBT-23, still open); this is the one line of it that this slice proves.
type storeBinder struct {
	controller.Binder
	exec *pgxpool.Pool
}

func (b storeBinder) ActivePolicyVersion(ctx context.Context, g controller.Grant) (string, error) {
	id, _, ok := strings.Cut(g.PolicyVersion, "@")
	if !ok {
		return "", nil
	}
	return policystore.Active(ctx, b.exec, g.TenantID, id)
}

func (s *stack) publish(t *testing.T, version string) error {
	t.Helper()
	trust := s.pools["trust_rw"]
	store := &policystore.Store{Appender: &chain.Appender{Clock: clock.NewManual(s.clk.Now()), IDs: ids.New(s.clk, nil)}}
	tx, err := trust.Begin(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(bg(t), tx, policystore.Request{TenantID: tenant, CorrelationID: "corr_pubadm01", PolicyID: policyScope, Version: version, PublishedBy: "adm_synth01"}); err != nil {
		_ = tx.Rollback(bg(t))
		return err
	}
	return tx.Commit(bg(t))
}

func (s *stack) useStoreBinder(t *testing.T) {
	t.Helper()
	s.ctl.Bind = storeBinder{Binder: s.trust, exec: s.pools["execution_rw"]}
}

func TestAGrantBoundToTheActiveVersionExecutesAndAnotherVersionIsVoidedBeforeAnyWrite(t *testing.T) {
	mc := startMockCase(t)
	s := newStack(t, mc)
	s.useStoreBinder(t)
	if err := s.publish(t, "1.0"); err != nil {
		t.Fatal(err)
	}
	g1, err := s.trust.issueCreateAt(bg(t), "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := s.ctl.Execute(bg(t), tenant, g1); err != nil || out.Void != "" || out.Receipt == nil {
		t.Fatalf("a grant bound to the active version 1.0: %+v, %v", out, err)
	}
	// the policy moves to 2.0: a grant decided under 1.0 is voided at E4, before any spend or dispatch
	if err := s.publish(t, "2.0"); err != nil {
		t.Fatal(err)
	}
	g2, err := s.trust.issueCreateAt(bg(t), "1.0")
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.ctl.Execute(bg(t), tenant, g2)
	if err != nil || out.Void != controller.VoidPolicySuperseded || out.Receipt != nil {
		t.Fatalf("a grant bound to 1.0 after 2.0 was published: %+v, %v; want POLICY_SUPERSEDED and no receipt", out, err)
	}
	if n := s.count(t, `SELECT count(*) FROM grant_spend WHERE grant_id = $1`, g2); n != 0 {
		t.Fatalf("the voided grant was spent %d times", n)
	}
	// the version that is now active executes
	g3, err := s.trust.issueCreateAt(bg(t), "2.0")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := s.ctl.Execute(bg(t), tenant, g3); err != nil || out.Void == controller.VoidPolicySuperseded {
		t.Fatalf("a grant bound to the active version 2.0: %+v, %v", out, err)
	}
}

func TestADowngradeCannotBePublishedToMakeAnOldGrantValidAgain(t *testing.T) {
	mc := startMockCase(t)
	s := newStack(t, mc)
	s.useStoreBinder(t)
	for _, v := range []string{"1.0", "2.0"} {
		if err := s.publish(t, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.publish(t, "1.0"); err == nil {
		t.Fatal("the downgrade to 1.0 was published")
	}
	g, err := s.trust.issueCreateAt(bg(t), "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := s.ctl.Execute(bg(t), tenant, g); err != nil || out.Void != controller.VoidPolicySuperseded {
		t.Fatalf("a grant bound to 1.0 after the refused downgrade: %+v, %v; want POLICY_SUPERSEDED", out, err)
	}
}

func TestAPolicyWithNoActiveVersionVoidsTheGrant(t *testing.T) {
	mc := startMockCase(t)
	s := newStack(t, mc)
	s.useStoreBinder(t)
	g, err := s.trust.issueCreateAt(bg(t), "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := s.ctl.Execute(bg(t), tenant, g); err != nil || out.Void != controller.VoidPolicySuperseded {
		t.Fatalf("a grant for a policy that was never published: %+v, %v; want POLICY_SUPERSEDED (fail closed)", out, err)
	}
}
