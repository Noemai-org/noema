package confirm_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

// TRU-07 (defence in depth for the confirmation route): MAKER_CHECKER is met only by a principal who is not the requester AND holds
// the approver role. REQ-TRU-009, INV-25, EC-U04, SoT 43.2a.

// ApproverRole is the platform role of edge/session, not a second spelling of it.
func TestApproverRoleIsTheSessionRole(t *testing.T) {
	if confirm.ApproverRole != string(session.RoleApprover) {
		t.Fatalf("ApproverRole %q, session.RoleApprover %q", confirm.ApproverRole, session.RoleApprover)
	}
	if string(session.RoleApprover) != "approver" {
		t.Fatalf("session.RoleApprover is %q", session.RoleApprover)
	}
}

func eligibleBy(t *testing.T, requirement, requester, approver string, roles []string) bool {
	t.Helper()
	req := request(requirement)
	req.RequesterID = requester
	got, err := confirm.Resolve(req, event(approver, roles, confirm.Accept, 10), base)
	if err != nil {
		t.Fatal(err)
	}
	switch got {
	case confirm.Proceed:
		return true
	case confirm.NotEligible:
		return false
	}
	t.Fatalf("unexpected resolution %s", got)
	return false
}

func TestMakerCheckerNeedsTheApproverRole(t *testing.T) {
	cases := []struct {
		name      string
		requester string
		approver  string
		roles     []string
		want      bool
	}{
		{"another principal with the approver role", "prn_req", "prn_boss", []string{"approver"}, true},
		{"the approver role among others", "prn_req", "prn_boss", []string{"customer", "approver"}, true},
		{"another principal, a customer session", "prn_req", "prn_cust2", []string{"customer"}, false},
		{"another principal, an ops session", "prn_req", "prn_ops", []string{"ops"}, false},
		{"another principal, an audit session", "prn_req", "prn_aud", []string{"audit"}, false},
		{"another principal, no roles (nil)", "prn_req", "prn_boss", nil, false},
		{"another principal, no roles (empty)", "prn_req", "prn_boss", []string{}, false},
		{"another principal, one empty role name", "prn_req", "prn_boss", []string{""}, false},
		{"the requester with the approver role", "prn_req", "prn_req", []string{"approver"}, false},
		{"the requester without a role", "prn_req", "prn_req", nil, false},
		{"approver role in capitals", "prn_req", "prn_boss", []string{"APPROVER"}, false},
		{"approver role capitalised", "prn_req", "prn_boss", []string{"Approver"}, false},
		{"approver role with a leading space", "prn_req", "prn_boss", []string{" approver"}, false},
		{"approver role with a trailing space", "prn_req", "prn_boss", []string{"approver "}, false},
		{"a pack role of the same family", "prn_req", "prn_boss", []string{"approvers"}, false},
		{"no approver principal, with the role", "prn_req", "", []string{"approver"}, false},
		{"no requester recorded, with the role", "", "prn_boss", []string{"approver"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eligibleBy(t, "MAKER_CHECKER", c.requester, c.approver, c.roles); got != c.want {
				t.Fatalf("MAKER_CHECKER requester %q approver %q roles %q: eligible %v, want %v", c.requester, c.approver, c.roles, got, c.want)
			}
		})
	}
}

// The expected answer, written independently of the code under test.
func wantEligible(requirement, requester, approver string, roles []string) bool {
	has := func(role string) bool {
		for _, r := range roles {
			if r == role && r != "" {
				return true
			}
		}
		return false
	}
	switch {
	case approver == "":
		return false
	case requirement == "SELF":
		return approver == requester
	case requirement == "MAKER_CHECKER":
		return requester != "" && approver != requester && has("approver")
	case len(requirement) > 5 && requirement[:5] == "ROLE:":
		return has(requirement[5:])
	}
	return false
}

// Principal x roles x requirement x requester, every combination.
func TestEligibilityCrossProduct(t *testing.T) {
	requesters := []string{"prn_req", ""}
	approvers := []string{"prn_req", "prn_boss", ""}
	roleSets := [][]string{nil, {}, {""}, {"approver"}, {"customer"}, {"ops"}, {"audit"}, {"Approver"}, {"APPROVER"}, {" approver"}, {"supervisor"},
		{"Supervisor"}, {"customer", "approver"}, {"supervisor", "approver"}, {"", "approver"}, {"", ""}}
	requirements := []string{"SELF", "MAKER_CHECKER", "ROLE:supervisor", "ROLE:approver", "ROLE:", "ROLE:Supervisor", "role:supervisor", "maker_checker", "Self", "ANYONE", "", " MAKER_CHECKER", "MAKER_CHECKER ", "ROLE"}
	n, yes := 0, 0
	for _, requirement := range requirements {
		for _, requester := range requesters {
			for _, approver := range approvers {
				for _, roles := range roleSets {
					n++
					want := wantEligible(requirement, requester, approver, roles)
					if want {
						yes++
					}
					if got := eligibleBy(t, requirement, requester, approver, roles); got != want {
						t.Fatalf("requirement %q requester %q approver %q roles %q: eligible %v, want %v", requirement, requester, approver, roles, got, want)
					}
				}
			}
		}
	}
	if n != 14*2*3*16 || yes < 20 {
		t.Fatalf("%d combinations, %d eligible: the table lost its shape", n, yes)
	}
}

// ROLE:x with an empty x is a malformed requirement, and an empty role name in the set never matches it.
func TestEmptyRoleNeverSatisfiesRole(t *testing.T) {
	for _, roles := range [][]string{nil, {""}, {"", "supervisor"}} {
		if eligibleBy(t, "ROLE:", "prn_req", "prn_boss", roles) {
			t.Fatalf("ROLE: with an empty name accepted roles %q", roles)
		}
	}
}

// REQ-TRU-009, INV-25 (property, fixed seed): no principal without the approver role is ever eligible for MAKER_CHECKER, and nobody
// is eligible without a principal of their own.
func TestNoPrincipalWithoutTheApproverRoleIsEverMakerChecker(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	pool := []string{"", "approver", "Approver", "APPROVER", "approver ", " approver", "customer", "ops", "audit", "supervisor", "agent", "approvers", "role:approver", "ROLE:approver"}
	ids := []string{"", "prn_req", "prn_boss", "prn_other", "usr_cust"}
	withRole, without := 0, 0
	for i := 0; i < 30000; i++ {
		roles := make([]string, r.Intn(5))
		for j := range roles {
			roles[j] = pool[r.Intn(len(pool))]
		}
		requester, approver := ids[r.Intn(len(ids))], ids[r.Intn(len(ids))]
		got := eligibleBy(t, "MAKER_CHECKER", requester, approver, roles)
		holds := false
		for _, role := range roles {
			holds = holds || role == "approver"
		}
		if holds {
			withRole++
		} else {
			without++
		}
		if got && !holds {
			t.Fatalf("eligible without the approver role: requester %q approver %q roles %q", requester, approver, roles)
		}
		if got && (approver == "" || approver == requester) {
			t.Fatalf("eligible without being a second principal: requester %q approver %q", requester, approver)
		}
		if got != wantEligible("MAKER_CHECKER", requester, approver, roles) {
			t.Fatalf("answer differs from the independent rule: %s", fmt.Sprint(requester, approver, roles))
		}
	}
	if withRole < 3000 || without < 3000 {
		t.Fatalf("the generator is lopsided: %d with the role, %d without", withRole, without)
	}
}

// A decline is not counted for a customer session either: the approver-role check comes before the reply.
func TestDeclineByACustomerIsNotEligible(t *testing.T) {
	req := request("MAKER_CHECKER")
	got, err := confirm.Resolve(req, event("prn_cust2", []string{"customer"}, confirm.Decline, 10), base)
	if err != nil || got != confirm.NotEligible {
		t.Fatalf("got %s (%v)", got, err)
	}
}
