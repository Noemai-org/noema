package main

// STM-06 (ruling A514, interim; REQ-INT-001, REQ-SEC-004; INV-12, AGENT-10): the Interpreter port's session resolver builds the Interpreter's session from the orchestrator.Session
// and nothing else. After noema#882 every caller fills it from a verified source (the gateway start from the verified token, a recovery from the stored investigation row), so
// the resolver no longer reads the request's context: a recovery has no request. These tests need no database. All references are SYNTHETIC.

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/orchestrator"
)

const synthAuthnRef = "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func synthSession(role string) orchestrator.Session {
	pt, _ := session.PrincipalTypeOf(role)
	return orchestrator.Session{TenantID: "ten_aa", PrincipalID: "usr_88213", Role: role, AuthnRef: synthAuthnRef, PrincipalType: pt}
}

// A session a caller built from a verified source is resolved with no session in the context: a recovery has no request. The fields are the session's own, the actor is of its principal
// type, and the issuer stays empty because it is not stored (A514).
func TestTheResolverBuildsTheInterpretersSessionFromTheOrchestratorSession(t *testing.T) {
	for _, role := range []string{"customer", "ops", "audit", "approver"} {
		s := synthSession(role)
		got, err := edgeSessions{}.Resolve(context.Background(), s)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if got.Session.TenantID != s.TenantID || got.Session.PrincipalID != s.PrincipalID || got.Session.AuthnRef != s.AuthnRef || got.Session.PrincipalType != s.PrincipalType || got.Session.AssertedBy != "" {
			t.Errorf("%s: the resolved session is %+v", role, got.Session)
		}
		if got.ActorKind != s.PrincipalType || got.ActorKind == "" {
			t.Errorf("%s: the actor kind is %q; want the principal type %q", role, got.ActorKind, s.PrincipalType)
		}
	}
}

// The resolver never reads the request's context: no call to session.FromContext appears in the method (a field a client could influence must not reach the answer, and a recovery
// has no request). The guard parses the source; a mutant that falls back to the context fails it.
func TestTheResolverDoesNotReadTheRequestsContext(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "wire_understanding.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Resolve" || fn.Recv == nil {
			continue
		}
		found = true
		ast.Inspect(fn, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "FromContext" {
				t.Errorf("Resolve reads the request's context at %v", fset.Position(sel.Pos()))
			}
			return true
		})
	}
	if !found {
		t.Fatal("the Resolve method of edgeSessions was not found")
	}
}

// Every member that identifies the caller must be there and agree: a tenant, a principal, a role of the four of ruling A1, an authentication reference of the session shape, and the
// principal type the edge derives from that role. Anything else is one fixed error that names none of them, and no role is ever invented or defaulted.
func TestTheResolverRefusesASessionThatIsNotWhole(t *testing.T) {
	cases := map[string]func(orchestrator.Session) orchestrator.Session{
		"no tenant":                   func(s orchestrator.Session) orchestrator.Session { s.TenantID = ""; return s },
		"no principal":                func(s orchestrator.Session) orchestrator.Session { s.PrincipalID = ""; return s },
		"no role":                     func(s orchestrator.Session) orchestrator.Session { s.Role = ""; return s },
		"a role outside the four":     func(s orchestrator.Session) orchestrator.Session { s.Role = "superuser"; return s },
		"no authentication reference": func(s orchestrator.Session) orchestrator.Session { s.AuthnRef = ""; return s },
		"a malformed reference":       func(s orchestrator.Session) orchestrator.Session { s.AuthnRef = "ses_short"; return s },
		"a reference with upper case": func(s orchestrator.Session) orchestrator.Session {
			s.AuthnRef = "ses_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			return s
		},
		"a token in the reference":          func(s orchestrator.Session) orchestrator.Session { s.AuthnRef = "Bearer " + synthAuthnRef; return s },
		"no principal type":                 func(s orchestrator.Session) orchestrator.Session { s.PrincipalType = ""; return s },
		"a type that differs from the role": func(s orchestrator.Session) orchestrator.Session { s.PrincipalType = "operator"; return s },
		"nothing at all":                    func(orchestrator.Session) orchestrator.Session { return orchestrator.Session{} },
	}
	for name, mutate := range cases {
		_, err := edgeSessions{}.Resolve(context.Background(), mutate(synthSession("customer")))
		if !errors.Is(err, ErrSessionUnavailable) {
			t.Errorf("%s: got %v; want ErrSessionUnavailable", name, err)
			continue
		}
		if msg := err.Error(); strings.Contains(msg, "ten_") || strings.Contains(msg, "usr_") || strings.Contains(msg, "ses_") {
			t.Errorf("%s: the error names a tenant, a principal or a reference: %s", name, msg)
		}
	}
}

// The issuer is only what the caller stored or verified: a non-empty value is passed through unchanged, an empty one stays empty and is never filled from the context or a default.
func TestTheIssuerIsPassedThroughAndNeverInvented(t *testing.T) {
	s := synthSession("ops")
	s.AssertedBy = "issuer.synthetic"
	got, err := edgeSessions{}.Resolve(context.Background(), s)
	if err != nil || got.Session.AssertedBy != "issuer.synthetic" {
		t.Errorf("got %+v, %v; want the issuer unchanged", got.Session, err)
	}
	s.AssertedBy = ""
	got, err = edgeSessions{}.Resolve(context.Background(), s)
	if err != nil || got.Session.AssertedBy != "" {
		t.Errorf("got %+v, %v; want an empty issuer", got.Session, err)
	}
}
