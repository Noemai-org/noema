package main

// LANE-5 (A709, A774, interim, relayed; REQ-EXE-006, REQ-AUD-006; INV-14, AGENT-13, AGENT-16): the Controller learns which params of a plan step the receipt carries, and how each is
// written, from the pack's `outcome_bindings` and nothing of the core's own. A param the binding's `disclosure` does not list is HASHED. Every value is SYNTHETIC.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

func TestThePolicyIsBuiltFromThePacksBindingsAndEveryUnlistedParamIsHashed(t *testing.T) {
	v := tiVerified(t)
	p, err := stepParamPolicyFromPack(v.Pack)
	if err != nil || p == nil {
		t.Fatalf("policy: %v, %v", p, err)
	}
	got, err := p.Bound("fin.cap.route_to_internal_owner")
	want := map[string]controller.ParamHandling{"case_id": controller.HandlingHashed, "owner_queue": controller.HandlingHashed}
	if err != nil || len(got) != len(want) || got["case_id"] != want["case_id"] || got["owner_queue"] != want["owner_queue"] {
		t.Errorf("bound %v, %v; want %v (the real pack lists no disclosure: all HASHED)", got, err, want)
	}
	for _, none := range []string{"fin.cap.lookup_transaction", "fin.cap.not_declared"} {
		if got, err := p.Bound(none); err != nil || len(got) != 0 {
			t.Errorf("%s: bound %v, %v; want none and no error", none, got, err)
		}
	}
	// A binding with no PLAN_PARAM source (SUBJECT, INTENT_ID, EPOCH only) binds no param.
	if got, err := p.Bound("fin.cap.log_complaint_reference"); err != nil || len(got) != 0 {
		t.Errorf("complaint: bound %v, %v; want none (it binds SUBJECT and EPOCH only)", got, err)
	}
}

func withDisclosure(t *testing.T, pack []byte, capability string, disclosure map[string]any) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(pack, &doc); err != nil {
		t.Fatal(err)
	}
	for _, b := range doc["outcome_bindings"].([]any) {
		if m := b.(map[string]any); m["capability_id"] == capability {
			m["disclosure"] = disclosure
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestADisclosureOfTheBindingIsHonouredAndAnUnknownOneIsRefused(t *testing.T) {
	v := tiVerified(t)
	const cap = "fin.cap.record_investigation_evidence"
	p, err := stepParamPolicyFromPack(withDisclosure(t, v.Pack, cap, map[string]any{"payload": "VERBATIM", "case_id": "MASKED"}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Bound(cap)
	if err != nil || got["payload"] != controller.HandlingVerbatim || got["case_id"] != controller.HandlingMasked {
		t.Errorf("bound %v, %v; want payload VERBATIM and case_id MASKED", got, err)
	}
	for _, bad := range []any{"RAW", "", 1, nil} {
		if p, err := stepParamPolicyFromPack(withDisclosure(t, v.Pack, cap, map[string]any{"payload": bad})); err == nil || p != nil {
			t.Errorf("disclosure %v: %v, %v; want an error and no policy (fail closed)", bad, p, err)
		}
	}
}

func TestAPackThatCannotBeReadGivesNoPolicy(t *testing.T) {
	for name, b := range map[string][]byte{"empty": nil, "not json": []byte("{"), "bindings not an array": []byte(`{"outcome_bindings":{}}`)} {
		if p, err := stepParamPolicyFromPack(b); err == nil || p != nil {
			t.Errorf("%s: %v, %v; want an error and no policy", name, p, err)
		}
	}
	// A pack with no bindings is a pack with none: an empty policy, never an error.
	if p, err := stepParamPolicyFromPack([]byte(`{"pack_id":"x"}`)); err != nil || p == nil {
		t.Errorf("no bindings: %v, %v; want an empty policy", p, err)
	}
}

func TestTheExecutionSideNamesAMissingPolicyAndGivesEachControllerTheOneItWasGiven(t *testing.T) {
	d, parts := sideParts(t, "ten_aa", "ten_bb")
	s := sideSeams()
	s.StepParams = nil
	side, err := buildExecutionSide(sideEnv(d, nil), parts, s)
	if err != nil || side == nil || side.Executor != nil || len(side.Controllers) != 0 {
		t.Fatalf("no policy: side %+v, err %v; want no Executor and no controller", side, err)
	}
	if len(side.Gaps) != 1 || side.Gaps[0] != "execution: step param policy missing" || strings.ContainsAny(side.Gaps[0], "0123456789") {
		t.Errorf("gaps %q; want exactly the one fixed line", side.Gaps)
	}
	policy, err := stepParamPolicyFromPack(tiVerified(t).Pack)
	if err != nil {
		t.Fatal(err)
	}
	s.StepParams = policy
	side, err = buildExecutionSide(sideEnv(d, nil), parts, s)
	if err != nil || side == nil || len(side.Gaps) != 0 || len(side.Controllers) != 2 {
		t.Fatalf("side %+v, err %v; want two controllers and no gap", side, err)
	}
	for tenant, ctl := range side.Controllers {
		if ctl.StepParams != policy {
			t.Errorf("%s: the controller's policy is not the one given", tenant)
		}
	}
}
