package decide_test

// TEN-02 (internal, task ENV-14; PERD §34.5 [REC]): "A replayed scenario gives identical Trust decisions and verdicts."
//
// WHAT THIS SHOWS: the Trust half. The real Admit, FailAtGate and Allow of this package, run on the canned Intent Objects of
// synthetic/proposer_swap, are replayed many times, in a seeded order, concurrently, each time from freshly read files. Every
// replay must produce the same decision bytes as the first pass and as the committed decisions.json, which is an earlier
// recorded run (INV-21). The same stand-in intake as the proposer swap test is used (see proposer_swap_test.go): the Trust
// intake is not built, and the three authority layers are fixed to PASS because the authority and policy engines are not built.
//
// WHAT THIS DOES NOT SHOW: the verdict half. The Outcome verdict code (outcome/verifier, outcome/predicate) is not built on
// main, so there is nothing to replay; the TEN_RESULT line below names the part it covers and nothing else. The verdict half
// waits for OUT-01 and is reported as absent, not as a pass.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

const (
	ten02Seed     = 20261103 // SYNTHETIC. The seed of the replay order; nothing else in this test is random.
	ten02Replays  = 64
	ten02Scenario = "escalate"
)

type ten02Key struct{ scenario, proposer string }

func ten02Keys() []ten02Key {
	var keys []ten02Key
	for _, s := range swapScenarios {
		for _, p := range []string{"A", "B"} {
			keys = append(keys, ten02Key{s, p})
		}
	}
	return keys
}

func ten02Decide(t *testing.T, pack swapPack, k ten02Key) []byte {
	t.Helper()
	_, obj := loadProposal(t, k.scenario, k.proposer)
	out, err := json.Marshal(intake(t, obj, pack, nil))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTEN02ReplayedScenariosGiveIdenticalTrustDecisions(t *testing.T) {
	defer func() {
		t.Logf("TEN_RESULT id=TEN-02 part=trust-decisions seed=%d replays=%d pass=%t (verdict half: not built, OUT-01)", ten02Seed, ten02Replays, !t.Failed())
	}()
	pack := loadSwapPack(t)
	keys := ten02Keys()

	// The recorded run: the decisions the real constructors gave when synthetic/proposer_swap/decisions.json was written.
	raw, err := os.ReadFile(filepath.Join(swapDir(t), "decisions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recorded swapDecisions
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatal(err)
	}

	baseline := map[ten02Key][]byte{}
	for _, k := range keys {
		baseline[k] = ten02Decide(t, pack, k)
		want, err := json.Marshal(recorded.Decisions[k.scenario])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(baseline[k], want) {
			t.Fatalf("%s/%s: today's decision differs from the recorded one:\n now: %s\n rec: %s", k.scenario, k.proposer, baseline[k], want)
		}
	}

	rng := rand.New(rand.NewSource(ten02Seed))
	t.Run("replays", func(t *testing.T) {
		for i := 0; i < ten02Replays; i++ {
			order := rng.Perm(len(keys))
			t.Run(fmt.Sprintf("replay-%02d", i), func(t *testing.T) {
				t.Parallel()
				for _, idx := range order {
					k := keys[idx]
					if got := ten02Decide(t, pack, k); !bytes.Equal(got, baseline[k]) {
						t.Errorf("%s/%s: a replay gave a different decision:\n got:  %s\n base: %s", k.scenario, k.proposer, got, baseline[k])
					}
				}
			})
		}
	})

	// The comparison is not vacuous: change one input of the real decision path and the bytes must change.
	_, obj := loadProposal(t, ten02Scenario, "A")
	obj.TenantID = "ten_zz" // not the session's tenant (INV-12): T1 must refuse
	changed, err := json.Marshal(intake(t, obj, pack, nil))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(changed, baseline[ten02Key{ten02Scenario, "A"}]) {
		t.Fatal("a different tenant gave the same decision: the replay comparison could not have caught a change")
	}
}
