package proxy_test

import (
	"encoding/json"
	"regexp"
	"testing"
)

// A22 (DEC-2026-11-06; noema#170): GET /control/v1/requests carries a generation that is new at every start and every reset of the
// log. A reader that took a baseline then sees a restart, or a reset followed by a byte-identical refill, as a changed generation and
// not as "no new requests" (BEN-05, EC-T09). The generation is not part of the log hash (REQ-ENV-004: the same plan, seed and calls
// give the same hash). SYNTHETIC (AGENT-25).

var generationShape = regexp.MustCompile(`^[0-9a-f]{32}$`)

func generationOf(t *testing.T, r *rig) (string, string) {
	t.Helper()
	res := r.ctl("GET", "/control/v1/requests", "")
	var body struct {
		Generation *string `json:"generation"`
		LogHash    string  `json:"log_hash"`
	}
	if err := json.Unmarshal([]byte(res.body), &body); err != nil {
		t.Fatalf("%v: %s", err, res.body)
	}
	if body.Generation == nil {
		t.Fatalf("the request log carries no generation: %s", res.body)
	}
	return *body.Generation, body.LogHash
}

func TestTheRequestLogCarriesAGenerationThatStaysTheSameWhileRequestsArrive(t *testing.T) {
	r := newRig(t)
	g1, _ := generationOf(t, r)
	if !generationShape.MatchString(g1) {
		t.Fatalf("generation %q is not 32 lower-case hex digits", g1)
	}
	r.get("/v1/status/TX1")
	r.get("/v1/status/TX2")
	if g2, _ := generationOf(t, r); g2 != g1 {
		t.Fatalf("the generation changed from %s to %s without a restart or reset", g1, g2)
	}
}

func TestAnotherStartHasAnotherGeneration(t *testing.T) {
	a, b := newRig(t), newRig(t)
	ga, _ := generationOf(t, a)
	gb, _ := generationOf(t, b)
	if ga == gb {
		t.Fatalf("two proxies started apart share the generation %s: a restart would be invisible", ga)
	}
}

func TestAResetOfTheLogChangesTheGenerationEvenIfTheSameCallsRefillIt(t *testing.T) {
	r := newRig(t)
	r.get("/v1/status/TX1")
	g1, h1 := generationOf(t, r)
	if res := r.ctl("DELETE", "/control/v1/fault-plan", ""); res.status != 200 && res.status != 204 {
		t.Fatalf("delete plan: %d %s", res.status, res.body)
	}
	r.get("/v1/status/TX1") // the byte-identical refill
	g2, h2 := generationOf(t, r)
	if g1 == g2 {
		t.Fatal("the log was cleared and refilled identically, and the generation did not change: the reset is invisible")
	}
	if h1 != h2 {
		t.Fatalf("the log hash differs (%s, %s) for identical calls: the generation must not enter it", h1, h2)
	}
}

func TestTheGenerationIsNotPartOfTheLogHash(t *testing.T) {
	a, b := newRig(t), newRig(t)
	a.get("/v1/status/TX1")
	b.get("/v1/status/TX1")
	_, ha := generationOf(t, a)
	_, hb := generationOf(t, b)
	if ha != hb {
		t.Fatalf("the same calls on two starts give different log hashes: %s %s", ha, hb)
	}
}
