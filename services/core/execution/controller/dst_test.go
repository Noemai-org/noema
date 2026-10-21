package controller_test

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

// minSeeds is the figure of DEC-2026-10-20 D7 (PERD 27.3): at least 10,000 seeded interleavings in CI. It is
// a floor, never lowered. NOEMA_DST_EXTRA adds more seeds on top; NOEMA_DST_SEED replays one.
const minSeeds = 10000

// D7, deterministic simulation: a seeded scheduler interleaves concurrent executors of one grant, a recovery
// sweeper and a state mutator against a fault-injecting network and clock. Any failure prints its seed; the
// seed reproduces it exactly (NOEMA_DST_SEED=<seed> go test -run TestDeterministicSimulation).
func TestDeterministicSimulation(t *testing.T) {
	if s := os.Getenv("NOEMA_DST_SEED"); s != "" {
		seed, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		r := RunSeed(seed)
		for _, v := range r.Violations {
			t.Error(v)
		}
		return
	}
	n := uint64(minSeeds)
	if e := os.Getenv("NOEMA_DST_EXTRA"); e != "" {
		extra, err := strconv.ParseUint(e, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		n += extra
	}
	failed := 0
	for seed := uint64(1); seed <= n; seed++ {
		r := RunSeed(seed)
		for _, v := range r.Violations {
			t.Error(v)
			failed++
		}
		if failed > 20 {
			t.Fatal("stopping after 20 violations; replay a seed with NOEMA_DST_SEED")
		}
	}
	t.Logf("%d seeded interleavings, 0 violations", n)
}

// Every failure of the simulation becomes a regression test: its seed is added to testdata/regression_seeds.json
// and replayed here for ever.
func TestRegressionSeeds(t *testing.T) {
	raw, err := os.ReadFile("testdata/regression_seeds.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Seeds []struct {
			Seed uint64 `json:"seed"`
			Why  string `json:"why"`
		} `json:"seeds"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	for _, s := range f.Seeds {
		for _, v := range RunSeed(s.Seed).Violations {
			t.Errorf("%s (%s)", v, s.Why)
		}
	}
}

// The same seed gives the same run: the Record, the dispatches and the effects are identical.
func TestASeedIsDeterministic(t *testing.T) {
	for _, seed := range []uint64{1, 7, 42, 1234, 9999} {
		a, b := RunSeed(seed).World.Snapshot(), RunSeed(seed).World.Snapshot()
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			t.Fatalf("seed %d is not reproducible", seed)
		}
	}
}
