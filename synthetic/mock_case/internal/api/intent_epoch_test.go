package api_test

import (
	"strings"
	"testing"
)

// The pack's case entity (packs/fintech_v1.json, SoT §15.3 success predicate) reads three fields the first
// mock-case did not return: created_by_intent, epoch, and the status ROUTED. mock-case now stores and returns
// them (reconciliation of the pack and mock-case; the pack is not changed). SYNTHETIC.

func TestACaseCarriesItsIntentAndEpoch(t *testing.T) {
	e := newEnv(t)
	body := caseBody()
	body["created_by_intent"], body["epoch"] = "int_88213", 3
	r := e.runtime("POST", "/v1/cases", newKey("idk_1"), body)
	if r.status != 201 {
		t.Fatalf("%d %s", r.status, r.raw)
	}
	if r.str("created_by_intent") != "int_88213" || !strings.Contains(string(r.raw), `"epoch":3`) {
		t.Fatalf("the case must return created_by_intent and epoch: %s", r.raw)
	}
	read := e.runtime("GET", "/v1/cases/"+r.str("case_id")+"?tenant_id=ten_a", nil, nil)
	if read.str("created_by_intent") != "int_88213" || !strings.Contains(string(read.raw), `"epoch":3`) {
		t.Fatalf("a read returns them too: %s", read.raw)
	}
	for _, item := range e.runtime("GET", "/v1/cases?tenant_id=ten_a&principal_id=usr_1", nil, nil).items() {
		if item["created_by_intent"] != "int_88213" || item["epoch"] != float64(3) {
			t.Fatalf("a list item returns them too: %v", item)
		}
	}
}

func TestIntentAndEpochAreRequiredAndValidated(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name string
		edit func(map[string]any)
	}{
		{"intent missing", func(b map[string]any) { delete(b, "created_by_intent") }},
		{"intent empty", func(b map[string]any) { b["created_by_intent"] = "" }},
		{"intent with a space", func(b map[string]any) { b["created_by_intent"] = "int 1" }},
		{"epoch missing", func(b map[string]any) { delete(b, "epoch") }},
		{"epoch negative", func(b map[string]any) { b["epoch"] = -1 }},
		{"epoch a string", func(b map[string]any) { b["epoch"] = "0" }},
		{"epoch a fraction", func(b map[string]any) { b["epoch"] = 1.5 }},
		{"epoch null", func(b map[string]any) { b["epoch"] = nil }},
		{"epoch above 2^53-1", func(b map[string]any) { b["epoch"] = 9007199254740992 }},
	}
	for _, c := range cases {
		body := caseBody()
		c.edit(body)
		r := e.runtime("POST", "/v1/cases", newKey("idk_bad"), body)
		if r.status != 400 {
			t.Errorf("%s: want 400, got %d %s", c.name, r.status, r.raw)
		}
	}
	if n := e.count("cases"); n != 0 {
		t.Fatalf("a refused create must leave no case: %d", n)
	}
}

func TestIntentAndEpochAreTheIdempotentPayload(t *testing.T) {
	e := newEnv(t)
	if r := e.createCase("idk_1"); r.status != 201 {
		t.Fatalf("%d %s", r.status, r.raw)
	}
	for name, edit := range map[string]func(map[string]any){
		"another intent": func(b map[string]any) { b["created_by_intent"] = "int_other" },
		"another epoch":  func(b map[string]any) { b["epoch"] = 1 },
	} {
		b := caseBody()
		edit(b)
		if r := e.runtime("POST", "/v1/cases", newKey("idk_1"), b); r.status != 409 || r.errCode() != "IDEMPOTENCY_CONFLICT" {
			t.Errorf("%s under the same key must be a conflict: %d %s", name, r.status, r.raw)
		}
	}
	if again := e.createCase("idk_1"); again.status != 200 || again.header.Get("Idempotent-Replay") != "true" {
		t.Fatalf("the identical payload is still a replay: %d %s", again.status, again.raw)
	}
}

func TestRoutingMovesAnOpenCaseToRoutedAndNeverFurther(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_1")
	if c.str("status") != "OPEN" {
		t.Fatalf("a new case is OPEN: %s", c.raw)
	}
	path := "/v1/cases/" + c.str("case_id") + "/route"
	stale := e.runtime("POST", path, nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v9"})
	if stale.status != 409 {
		t.Fatalf("%d %s", stale.status, stale.raw)
	}
	if read := e.runtime("GET", "/v1/cases/"+c.str("case_id")+"?tenant_id=ten_a", nil, nil); read.str("status") != "OPEN" {
		t.Fatalf("a refused route must leave the case OPEN: %s", read.raw)
	}
	ok := e.runtime("POST", path, nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v1"})
	if ok.status != 200 || ok.str("status") != "ROUTED" || ok.str("owner_queue") != "DISPUTES" || ok.str("version_token") != "v2" {
		t.Fatalf("%d %s", ok.status, ok.raw)
	}
	if read := e.runtime("GET", "/v1/cases/"+c.str("case_id")+"?tenant_id=ten_a", nil, nil); read.str("status") != "ROUTED" || read.str("version_token") != "v2" {
		t.Fatalf("a later read must show the stored status ROUTED: %s", read.raw)
	}
	for _, item := range e.runtime("GET", "/v1/cases?tenant_id=ten_a&principal_id=usr_1", nil, nil).items() {
		if item["status"] != "ROUTED" {
			t.Fatalf("a list item must show ROUTED too: %v", item)
		}
	}
	again := e.runtime("POST", path, nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "FRAUD_REVIEW", "expected_version_token": "v2"})
	if again.status != 200 || again.str("status") != "ROUTED" || again.str("owner_queue") != "FRAUD_REVIEW" {
		t.Fatalf("re-routing keeps ROUTED: %d %s", again.status, again.raw)
	}
}
