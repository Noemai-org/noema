package session_test

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/session"
)

// A149, A490 (interim, relayed), INV-12: the principal type of a stored role is the one mapping the edge's verifier uses, and only the four roles of ruling A1 have one.
func TestPrincipalTypeOfIsTheEdgeMapping(t *testing.T) {
	for role, want := range map[string]string{"customer": "customer", "ops": "operator", "audit": "auditor", "approver": "approver"} {
		got, ok := session.PrincipalTypeOf(role)
		if !ok || got != want {
			t.Errorf("%s: got %q, %v, want %q", role, got, ok, want)
		}
	}
	for _, role := range []string{"", "admin", "Customer", "customer ", "operator", "auditor"} {
		if got, ok := session.PrincipalTypeOf(role); ok || got != "" {
			t.Errorf("%q: got %q, %v, want no mapping", role, got, ok)
		}
	}
}
