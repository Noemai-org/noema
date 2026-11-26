package trustport_test

// TRU-12, ruling A625 (interim, relayed, not yet committed; INV-12): the decider hands the request's typed session to the scope reader, which is the only place the session comes from.
// SYNTHETIC.

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

func TestTheDeciderHandsTheRequestsSessionToTheScopeReader(t *testing.T) { // A625
	s := &stepSpy{res: withAllow(t, allowed("grt_1"))}
	sc := routeScope()
	r := routeReq()
	r.Session = intake.Session{TenantID: "ten_1", PrincipalID: "prn_1", Role: "customer"}
	if _, err := decider(s, sc).DecideRoute(ctx(t), r); err != nil {
		t.Fatal(err)
	}
	if len(sc.sessions) != 1 || sc.sessions[0] != r.Session {
		t.Fatalf("ScopeFor got sessions %+v, want [%+v]", sc.sessions, r.Session)
	}
}
