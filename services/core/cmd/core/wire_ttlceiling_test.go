package main

// The Recorder is given the pack's confirmation TTL ceiling (A561, A724, A758; REQ-TRU-007, AGENT-16): the value is the pack's `confirmation_ttl_ceiling_ms` read from the verified trust
// limits through trustlimits, never a number of this package. Absent limits leave it zero (the Writer then writes no CONFIRM request: fail closed); present limits that do not load are an
// error (A612). Every value is SYNTHETIC.

import (
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

func TestTheCeilingIsThePacksVerifiedTrustLimitsValue(t *testing.T) {
	v := tiVerified(t)
	limits, _, err := trustLimitsOf(v)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := limits.ConfirmationTTLCeilingMS()
	if err != nil || ms <= 0 {
		t.Fatalf("the pack's own ceiling: %d, %v", ms, err)
	}
	pack, err := trustpack.LoadVerifiedWithLimits(v.Pack, v.ActionGraphs, v.Limits)
	if err != nil {
		t.Fatal(err)
	}
	got, err := confirmationTTLCeiling(pack)
	if err != nil || got != time.Duration(ms)*time.Millisecond {
		t.Errorf("ceiling %v, %v; want %d ms from the pack's limits", got, err, ms)
	}
}

func TestAPackWithNoLimitsGivesNoCeilingAndNoError(t *testing.T) {
	v := tiVerified(t)
	pack, err := trustpack.LoadVerified(v.Pack, v.ActionGraphs)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := confirmationTTLCeiling(pack); err != nil || got != 0 {
		t.Errorf("ceiling %v, %v; want zero and no error (the Writer then writes no CONFIRM request)", got, err)
	}
	if got, err := confirmationTTLCeiling(nil); err != nil || got != 0 {
		t.Errorf("a nil pack: %v, %v; want zero and no error", got, err)
	}
}

func TestLimitsThatDoNotLoadAreAnErrorAndGiveNoCeiling(t *testing.T) {
	v := tiVerified(t)
	bad := map[string][]byte{}
	for path := range v.Limits {
		bad[path] = []byte(`{"synthetic":true}`)
	}
	pack, err := trustpack.LoadVerifiedWithLimits(v.Pack, v.ActionGraphs, bad)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := confirmationTTLCeiling(pack); err == nil || got != 0 {
		t.Errorf("ceiling %v, %v; want an error and no value", got, err)
	}
}

func TestTheWiringGivesTheDecisionsThePacksCeilingAndTheRecorderCarriesIt(t *testing.T) {
	v := tiVerified(t)
	in := twIn(t)
	pack, err := trustpack.LoadVerifiedWithLimits(v.Pack, v.ActionGraphs, v.Limits)
	if err != nil {
		t.Fatal(err)
	}
	in.Pack = pack
	di, err := decisionInputsFrom(in)
	if err != nil || di.ConfirmationTTLCeiling <= 0 {
		t.Fatalf("decision inputs: ceiling %v, %v; want the pack's ceiling", di.ConfirmationTTLCeiling, err)
	}
	full := decInputs(t, nil)
	full.ConfirmationTTLCeiling = di.ConfirmationTTLCeiling
	cfg, missing, err := decisionRecorderConfig(full)
	if err != nil || len(missing) > 0 {
		t.Fatalf("config: %v, %v", err, missing)
	}
	if cfg.ConfirmationTTLCeiling != di.ConfirmationTTLCeiling {
		t.Errorf("the Recorder's ceiling %v; want %v", cfg.ConfirmationTTLCeiling, di.ConfirmationTTLCeiling)
	}
}
