package api_test

import (
	"fmt"
	"sync"
	"testing"
)

// Create-if-absent (create_escalation_case stays CONDITIONAL_WRITE): a create is conditional on "no case exists for this
// transaction and epoch". The target enforces it atomically; the idempotency key makes the same create safe to repeat.
// The guarantee is the mock target's. Tushe's create semantics are an open item for the founders.

func TestCreateWithoutThePreconditionHasNoRoute(t *testing.T) {
	e := newEnv(t)
	r := e.runtime("POST", "/v1/cases", key("idk_1"), caseBody())
	if r.status != 428 || r.errCode() != "PRECONDITION_REQUIRED" || r.json["error"].(map[string]any)["retryable"] != false {
		t.Fatalf("an unconditional create is refused: %d %s", r.status, r.raw)
	}
	for _, v := range []string{`"v1"`, "W/*", "x"} {
		r := e.runtime("POST", "/v1/cases", map[string]string{"Idempotency-Key": "idk_1", "If-None-Match": v}, caseBody())
		if r.status != 400 || r.errCode() != "INVALID_REQUEST" {
			t.Errorf("If-None-Match %s is not the precondition: %d %s", v, r.status, r.raw)
		}
	}
	if e.count("cases") != 0 {
		t.Fatal("a refused create writes nothing")
	}
}

// Duplicate create: the same key and the same payload, repeated, is one case.
func TestDuplicateCreateIsOneCase(t *testing.T) {
	e := newEnv(t)
	first := e.createCase("idk_1")
	again := e.createCase("idk_1")
	if first.status != 201 || again.status != 200 || again.header.Get("Idempotent-Replay") != "true" || again.str("case_id") != first.str("case_id") {
		t.Fatalf("first %d %s, repeat %d %s", first.status, first.raw, again.status, again.raw)
	}
	if e.count("cases") != 1 {
		t.Fatalf("%d cases", e.count("cases"))
	}
}

// Conflicting creates: the same key with another payload, and another key for a subject that has a case.
func TestConflictingCreatesAreRejectedWithTheRightError(t *testing.T) {
	e := newEnv(t)
	first := e.createCase("idk_1")

	other := caseBody()
	other["priority"] = "HIGH"
	sameKey := e.runtime("POST", "/v1/cases", newKey("idk_1"), other)
	if sameKey.status != 409 || sameKey.errCode() != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("same key, other payload: %d %s", sameKey.status, sameKey.raw)
	}

	rival := e.createCase("idk_2")
	if rival.status != 412 || rival.errCode() != "PRECONDITION_FAILED" || rival.json["error"].(map[string]any)["retryable"] != false {
		t.Fatalf("other key, same subject and epoch: %d %s", rival.status, rival.raw)
	}
	if detail(rival, "existing_case_id") != first.str("case_id") || detail(rival, "existing_version_token") != first.str("version_token") {
		t.Fatalf("the refusal names the case that exists: %s", rival.raw)
	}
	if again := e.createCase("idk_2"); again.status != 412 || detail(again, "existing_case_id") != first.str("case_id") {
		t.Fatalf("a refusal repeats: %d %s", again.status, again.raw)
	}
	if e.count("cases") != 1 {
		t.Fatalf("%d cases: a refused create writes nothing", e.count("cases"))
	}

	// Another epoch, another transaction and another tenant are other subjects.
	epoch := caseBody()
	epoch["epoch"] = 1
	tenant := caseBody()
	tenant["tenant_id"] = "ten_b"
	for name, body := range map[string]map[string]any{"epoch": epoch, "transaction": caseBodyFor("TX00002"), "tenant": tenant} {
		if r := e.runtime("POST", "/v1/cases", newKey("idk_"+name), body); r.status != 201 {
			t.Errorf("%s is another subject: %d %s", name, r.status, r.raw)
		}
	}
	// The refusals spent no reference number.
	if next := e.runtime("POST", "/v1/cases", newKey("idk_next"), caseBodyFor("TX00003")); next.status != 201 || next.str("reference") != "CMP-2026-00005" {
		t.Fatalf("references stay gapless across refusals: %d %s", next.status, next.raw)
	}
}

// Concurrent creates under load: many callers, each with its own key, race to create for one subject. Exactly one
// wins; every other caller is refused and is told which case won.
func TestConcurrentCreatesForOneSubjectUnderLoadYieldOneCase(t *testing.T) {
	e := newEnv(t)
	const callers = 64
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	refused := map[string]int{}
	var winner string
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := e.send(e.client, "POST", e.caseURL+"/v1/cases", runtimeToken, newKey(fmt.Sprintf("idk_load_%d", i)), caseBody())
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case r.status == 201:
				won++
				winner = r.str("case_id")
			case r.status == 412 && r.errCode() == "PRECONDITION_FAILED":
				refused[detail(r, "existing_case_id")]++
			default:
				t.Errorf("unexpected answer: %d %s", r.status, r.raw)
			}
		}(i)
	}
	wg.Wait()
	if won != 1 || len(refused) != 1 || refused[winner] != callers-1 {
		t.Fatalf("won %d, refused %v, winner %q", won, refused, winner)
	}
	if e.count("cases") != 1 {
		t.Fatalf("%d cases", e.count("cases"))
	}
	if next := e.runtime("POST", "/v1/cases", newKey("idk_after"), caseBodyFor("TX00002")); next.str("reference") != "CMP-2026-00002" {
		t.Fatalf("references stay gapless after the race: %s", next.raw)
	}
}

// The same create sent by many callers with one key is one case too: the winner's answer and its replays.
func TestConcurrentRepeatsOfOneCreateAreOneCase(t *testing.T) {
	e := newEnv(t)
	const callers = 32
	var wg sync.WaitGroup
	var mu sync.Mutex
	statuses := map[int]int{}
	ids := map[string]bool{}
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := e.send(e.client, "POST", e.caseURL+"/v1/cases", runtimeToken, newKey("idk_same"), caseBody())
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			statuses[r.status]++
			ids[r.str("case_id")] = true
		}()
	}
	wg.Wait()
	if statuses[201] != 1 || statuses[200] != callers-1 || len(ids) != 1 || e.count("cases") != 1 {
		t.Fatalf("statuses %v, ids %v, cases %d", statuses, ids, e.count("cases"))
	}
}

// Read-back: the state a caller verifies after a create is the target's own. The version token is issued by the
// target; a token the caller made up is refused.
func TestReadBackAfterCreateIsVerifiable(t *testing.T) {
	e := newEnv(t)
	created := e.createCase("idk_1")
	id, token := created.str("case_id"), created.str("version_token")
	if created.status != 201 || id == "" || token != "v1" {
		t.Fatalf("%d %s", created.status, created.raw)
	}

	byKey := e.runtime("GET", "/v1/cases?tenant_id=ten_a&idempotency_key=idk_1", nil, nil)
	if items := byKey.items(); len(items) != 1 || items[0]["case_id"] != id || items[0]["version_token"] != token {
		t.Fatalf("find by key: %s", byKey.raw)
	}
	byID := e.runtime("GET", "/v1/cases/"+id+"?tenant_id=ten_a", nil, nil)
	if byID.status != 200 || byID.str("version_token") != token || byID.str("transaction_ref") != "TX88213" || byID.str("status") != "OPEN" {
		t.Fatalf("get by id: %d %s", byID.status, byID.raw)
	}

	// A made-up token is refused and changes nothing; the token read back is accepted.
	if r := e.runtime("POST", "/v1/cases/"+id+"/route", nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v99"}); r.status != 409 || r.errCode() != "VERSION_MISMATCH" || detail(r, "current_version_token") != token {
		t.Fatalf("a fabricated token: %d %s", r.status, r.raw)
	}
	routed := e.runtime("POST", "/v1/cases/"+id+"/route", nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": token})
	if routed.status != 200 || routed.str("version_token") != "v2" || routed.str("owner_queue") != "DISPUTES" {
		t.Fatalf("the token read back routes: %d %s", routed.status, routed.raw)
	}

	// A refused create names the case that exists, and that case reads back as it was named.
	rival := e.createCase("idk_2")
	named := e.runtime("GET", "/v1/cases/"+detail(rival, "existing_case_id")+"?tenant_id=ten_a", nil, nil)
	if rival.status != 412 || named.status != 200 || named.str("case_id") != id || named.str("version_token") != "v2" {
		t.Fatalf("refusal %d %s, read back %d %s", rival.status, rival.raw, named.status, named.raw)
	}
	if e.count("cases") != 1 {
		t.Fatalf("the truth endpoint holds %d cases", e.count("cases"))
	}
}
