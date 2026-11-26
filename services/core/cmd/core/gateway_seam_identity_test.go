package main

// STM-06 follow-up (ruling A514, interim; REQ-INT-001, REQ-SEC-004; INV-12, AGENT-10; PR noema#919 merged without it). CHARACTERISATION TESTS: the behaviour exists, there is no
// production change and no red run; their teeth are the hand mutants in the pull request. How the seam reads: the gateway start (turnStarter.Start) builds the orchestrator.Session
// from the VERIFIED token's identity (tenant, principal, session reference and verified issuer, which gatewayIdentity takes from the session middleware's principal, and the customer
// role and its principal type); edgeSessions.Resolve turns that Session, and nothing else, into the Interpreter's session; the port refuses what is not whole before it asks the
// Interpreter. These tests go through the real chain (see gateway_seam_world_test.go), need no database, and name the verified token's own values as the oracle.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	interpreterport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/interpreter"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/understanding"
)

// The Interpreter is handed the verified token's tenant, principal, role, authentication reference, principal type and issuer, for two different tokens (so no value is a fixed one).
func TestTheInterpreterIsHandedExactlyTheVerifiedTokensIdentity(t *testing.T) {
	for name, c := range map[string]struct{ tenant, sub string }{"tenant A": {seamTenantA, seamPrincipalA}, "tenant B": {seamTenantB, seamPrincipalB}} {
		t.Run(name, func(t *testing.T) {
			w := newSeamWorld(t)
			authz, p := seamToken(t, w.iss, session.RoleCustomer, c.tenant, c.sub)
			if rec := w.send(authz, startBody(aRequestID, "synthetic text"), nil, ""); rec.Code != http.StatusAccepted {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			w.drain()
			seamAssertIdentity(t, w, p)
			if got := w.port.sessions()[0]; got.Role != "customer" || got.PrincipalType != "customer" || got.AssertedBy != session.Issuer {
				t.Errorf("the session has role %q, type %q and issuer %q; want customer, customer and the verified issuer", got.Role, got.PrincipalType, got.AssertedBy)
			}
		})
	}
}

// Nothing a request carries changes the identity: another tenant, principal, role, type, issuer or session reference in the headers or the query still gives the token's own, and a
// body that names any of them is refused by the gateway before anything is recorded, started or interpreted (INV-12, AGENT-10).
func TestNothingTheRequestCarriesChangesTheIdentity(t *testing.T) {
	w := newSeamWorld(t)
	authz, p := seamToken(t, w.iss, session.RoleCustomer, seamTenantA, seamPrincipalA)
	forged := map[string]string{"tenant_id": seamTenantB, "principal_id": seamPrincipalB, "principal_ref": seamPrincipalB, "role": "ops", "roles": "ops", "principal_type": "operator",
		"asserted_by": "forged-idp", "issuer": "forged-idp", "session_id": "ses_ffffffffffffffffffffffffffffffff", "authn_ref": "ses_ffffffffffffffffffffffffffffffff"}
	for member, value := range forged {
		body := `{"request_id":"` + aRequestID + `","channel":"text","text":"x","` + member + `":"` + value + `"}`
		if rec := w.send(authz, body, nil, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("a body naming %s: status %d, want 400", member, rec.Code)
		}
	}
	w.drain()
	seamNothingRan(t, w, "a body that names an identity")
	if w.rec.n != 0 {
		t.Errorf("%d inputs were recorded for refused bodies; want none", w.rec.n)
	}

	w = newSeamWorld(t)
	authz, p = seamToken(t, w.iss, session.RoleCustomer, seamTenantA, seamPrincipalA)
	headers := map[string]string{"X-Tenant-Id": seamTenantB, "X-Principal-Id": seamPrincipalB, "X-Principal-Type": "operator", "X-Role": "ops", "X-Roles": "ops",
		"X-Asserted-By": "forged-idp", "X-Authn-Ref": "ses_ffffffffffffffffffffffffffffffff", "X-Session-Id": "ses_ffffffffffffffffffffffffffffffff", "X-Forwarded-User": seamPrincipalB}
	query := "tenant_id=" + seamTenantB + "&principal_id=" + seamPrincipalB + "&role=ops&principal_type=operator&asserted_by=forged-idp&authn_ref=ses_ffffffffffffffffffffffffffffffff"
	if rec := w.send(authz, startBody(aRequestID, "synthetic text"), headers, query); rec.Code != http.StatusAccepted {
		t.Fatalf("headers and a query that name another identity: status %d: %s", rec.Code, rec.Body.String())
	}
	w.drain()
	seamAssertIdentity(t, w, p)
}

// Only a customer token starts an investigation: an ops, audit or approver token is refused at the role gate, and nothing past the gate runs.
func TestAnotherRolesTokenNeverReachesTheInterpreter(t *testing.T) {
	w := newSeamWorld(t)
	for _, role := range []session.Role{session.RoleOps, session.RoleAudit, session.RoleApprover} {
		authz, _ := seamToken(t, w.iss, role, seamTenantA, seamPrincipalA)
		if rec := w.send(authz, startBody(aRequestID, "x"), nil, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", role, rec.Code)
		}
	}
	if rec := w.send("", startBody(aRequestID, "x"), nil, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status %d, want 401", rec.Code)
	}
	w.drain()
	seamNothingRan(t, w, "a token that is not a customer's")
	if w.rec.n != 0 {
		t.Errorf("%d inputs were recorded; want none", w.rec.n)
	}
}

// The issuer is the verified session's and is carried, not chosen: a Turn that the real Start is given with an issuer of its own reaches the store, the port and the Interpreter as
// that issuer, whatever the process's own provider is (a fixed issuer in Start, the port or the resolver fails here). A Turn with no issuer, or one off the contract's pattern, is
// refused by Start before the store is asked.
func TestTheIssuerIsCarriedFromTheTurnAndNeverInvented(t *testing.T) {
	w := newSeamWorld(t)
	w.rec.admit(seamRecordRef, orchestrator.Admission{EventType: "INPUT_RECEIVED", TenantID: seamTenantA, PrincipalID: seamPrincipalA, CorrelationID: "corr_" + aULID, RequestID: aRequestID, ObjectRef: "req_01ARZ3NDEKTSV4RRFFQ69G5FAV"})
	if err := w.starter.Start(t.Context(), seamTurn(func(tn *inputgateway.Turn) { tn.AssertedBy = "idp_two-9" })); err != nil {
		t.Fatal(err)
	}
	w.drain()
	want := orchestrator.Session{TenantID: seamTenantA, PrincipalID: seamPrincipalA, Role: "customer", AuthnRef: seamAuthnRef, PrincipalType: "customer", AssertedBy: "idp_two-9"}
	if w.store.sess != want {
		t.Errorf("the store was asked as %+v; want %+v", w.store.sess, want)
	}
	if got := w.port.sessions(); len(got) != 1 || got[0] != want {
		t.Errorf("the port received %+v; want %+v", got, want)
	}
	if in := w.core.inputs(); len(in) != 1 || in[0].Session.AssertedBy != "idp_two-9" || in[0].Session.AuthnRef != seamAuthnRef || in[0].Session.PrincipalType != "customer" {
		t.Errorf("the Interpreter was handed %+v; want the Turn's own issuer, reference and type", in)
	}
}

func seamTurn(mod func(*inputgateway.Turn)) inputgateway.Turn {
	tn := inputgateway.Turn{Identity: inputgateway.Identity{TenantID: seamTenantA, PrincipalID: seamPrincipalA, PrincipalKind: "customer", SessionID: seamAuthnRef, AssertedBy: "noema-poc-idp"},
		CorrelationID: "corr_" + aULID, RequestID: aRequestID, Channel: "text", Text: "x", Input: inputgateway.Ref{EventID: seamRecordRef, ObjectRef: "req_01ARZ3NDEKTSV4RRFFQ69G5FAV"}}
	if mod != nil {
		mod(&tn)
	}
	return tn
}

// A turn the production Start refuses, or a store that fails, never reaches the Interpreter: the counting double saw no call, and no session reached the port.
func TestTheInterpreterIsNeverCalledAfterTheStartRefuses(t *testing.T) {
	w := newSeamWorld(t)
	w.rec.admit(seamRecordRef, orchestrator.Admission{EventType: "INPUT_RECEIVED", TenantID: seamTenantA, PrincipalID: seamPrincipalA, CorrelationID: "corr_" + aULID, RequestID: aRequestID, ObjectRef: "req_01ARZ3NDEKTSV4RRFFQ69G5FAV"})
	for name, mod := range map[string]func(*inputgateway.Turn){
		"no issuer":                    func(tn *inputgateway.Turn) { tn.AssertedBy = "" },
		"an issuer in capitals":        func(tn *inputgateway.Turn) { tn.AssertedBy = "Forged-IDP" },
		"an operator's principal kind": func(tn *inputgateway.Turn) { tn.PrincipalKind = "operator" },
		"no principal kind":            func(tn *inputgateway.Turn) { tn.PrincipalKind = "" },
		"a correlation not minted":     func(tn *inputgateway.Turn) { tn.CorrelationID = "corr_not-a-ulid" },
		"a tenant with no store":       func(tn *inputgateway.Turn) { tn.TenantID = "ten_zz" },
	} {
		if err := w.starter.Start(t.Context(), seamTurn(mod)); err == nil {
			t.Errorf("%s: Start accepted the turn", name)
		}
	}
	w.store.err = errors.New("synthetic store failure")
	if err := w.starter.Start(t.Context(), seamTurn(nil)); err == nil {
		t.Error("a failing store: Start accepted the turn")
	}
	w.drain()
	if got := w.port.sessions(); len(got) != 0 {
		t.Errorf("the port received %+v after a refusal; want nothing", got)
	}
	if got := w.core.inputs(); len(got) != 0 {
		t.Errorf("the Interpreter was called %d times after a refusal; want none", len(got))
	}
}

// A token that the edge accepts but whose subject is not a principal of the Interpreter's form (usr_) is not turned into a session: the port refuses it with its one session error and
// the Interpreter is never called (the port is the last guard before the model service).
func TestAVerifiedSubjectOfAnotherFormIsRefusedBeforeTheInterpreter(t *testing.T) {
	w := newSeamWorld(t)
	authz, p := seamToken(t, w.iss, session.RoleCustomer, seamTenantA, "prn_1")
	if rec := w.send(authz, startBody(aRequestID, "x"), nil, ""); rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	w.drain()
	if got := w.port.sessions(); len(got) != 1 || got[0] != seamWantSession(p) {
		t.Errorf("the port received %+v; want the verified session %+v", got, seamWantSession(p))
	}
	if len(w.fails.errs) != 1 || !errors.Is(w.fails.errs[0], interpreterport.ErrSession) {
		t.Errorf("the run reported %v; want one interpreterport.ErrSession", w.fails.errs)
	}
	if got := w.core.inputs(); len(got) != 0 {
		t.Errorf("the Interpreter was called %d times; want none", len(got))
	}
}

func seamWholeSession() orchestrator.Session {
	return orchestrator.Session{TenantID: seamTenantA, PrincipalID: seamPrincipalA, Role: "customer", AuthnRef: seamAuthnRef, PrincipalType: "customer", AssertedBy: "noema-poc-idp"}
}

func seamWholeTurn() orchestrator.Turn {
	return orchestrator.Turn{InvestigationID: "inv_" + aULID, CorrelationID: "corr_" + aULID, Text: "x", InputReceivedRef: seamRecordRef, RequestID: aRequestID}
}

func seamAdmitWhole(rec *seamRecord) {
	rec.admit(seamRecordRef, orchestrator.Admission{EventType: "INPUT_RECEIVED", TenantID: seamTenantA, PrincipalID: seamPrincipalA, CorrelationID: "corr_" + aULID, RequestID: aRequestID, ObjectRef: "req_01ARZ3NDEKTSV4RRFFQ69G5FAV"})
}

// A Session that is not whole is refused by the real port, with the resolver's error or the port's own, and the Interpreter is never called. The whole Session is the control: the
// same turn, record and port ask the Interpreter once, so every refusal below is the Session's and nothing else's. No member is ever defaulted.
func TestThePortRefusesASessionThatIsNotWholeAndNeverCallsTheInterpreter(t *testing.T) {
	rec, core := newSeamRecord(), &seamCore{}
	port := seamPortOver(t, core, rec)
	seamAdmitWhole(rec)
	if in, err := port.Interpret(t.Context(), seamWholeSession(), seamWholeTurn()); err != nil || in.Kind != orchestrator.GateFailed || len(core.inputs()) != 1 {
		t.Fatalf("the control: %+v, %v, %d calls; want the whole session interpreted once", in, err, len(core.inputs()))
	}
	core.mu.Lock()
	core.in = nil
	core.mu.Unlock()
	resolver, shape := ErrSessionUnavailable, interpreterport.ErrSession
	cases := []struct {
		name   string
		mutate func(*orchestrator.Session)
		want   error
	}{
		{"no tenant", func(s *orchestrator.Session) { s.TenantID = "" }, resolver},
		{"a tenant with a space", func(s *orchestrator.Session) { s.TenantID = "ten_ aa" }, resolver},
		{"no principal", func(s *orchestrator.Session) { s.PrincipalID = "" }, resolver},
		{"no role", func(s *orchestrator.Session) { s.Role = "" }, resolver},
		{"a role outside the four", func(s *orchestrator.Session) { s.Role = "superuser" }, resolver},
		{"a principal type as the role", func(s *orchestrator.Session) { s.Role = "operator" }, resolver},
		{"no authentication reference", func(s *orchestrator.Session) { s.AuthnRef = "" }, resolver},
		{"a short reference", func(s *orchestrator.Session) { s.AuthnRef = "ses_short" }, resolver},
		{"a token as the reference", func(s *orchestrator.Session) { s.AuthnRef = "Bearer " + seamAuthnRef }, resolver},
		{"no principal type", func(s *orchestrator.Session) { s.PrincipalType = "" }, resolver},
		{"a type that differs from the role", func(s *orchestrator.Session) { s.PrincipalType = "operator" }, resolver},
		{"no issuer", func(s *orchestrator.Session) { s.AssertedBy = "" }, shape},
		{"an issuer in capitals", func(s *orchestrator.Session) { s.AssertedBy = "Noema-IDP" }, shape},
		{"an issuer with a space", func(s *orchestrator.Session) { s.AssertedBy = "idp idp" }, shape},
		{"a principal that is not a usr_ id", func(s *orchestrator.Session) { s.PrincipalID = "prn_1" }, shape},
		{"a tenant that is not a ten_ id", func(s *orchestrator.Session) { s.TenantID = "tnt_a" }, shape},
	}
	for _, c := range cases {
		s := seamWholeSession()
		c.mutate(&s)
		_, err := port.Interpret(t.Context(), s, seamWholeTurn())
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v; want %v", c.name, err, c.want)
			continue
		}
		if msg := err.Error(); strings.Contains(msg, "ten_") || strings.Contains(msg, "usr_") || strings.Contains(msg, "ses_") || strings.Contains(msg, "idp") {
			t.Errorf("%s: the error names a tenant, a principal, a reference or an issuer: %s", c.name, msg)
		}
	}
	if got := core.inputs(); len(got) != 0 {
		t.Errorf("the Interpreter was called %d times after a refusal; want none", len(got))
	}
}

// The resolver builds the Interpreter's session from the orchestrator.Session alone. A context that carries ANOTHER verified session (another tenant, principal, role and reference)
// neither completes a member the Session lacks nor replaces one it has, and no issuer is read from it (INV-12, AGENT-10; a recovery has no request).
func TestAVerifiedSessionInTheContextNeitherCompletesNorReplacesTheSession(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	authz, other := seamToken(t, iss, session.RoleOps, seamTenantB, seamPrincipalB)
	var ctx context.Context
	clk := clock.NewManual(apiEpoch)
	mw := session.Middleware(seamVerifier(t, iss, seamTenantB), ids.New(clk, nil))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() }))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", authz)
	mw.ServeHTTP(httptest.NewRecorder(), req)
	if p, ok := session.FromContext(ctx); !ok || p.ID != other.ID || p.Tenant != seamTenantB || p.Role != session.RoleOps {
		t.Fatalf("the context holds %+v, %v; want the other verified session", p, ok)
	}
	got, err := edgeSessions{}.Resolve(ctx, seamWholeSession())
	want := understanding.Session{PrincipalID: seamPrincipalA, TenantID: seamTenantA, AuthnRef: seamAuthnRef, PrincipalType: "customer", AssertedBy: "noema-poc-idp"}
	if err != nil || got.Session != want || got.ActorKind != "customer" {
		t.Errorf("a whole session with another session in the context: %+v, %v; want %+v as customer", got, err, want)
	}
	for name, mutate := range map[string]func(*orchestrator.Session){
		"no tenant":         func(s *orchestrator.Session) { s.TenantID = "" },
		"no principal":      func(s *orchestrator.Session) { s.PrincipalID = "" },
		"no role":           func(s *orchestrator.Session) { s.Role = "" },
		"no reference":      func(s *orchestrator.Session) { s.AuthnRef = "" },
		"no principal type": func(s *orchestrator.Session) { s.PrincipalType = "" },
	} {
		s := seamWholeSession()
		mutate(&s)
		if r, err := (edgeSessions{}).Resolve(ctx, s); !errors.Is(err, ErrSessionUnavailable) {
			t.Errorf("%s, with another session in the context: %+v, %v; want ErrSessionUnavailable", name, r, err)
		}
	}
	noIssuer := seamWholeSession()
	noIssuer.AssertedBy = ""
	if r, err := (edgeSessions{}).Resolve(ctx, noIssuer); err != nil || r.Session.AssertedBy != "" {
		t.Errorf("no issuer, with another session in the context: %+v, %v; want the issuer left empty, never read from the context", r.Session, err)
	}
	rec, core := newSeamRecord(), &seamCore{}
	port := seamPortOver(t, core, rec)
	seamAdmitWhole(rec)
	if _, err := port.Interpret(ctx, noIssuer, seamWholeTurn()); !errors.Is(err, interpreterport.ErrSession) || len(core.inputs()) != 0 {
		t.Errorf("a session with no issuer, with another session in the context: %v, %d calls; want ErrSession and no call", err, len(core.inputs()))
	}
}
