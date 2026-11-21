package trustpack_test

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

// Proves REQ-TRU-004, REQ-TRU-005 (T5 scope, T6 technical authority), REQ-INT-005 (the node with no profile is out of scope), A255, INV-16.

func newScope(t testing.TB, p *trustpack.Pack) trustpack.Scope {
	t.Helper()
	s, err := trustpack.NewScope(p)
	if err != nil {
		t.Fatalf("NewScope: %v", err)
	}
	return s
}

func newAuthority(t testing.TB, p *trustpack.Pack) trustpack.Authority {
	t.Helper()
	a, err := trustpack.NewAuthority(p)
	if err != nil {
		t.Fatalf("NewAuthority: %v", err)
	}
	return a
}

func TestScopeAndAuthorityNeedAPack(t *testing.T) {
	if _, err := trustpack.NewScope(nil); !errors.Is(err, trustpack.ErrConfig) {
		t.Fatalf("NewScope(nil): %v", err)
	}
	if _, err := trustpack.NewAuthority(nil); !errors.Is(err, trustpack.ErrConfig) {
		t.Fatalf("NewAuthority(nil): %v", err)
	}
}

// A zero value is not an adapter: never "in scope", never "allowed" (INV-16).
func TestAZeroScopeAndAZeroAuthorityAnswerNothing(t *testing.T) {
	var s trustpack.Scope
	if ok, err := s.InScope(ctx(t), tenant, firstNodeWithProfile(t).ID); ok || !errors.Is(err, trustpack.ErrConfig) {
		t.Fatalf("InScope = %v, %v", ok, err)
	}
	var a trustpack.Authority
	if ok, err := a.RoleAllowed(ctx(t), firstNodeWithProfile(t).ID, "customer"); ok || !errors.Is(err, trustpack.ErrConfig) {
		t.Fatalf("RoleAllowed = %v, %v", ok, err)
	}
}

// T5: a node of the pack that carries a trust profile is in scope; the pack's node with no profile (the out-of-scope request) is not; an unknown or empty node is not.
func TestScopeIsTheNodesThePackGivesATrustProfile(t *testing.T) {
	s := newScope(t, loadReal(t))
	for _, n := range readRawPack(t).Nodes {
		want := n.TrustProfile != nil
		for _, ten := range []string{tenant, otherTenant} {
			if got, err := s.InScope(ctx(t), ten, n.ID); err != nil || got != want {
				t.Errorf("InScope(%s, %s) = %v, %v; want %v", ten, n.ID, got, err, want)
			}
		}
	}
	for _, id := range []string{"x.unknown.node", "", " ", upper(firstNodeWithProfile(t).ID), firstNodeWithProfile(t).ID + " "} {
		if got, err := s.InScope(ctx(t), tenant, id); err != nil || got {
			t.Errorf("InScope(%q) = %v, %v; want false", id, got, err)
		}
	}
}

func TestScopeAsksAboutATenant(t *testing.T) {
	s := newScope(t, loadReal(t))
	if got, err := s.InScope(ctx(t), "", firstNodeWithProfile(t).ID); got || !errors.Is(err, trustpack.ErrSession) {
		t.Fatalf("InScope(no tenant) = %v, %v; want false and ErrSession", got, err)
	}
}

// T6: the role must be in the `roles_allowed` of the node's trust profile, exactly. Every node against every role of the matrix, the expectation read independently from the pack.
func TestAuthorityRoleMatrixOverEveryNodeOfThePack(t *testing.T) {
	a := newAuthority(t, loadReal(t))
	for _, n := range readRawPack(t).Nodes {
		for _, role := range roles {
			want := n.TrustProfile != nil && contains(profileRoles(t, *n.TrustProfile), role)
			if got, err := a.RoleAllowed(ctx(t), n.ID, role); err != nil || got != want {
				t.Errorf("RoleAllowed(%s, %q) = %v, %v; want %v", n.ID, role, got, err, want)
			}
		}
	}
}

// A node with no trust profile has no authority for any role; an unknown node neither.
func TestAuthorityOfANodeWithNoProfileOrNoNodeIsNeverAllowed(t *testing.T) {
	a := newAuthority(t, loadReal(t))
	for _, id := range []string{nodeWithoutProfile(t).ID, "x.unknown.node", ""} {
		for _, role := range append([]string{"customer", "ops"}, roles...) {
			if got, err := a.RoleAllowed(ctx(t), id, role); err != nil || got {
				t.Errorf("RoleAllowed(%q, %q) = %v, %v; want false", id, role, got, err)
			}
		}
	}
}

// A profile that lists no role allows no role (never "everyone").
func TestAnEmptyRoleListAllowsNobody(t *testing.T) {
	m := files(t)
	m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
		for _, tp := range p["trust_profiles"].([]any) {
			tp.(map[string]any)["authority"].(map[string]any)["roles_allowed"] = []any{}
		}
	})
	a := newAuthority(t, mustLoadFiles(t, m))
	for _, role := range roles {
		if got, err := a.RoleAllowed(ctx(t), firstNodeWithProfile(t).ID, role); err != nil || got {
			t.Errorf("RoleAllowed(%q) = %v, %v; want false", role, got, err)
		}
	}
}

// Changing a role in the pack's data changes the answer: nothing is written in the adapter.
func TestAuthorityFollowsThePackData(t *testing.T) {
	m := files(t)
	m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
		for _, tp := range p["trust_profiles"].([]any) {
			tp.(map[string]any)["authority"].(map[string]any)["roles_allowed"] = []any{"auditor_synthetic"}
		}
	})
	a := newAuthority(t, mustLoadFiles(t, m))
	node := firstNodeWithProfile(t).ID
	if got, err := a.RoleAllowed(ctx(t), node, "auditor_synthetic"); err != nil || !got {
		t.Fatalf("the role the data lists is refused: %v, %v", got, err)
	}
	if got, err := a.RoleAllowed(ctx(t), node, "customer"); err != nil || got {
		t.Fatalf("a role the data no longer lists is allowed: %v, %v", got, err)
	}
}
