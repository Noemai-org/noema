package orchestrator_test

// STM-01, ruling A205 (Q5, relayed, not yet committed), INV-12, AGENT-10: the authenticated session carries the caller's role, and the orchestrator hands the session to
// every port exactly as it received it, role included, so Trust's technical-authority step (T6) reads the role from the session and from nothing else. The orchestrator
// itself interprets no role: an empty role is the edge's and Trust's to refuse, not the runner's. All data is SYNTHETIC.

import (
	"context"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

func TestTheSessionsRoleReachesEveryPortUnchanged(t *testing.T) {
	f := happy()
	s := orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_01", Role: "customer"}
	if _, err := runner(t, f).RunRead(context.Background(), s, turn); err != nil {
		t.Fatal(err)
	}
	if f.session != s || f.session.Role != "customer" {
		t.Fatalf("the ports saw %+v, want the session as given, role included: %+v", f.session, s)
	}
}

// A session with no role is not refused by the runner: Trust refuses it at T6 (fail closed there), and a read-only run for a caller the edge has verified must not be
// stopped by a field the orchestrator does not interpret.
func TestASessionWithoutARoleIsPassedOnAsItIs(t *testing.T) {
	f := happy()
	if _, err := runner(t, f).RunRead(context.Background(), sess, turn); err != nil || f.session.Role != "" {
		t.Fatalf("err %v, role %q; want the session passed on with its role empty", err, f.session.Role)
	}
}
