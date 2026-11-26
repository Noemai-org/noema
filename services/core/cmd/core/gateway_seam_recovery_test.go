package main

// STM-06 follow-up (ruling A514, interim; REQ-INT-001, REQ-SEC-004; INV-12, AGENT-10; PR noema#919 merged without it). CHARACTERISATION TESTS, no database. A recovery has no request:
// recovery.NewRecoverySession builds its session from the stored investigation row alone (investigations.RecordedSession, what SessionOf returns), and the same resolver and port turn
// it into the Interpreter's session. The row is whole or the session is refused whole: no partial session, no member from a default, a request or the caller. The storage half (Start
// writes the verified session's issuer into investigation.asserted_by, migration 0039, and SessionOf returns it) is proved on a real database by the CI-only tests of
// orchestrator/investigations (issuer_column_test.go, session_columns_test.go); here the row is the value those tests prove SessionOf returns.

import (
	"net/http"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
	"github.com/Noemai-org/noema/services/core/understanding"
)

// The row Start writes from the verified session is the row recovery rebuilds the session from: the session of a recovery is, member for member, the session the gateway start made
// from the token, and the Interpreter is handed the same understanding.Session either way.
func TestARecoverySessionEqualsTheSessionTheGatewayStartMadeFromTheToken(t *testing.T) {
	w := newSeamWorld(t)
	authz, p := seamToken(t, w.iss, session.RoleCustomer, seamTenantA, seamPrincipalA)
	if rec := w.send(authz, startBody(aRequestID, "synthetic text"), nil, ""); rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	w.drain()
	started := w.store.sess
	if started != seamWantSession(p) {
		t.Fatalf("the start built %+v; want the verified session %+v", started, seamWantSession(p))
	}
	row := investigations.RecordedSession{PrincipalID: started.PrincipalID, PrincipalRole: started.Role, AuthnRef: started.AuthnRef, AssertedBy: started.AssertedBy}
	recovered, err := recovery.NewRecoverySession(recovery.Candidate{TenantID: started.TenantID, PrincipalRef: started.PrincipalID}, row)
	if err != nil || recovered != started {
		t.Fatalf("the recovery session is %+v, %v; want exactly the start's %+v", recovered, err, started)
	}
	rec2, core2 := newSeamRecord(), &seamCore{}
	port2 := seamPortOver(t, core2, rec2)
	seamAdmitWhole(rec2)
	if _, err := port2.Interpret(t.Context(), recovered, seamWholeTurn()); err != nil {
		t.Fatal(err)
	}
	if a, b := w.core.inputs(), core2.inputs(); len(a) != 1 || len(b) != 1 || a[0].Session != b[0].Session || a[0].Actor != b[0].Actor {
		t.Errorf("the Interpreter was handed %+v from the start and %+v from the recovery; want the same session and actor", a, b)
	}
}

// For each of the four roles the recovery session carries the row's own principal, role, reference and issuer, the principal type the edge derives from the role, and nothing else:
// the issuer is the row's (a value of its own, not the process's provider) and the Interpreter is handed it, with the actor of that type.
func TestARecoverySessionIsBuiltFromTheStoredRowAlone(t *testing.T) {
	for role, pt := range map[string]string{"customer": "customer", "ops": "operator", "audit": "auditor", "approver": "approver"} {
		row := investigations.RecordedSession{PrincipalID: seamPrincipalB, PrincipalRole: role, AuthnRef: "ses_fedcba9876543210fedcba9876543210", AssertedBy: "idp_two-9"}
		s, err := recovery.NewRecoverySession(recovery.Candidate{TenantID: seamTenantB, PrincipalRef: seamPrincipalB}, row)
		want := orchestrator.Session{TenantID: seamTenantB, PrincipalID: seamPrincipalB, Role: role, AuthnRef: "ses_fedcba9876543210fedcba9876543210", PrincipalType: pt, AssertedBy: "idp_two-9"}
		if err != nil || s != want {
			t.Errorf("%s: the session is %+v, %v; want %+v", role, s, err, want)
			continue
		}
		rec, core := newSeamRecord(), &seamCore{}
		port := seamPortOver(t, core, rec)
		rec.admit(seamRecordRef, orchestrator.Admission{EventType: "INPUT_RECEIVED", TenantID: seamTenantB, PrincipalID: seamPrincipalB, CorrelationID: "corr_" + aULID, RequestID: aRequestID, ObjectRef: "req_01ARZ3NDEKTSV4RRFFQ69G5FAV"})
		if _, err := port.Interpret(t.Context(), s, seamWholeTurn()); err != nil {
			t.Errorf("%s: %v", role, err)
			continue
		}
		in := core.inputs()
		if len(in) != 1 || in[0].Session != (understanding.Session{PrincipalID: seamPrincipalB, TenantID: seamTenantB, AuthnRef: want.AuthnRef, PrincipalType: pt, AssertedBy: "idp_two-9"}) || in[0].Actor != pt+":"+seamPrincipalB {
			t.Errorf("%s: the Interpreter was handed %+v; want the row's identity as %s", role, in, pt)
		}
	}
}

// A row that lacks a member, or holds one outside its form, is refused whole: the error, the zero Session (no partial session) and, following the order recover.go uses (a refused
// session goes no further), no call to the port and none to the Interpreter. A row from before migration 0039 has no issuer and is such a row.
func TestARecoverySessionIsRefusedWholeWhenTheRowLacksAMember(t *testing.T) {
	good := recovery.Candidate{TenantID: seamTenantA, PrincipalRef: seamPrincipalA}
	row := investigations.RecordedSession{PrincipalID: seamPrincipalA, PrincipalRole: "customer", AuthnRef: seamAuthnRef, AssertedBy: "noema-poc-idp"}
	if s, err := recovery.NewRecoverySession(good, row); err != nil || s == (orchestrator.Session{}) {
		t.Fatalf("the control row: %+v, %v; want a session", s, err)
	}
	type mutation func(*recovery.Candidate, *investigations.RecordedSession)
	cases := map[string]mutation{
		"no tenant":                       func(c *recovery.Candidate, _ *investigations.RecordedSession) { c.TenantID = "" },
		"a tenant of another form":        func(c *recovery.Candidate, _ *investigations.RecordedSession) { c.TenantID = "tnt_a" },
		"no principal on the row":         func(c *recovery.Candidate, r *investigations.RecordedSession) { c.PrincipalRef, r.PrincipalID = "", "" },
		"a stored principal that differs": func(_ *recovery.Candidate, r *investigations.RecordedSession) { r.PrincipalID = seamPrincipalB },
		"no stored principal":             func(_ *recovery.Candidate, r *investigations.RecordedSession) { r.PrincipalID = "" },
		"no stored role":                  func(_ *recovery.Candidate, r *investigations.RecordedSession) { r.PrincipalRole = "" },
		"a role outside the four":         func(_ *recovery.Candidate, r *investigations.RecordedSession) { r.PrincipalRole = "superuser" },
		"a principal type as the role":    func(_ *recovery.Candidate, r *investigations.RecordedSession) { r.PrincipalRole = "operator" },
		"no authn reference":              func(_ *recovery.Candidate, r *investigations.RecordedSession) { r.AuthnRef = "" },
		"a short authn reference":         func(_ *recovery.Candidate, r *investigations.RecordedSession) { r.AuthnRef = "ses_short" },
		"a token as the reference":        func(_ *recovery.Candidate, r *investigations.RecordedSession) { r.AuthnRef = "Bearer " + seamAuthnRef },
		"no verified issuer":              func(_ *recovery.Candidate, r *investigations.RecordedSession) { r.AssertedBy = "" },
		"an issuer in capitals":           func(_ *recovery.Candidate, r *investigations.RecordedSession) { r.AssertedBy = "Noema-IDP" },
		"an issuer with a space":          func(_ *recovery.Candidate, r *investigations.RecordedSession) { r.AssertedBy = "idp idp" },
		"an empty row":                    func(_ *recovery.Candidate, r *investigations.RecordedSession) { *r = investigations.RecordedSession{} },
	}
	rec, core := newSeamRecord(), &seamCore{}
	port := seamPortOver(t, core, rec)
	seamAdmitWhole(rec)
	for name, mutate := range cases {
		c, r := good, row
		mutate(&c, &r)
		s, err := recovery.NewRecoverySession(c, r)
		if err == nil || s != (orchestrator.Session{}) {
			t.Errorf("%s: got %+v, %v; want an error and the zero Session", name, s, err)
			continue
		}
		if _, err := port.Interpret(t.Context(), s, seamWholeTurn()); err == nil {
			t.Errorf("%s: the port interpreted the zero Session", name)
		}
	}
	if got := core.inputs(); len(got) != 0 {
		t.Errorf("the Interpreter was called %d times after a refused recovery session; want none", len(got))
	}
}
