package api_test

import (
	"fmt"
	"testing"
	"time"
)

// Noema's own Tushe-environment checks, Noema side against mock-case (internal; PERD §34.5 [REC], plan task ENV-14).
// Only the case-system half of TEN-03 and TEN-04 is shown here: mock-case is the write target of the POC (DEC-2026-10-12 D1).
// Each test prints one TEN_RESULT line so the gate record can quote the run seed. The seed is the fault plan's seed
// (plan() in harness_test.go); a check without a fault plan says "none".

const tenSeed = 42 // the seed plan() puts in every fault plan of this package

func tenResult(t *testing.T, id, seed string) {
	t.Helper()
	t.Logf("TEN_RESULT id=%s seed=%s pass=%t", id, seed, !t.Failed())
}

// TEN-03 (REQ-ENV-008, INV-27): every response that describes a case carries a version token, and a write that names a
// stale expected version is rejected and changes nothing at all: the whole state digest is the same before and after.
func TestTEN03EveryCaseReadCarriesATokenAndAStaleWriteChangesNothing(t *testing.T) {
	defer tenResult(t, "TEN-03", "none")
	e := newEnv(t)
	created := e.createCase("idk_1")
	if created.status != 201 {
		t.Fatalf("create: %d %s", created.status, created.raw)
	}
	id := created.str("case_id")
	replay := e.createCase("idk_1")
	e.runtime("POST", "/v1/cases", newKey("idk_2"), caseBodyFor("TX2"))

	tokens := map[string]string{
		"create 201":    created.str("version_token"),
		"create replay": replay.str("version_token"),
		"get by id":     e.runtime("GET", "/v1/cases/"+id+"?tenant_id=ten_a", nil, nil).str("version_token"),
	}
	for _, list := range []string{
		"/v1/cases?tenant_id=ten_a&idempotency_key=idk_1",
		"/v1/cases?tenant_id=ten_a&principal_id=usr_1",
	} {
		items := e.runtime("GET", list, nil, nil).items()
		if len(items) == 0 {
			t.Fatalf("%s returned nothing", list)
		}
		for i, it := range items {
			tokens[fmt.Sprintf("%s [%d]", list, i)], _ = it["version_token"].(string)
		}
	}
	for where, tok := range tokens {
		if tok == "" {
			t.Errorf("%s carries no version token", where)
		}
	}

	path := "/v1/cases/" + id + "/route"
	stateBefore, _ := e.digest()
	stale := e.runtime("POST", path, nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v9"})
	if stale.status != 409 || stale.errCode() != "VERSION_MISMATCH" {
		t.Fatalf("a stale expected version must be rejected: %d %s", stale.status, stale.raw)
	}
	if stateAfter, _ := e.digest(); stateAfter != stateBefore {
		t.Fatalf("a rejected stale write changed the state: %s -> %s", stateBefore, stateAfter)
	}

	routed := e.runtime("POST", path, nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": created.str("version_token")})
	if routed.status != 200 || routed.str("version_token") == "" || routed.str("version_token") == created.str("version_token") {
		t.Fatalf("a landed write carries a new token: %d %s", routed.status, routed.raw)
	}
}

// TEN-04 (REQ-ENV-008, REQ-ENV-004, INV-07, EC-E02): a case committed with its response lost is found by its idempotency
// key, through both lost-response faults, and the same plan and seed reproduce the same state and fault trace.
func TestTEN04ACommittedCaseWithALostResponseIsFoundByKey(t *testing.T) {
	defer tenResult(t, "TEN-04", fmt.Sprint(tenSeed))
	for _, c := range []struct{ effect, params string }{
		{"DROP_RESPONSE", `{}`},
		{"TIMEOUT_AFTER_COMMIT", `{"delay_ms":200}`},
	} {
		t.Run(c.effect, func(t *testing.T) {
			var digests [2][2]string
			for run := range digests {
				e := newEnv(t)
				e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, c.effect, c.params), simClock))
				if _, err := e.createWith(impatient(100*time.Millisecond), "idk_1"); err == nil {
					t.Fatal("the response must be lost")
				}
				e.waitForCount("cases", 1, 5*time.Second, "the case is committed although the response is lost")
				found := e.runtime("GET", "/v1/cases?tenant_id=ten_a&idempotency_key=idk_1", nil, nil)
				items := found.items()
				if found.status != 200 || len(items) != 1 || found.json["complete"] != true {
					t.Fatalf("the case must be found by its key: %d %s", found.status, found.raw)
				}
				if tok, _ := items[0]["version_token"].(string); tok == "" {
					t.Fatalf("found without a version token: %v", items[0])
				}
				retry := e.createCase("idk_1")
				if retry.status != 200 || retry.header.Get("Idempotent-Replay") != "true" || retry.str("case_id") != items[0]["case_id"] {
					t.Fatalf("the retry must replay the committed case: %d %s", retry.status, retry.raw)
				}
				if e.count("cases") != 1 {
					t.Fatal("exactly one effect")
				}
				digests[run][0], digests[run][1] = e.digest()
			}
			if digests[0] != digests[1] {
				t.Fatalf("the same plan and seed must give the same state and fault trace: %v vs %v", digests[0], digests[1])
			}
		})
	}
}
