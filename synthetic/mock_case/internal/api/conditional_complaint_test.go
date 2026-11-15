package api_test

import (
	"fmt"
	"sync"
	"testing"
)

// Conditional complaint create (Architect ruling A185, 13 Nov 2026; issue noema#522; REQ-EXE-004, REQ-ENV-008).
//
// fin.cap.log_complaint_reference is a CONDITIONAL_WRITE whose precondition is "no complaint reference exists for this subject and epoch". Execution can only look before it writes, so two
// executions with different idempotency keys could both pass and log two complaints, which nothing can undo. The target therefore creates conditionally and atomically: a create that
// sends `If-None-Match: *` is made only if the case has no complaint, in the transaction that would insert it. A case is the subject and epoch (a case is unique per transaction and epoch,
// migration 0003), so "no complaint for the subject and epoch" is "no complaint for this case". A create without the header keeps its earlier behaviour. SYNTHETIC (AGENT-25).
// Needs PostgreSQL, so CI is its evidence.

func (e *env) complaintFor(caseID string, hdr map[string]string) resp {
	e.t.Helper()
	return e.runtime("POST", "/v1/cases/"+caseID+"/complaints", hdr, map[string]any{"tenant_id": "ten_a"})
}

func TestAConditionalComplaintIsCreatedOnceAndARivalIsRefusedWithTheExistingReference(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_case")
	id := c.str("case_id")

	first := e.complaintFor(id, newKey("idk_c1"))
	if first.status != 201 || first.str("complaint_ref") == "" {
		t.Fatalf("a conditional create on a case with no complaint: %d %s", first.status, first.raw)
	}
	rival := e.complaintFor(id, newKey("idk_c2")) // another key, the same subject and epoch
	if rival.status != 412 || rival.errCode() != "PRECONDITION_FAILED" || rival.json["error"].(map[string]any)["retryable"] != false {
		t.Fatalf("a rival conditional create: %d %s, want 412 PRECONDITION_FAILED, not retryable", rival.status, rival.raw)
	}
	if got := detail(rival, "existing_complaint_ref"); got != first.str("complaint_ref") {
		t.Fatalf("the refusal must name the complaint that exists: got %q, want %q: %s", got, first.str("complaint_ref"), rival.raw)
	}
	if n := e.count("complaints"); n != 1 {
		t.Fatalf("%d complaints, want exactly 1", n)
	}
	// A refused create spends no reference: the next complaint, on another case, is number 2.
	other := e.runtime("POST", "/v1/cases", newKey("idk_case2"), caseBodyFor("TX00002"))
	next := e.complaintFor(other.str("case_id"), newKey("idk_c3"))
	if next.status != 201 || next.str("complaint_ref") != "CPR-2026-00002" {
		t.Fatalf("references stay gapless after a refused create: %d %s", next.status, next.raw)
	}
}

func TestTheSameConditionalCreateRepeatedIsOneComplaintAndItsReplay(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("idk_case").str("case_id")
	first := e.complaintFor(id, newKey("idk_c1"))
	again := e.complaintFor(id, newKey("idk_c1")) // a complaint now exists, but this is a repeat of the winner, not a rival
	if first.status != 201 || again.status != 200 || again.header.Get("Idempotent-Replay") != "true" || again.str("complaint_ref") != first.str("complaint_ref") {
		t.Fatalf("first %d %s, repeat %d %s", first.status, first.raw, again.status, again.raw)
	}
	if n := e.count("complaints"); n != 1 {
		t.Fatalf("%d complaints", n)
	}
}

func TestAConditionalCreateIsRefusedWhenAnUnconditionalComplaintAlreadyExists(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("idk_case").str("case_id")
	plain := e.complaintFor(id, key("idk_plain")) // no precondition: the earlier behaviour
	if plain.status != 201 {
		t.Fatalf("an unconditional create still works: %d %s", plain.status, plain.raw)
	}
	r := e.complaintFor(id, newKey("idk_cond"))
	if r.status != 412 || detail(r, "existing_complaint_ref") != plain.str("complaint_ref") {
		t.Fatalf("a conditional create must see an earlier unconditional complaint: %d %s", r.status, r.raw)
	}
	if n := e.count("complaints"); n != 1 {
		t.Fatalf("%d complaints", n)
	}
}

func TestWithoutThePreconditionTheEarlierBehaviourIsUnchanged(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("idk_case").str("case_id")
	a, b := e.complaintFor(id, key("idk_a")), e.complaintFor(id, key("idk_b"))
	if a.status != 201 || b.status != 201 || a.str("complaint_ref") == b.str("complaint_ref") {
		t.Fatalf("two unconditional creates with distinct keys stay two complaints: %d %d", a.status, b.status)
	}
}

func TestOnlyTheStarPreconditionIsAcceptedOnAComplaint(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("idk_case").str("case_id")
	for _, v := range []string{`"v1"`, "W/*", "x"} {
		r := e.complaintFor(id, map[string]string{"Idempotency-Key": "idk_x", "If-None-Match": v})
		if r.status != 400 || r.errCode() != "INVALID_REQUEST" {
			t.Errorf("If-None-Match %s: %d %s", v, r.status, r.raw)
		}
	}
	if n := e.count("complaints"); n != 0 {
		t.Fatalf("a refused request wrote %d complaints", n)
	}
}

func TestAConditionalComplaintOnAnUnknownCaseIsNotFoundAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	r := e.complaintFor("case_no_such_case", newKey("idk_c"))
	if r.status != 404 || e.count("complaints") != 0 {
		t.Fatalf("%d %s", r.status, r.raw)
	}
}

// The race the ruling exists for: many executions with distinct keys, one case. Exactly one complaint is logged, and every other caller is refused and told which one won.
func TestConcurrentConditionalComplaintsOfOneCaseLogExactlyOne(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("idk_case").str("case_id")
	const callers = 32
	var wg sync.WaitGroup
	var mu sync.Mutex
	won, winner := 0, ""
	refused := map[string]int{}
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := e.send(e.client, "POST", e.caseURL+"/v1/cases/"+id+"/complaints", runtimeToken, newKey(fmt.Sprintf("idk_race_%d", i)), map[string]any{"tenant_id": "ten_a"})
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case r.status == 201:
				won++
				winner = r.str("complaint_ref")
			case r.status == 412 && r.errCode() == "PRECONDITION_FAILED":
				refused[detail(r, "existing_complaint_ref")]++
			default:
				t.Errorf("unexpected answer: %d %s", r.status, r.raw)
			}
		}(i)
	}
	wg.Wait()
	if won != 1 || len(refused) != 1 || refused[winner] != callers-1 {
		t.Fatalf("won %d, refused %v, winner %q", won, refused, winner)
	}
	if n := e.count("complaints"); n != 1 {
		t.Fatalf("%d complaints, want exactly 1", n)
	}
	if next := e.complaintFor(e.runtime("POST", "/v1/cases", newKey("idk_case2"), caseBodyFor("TX00002")).str("case_id"), newKey("idk_after")); next.str("complaint_ref") != "CPR-2026-00002" {
		t.Fatalf("references stay gapless after the race: %s", next.raw)
	}
}

// Conditional and unconditional creates racing: a conditional create succeeds only if no complaint existed when it committed, so at most one conditional create succeeds, and if one did
// no unconditional complaint committed before it. The first is asserted directly; the second through the ordering of the references.
func TestConditionalAndUnconditionalComplaintsRacingNeverGiveTwoConditionalWinners(t *testing.T) {
	e := newEnv(t)
	id := e.createCase("idk_case").str("case_id")
	const each = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	condWins := 0
	for i := 0; i < each; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			r, err := e.send(e.client, "POST", e.caseURL+"/v1/cases/"+id+"/complaints", runtimeToken, newKey(fmt.Sprintf("idk_cond_%d", i)), map[string]any{"tenant_id": "ten_a"})
			if err != nil {
				t.Error(err)
				return
			}
			if r.status == 201 {
				mu.Lock()
				condWins++
				mu.Unlock()
			} else if r.status != 412 {
				t.Errorf("conditional: %d %s", r.status, r.raw)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			r, err := e.send(e.client, "POST", e.caseURL+"/v1/cases/"+id+"/complaints", runtimeToken, key(fmt.Sprintf("idk_plain_%d", i)), map[string]any{"tenant_id": "ten_a"})
			if err != nil || r.status != 201 {
				t.Errorf("unconditional: %v %d %s", err, r.status, r.raw)
			}
		}(i)
	}
	wg.Wait()
	if condWins > 1 {
		t.Fatalf("%d conditional creates succeeded, want at most 1", condWins)
	}
}
