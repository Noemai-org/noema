package confirm_test

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

var base = confirm.BindingInput{PrincipalID: "prn_01J9", IntentHash: "sha256:aa", CapabilityID: "fin.cap.create_escalation_case", TargetRef: "txn_01J9X",
	ParamsHash: "sha256:bb", EvidenceSetHash: "sha256:cc", PlanHash: "sha256:dd", PolicyVersion: "3.1"}

// Computed independently with Python (json.dumps with sorted keys and no spaces, then hashlib.sha256).
const golden = "sha256:745b7dfc8cc726e947c76720efe4a6497bb14c5eb982789aa9ab334be1ebf79e"

func TestBindingHashGolden(t *testing.T) {
	got, err := confirm.BindingHash(base)
	if err != nil || got != golden {
		t.Fatalf("%s %v", got, err)
	}
}

func TestBindingHashRejectsValuesNeedingEscapes(t *testing.T) {
	for _, bad := range []string{"", "a b", "a\"b", "a\\b", "a\nb", "é", "a/b"} {
		in := base
		in.TargetRef = bad
		if _, err := confirm.BindingHash(in); !errors.Is(err, confirm.ErrUnsafeValue) {
			t.Fatalf("%q accepted", bad)
		}
	}
}

// Every one of the eight bound values changes the hash.
func TestEveryBoundValueChangesTheHash(t *testing.T) {
	v := reflect.ValueOf(&base).Elem()
	for i := 0; i < v.NumField(); i++ {
		in := base
		reflect.ValueOf(&in).Elem().Field(i).SetString("changed")
		got, _ := confirm.BindingHash(in)
		if got == golden {
			t.Fatalf("changing %s did not change the hash", v.Type().Field(i).Name)
		}
	}
}

func request(requirement string) confirm.Request {
	return confirm.Request{ID: "cfr_1", Binding: base, Requirement: requirement, RequesterID: "prn_req", ExpiresAt: 1000}
}

func event(approver string, roles []string, resp confirm.Response, at int64) confirm.Event {
	return confirm.Event{RequestID: "cfr_1", ApproverID: approver, ApproverRoles: roles, Response: resp, At: at}
}

func TestResolveTable(t *testing.T) {
	changedPolicy, changedPlan := base, base
	changedPolicy.PolicyVersion, changedPlan.PlanHash = "3.2", "sha256:ee"
	cases := []struct {
		name  string
		req   confirm.Request
		ev    confirm.Event
		fresh confirm.BindingInput
		want  confirm.Resolution
	}{
		{"self accepts", request("SELF"), event("prn_req", nil, confirm.Accept, 10), base, confirm.Proceed},
		{"self cannot be someone else", request("SELF"), event("prn_other", nil, confirm.Accept, 10), base, confirm.NotEligible},
		{"maker-checker, a different approver", request("MAKER_CHECKER"), event("prn_boss", nil, confirm.Accept, 10), base, confirm.Proceed},
		{"maker-checker, self-approval", request("MAKER_CHECKER"), event("prn_req", nil, confirm.Accept, 10), base, confirm.NotEligible},
		{"maker-checker, nobody", request("MAKER_CHECKER"), event("", nil, confirm.Accept, 10), base, confirm.NotEligible},
		{"role present", request("ROLE:supervisor"), event("prn_boss", []string{"supervisor"}, confirm.Accept, 10), base, confirm.Proceed},
		{"role absent", request("ROLE:supervisor"), event("prn_boss", []string{"agent"}, confirm.Accept, 10), base, confirm.NotEligible},
		{"unknown requirement", request("ANYONE"), event("prn_boss", nil, confirm.Accept, 10), base, confirm.NotEligible},
		{"decline by an eligible approver", request("MAKER_CHECKER"), event("prn_boss", nil, confirm.Decline, 10), base, confirm.Declined},
		{"decline by an ineligible approver does not count", request("MAKER_CHECKER"), event("prn_req", nil, confirm.Decline, 10), base, confirm.NotEligible},
		{"late acceptance", request("SELF"), event("prn_req", nil, confirm.Accept, 1001), base, confirm.Expired},
		{"acceptance at the expiry instant", request("SELF"), event("prn_req", nil, confirm.Accept, 1000), base, confirm.Proceed},
		{"binding changed", request("SELF"), event("prn_req", nil, confirm.Accept, 10), changedPlan, confirm.BindingChanged},
		{"policy version changed", request("SELF"), event("prn_req", nil, confirm.Accept, 10), changedPolicy, confirm.PolicyChanged},
		{"another request", request("SELF"), confirm.Event{RequestID: "cfr_9", ApproverID: "prn_req", Response: confirm.Accept, At: 10}, base, confirm.WrongRequest},
		{"unknown reply", request("SELF"), event("prn_req", nil, "MAYBE", 10), base, confirm.WrongRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := confirm.Resolve(c.req, c.ev, c.fresh)
			if err != nil || got != c.want {
				t.Fatalf("want %s, got %s (%v)", c.want, got, err)
			}
			if got.MayLeadToGrant() != (c.want == confirm.Proceed) {
				t.Fatalf("MayLeadToGrant wrong for %s", got)
			}
		})
	}
}

// INV-25, REQ-TRU-009: only a matching binding, an eligible approver, an acceptance and no expiry lead towards a grant.
func TestOnlyAFullMatchProceeds(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	ids := []string{"prn_req", "prn_boss", "prn_other"}
	reqs := []string{"SELF", "MAKER_CHECKER", "ROLE:supervisor", "ROLE:agent"}
	proceeded := 0
	for i := 0; i < 20000; i++ {
		req := request(reqs[r.Intn(len(reqs))])
		ev := event(ids[r.Intn(len(ids))], [][]string{nil, {"supervisor"}, {"agent", "supervisor"}}[r.Intn(3)], []confirm.Response{confirm.Accept, confirm.Decline}[r.Intn(2)], int64(r.Intn(1500)))
		fresh := base
		if r.Intn(3) == 0 {
			reflect.ValueOf(&fresh).Elem().Field(r.Intn(8)).SetString("changed")
		}
		got, err := confirm.Resolve(req, ev, fresh)
		if err != nil {
			t.Fatal(err)
		}
		if got != confirm.Proceed {
			continue
		}
		proceeded++
		sameBinding := fresh == base
		eligible := (req.Requirement == "SELF" && ev.ApproverID == req.RequesterID) ||
			(req.Requirement == "MAKER_CHECKER" && ev.ApproverID != req.RequesterID) ||
			(req.Requirement == "ROLE:supervisor" && contains(ev.ApproverRoles, "supervisor")) ||
			(req.Requirement == "ROLE:agent" && contains(ev.ApproverRoles, "agent"))
		if !sameBinding || !eligible || ev.Response != confirm.Accept || ev.At > req.ExpiresAt {
			t.Fatalf("proceeded without a full match: %+v %+v same=%v", req, ev, sameBinding)
		}
	}
	if proceeded < 500 {
		t.Fatalf("only %d acceptances reached the end", proceeded)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
