package main

// STM-01 decisions lane: what the Recorder is configured with (A475, A503; REQ-TRU-008, REQ-TRU-010; INV-03, INV-29). These tests were written after the hand mutation check of the
// green commit, for the mutants that built a Recorder without its escalation set, with other lifetimes or with a seam dropped. Every value is SYNTHETIC.

import (
	"reflect"
	"strings"
	"testing"
	"time"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

func TestTheRecordersConfigurationIsTheSettingsTheKeysTheSeamsAndThePacksEscalationSet(t *testing.T) {
	in := decInputs(t, nil)
	cfg, missing, err := decisionRecorderConfig(in)
	if err != nil || len(missing) > 0 {
		t.Fatalf("config: %v, %v", err, missing)
	}
	want, err := trustpack.EscalationSetFromPack(in.Pack, syntheticEscalationGraph)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Escalation, want) || reflect.DeepEqual(cfg.Escalation, trustport.RecorderConfig{}.Escalation) {
		t.Errorf("escalation %+v; want the pack's escalation graph %+v", cfg.Escalation, want)
	}
	if cfg.GrantLifetime != 7*time.Second || cfg.RecordLifetime != 8*time.Second || cfg.TxTimeout != 900*time.Millisecond {
		t.Errorf("lifetimes %v %v %v; want the settings' own (7000, 8000, 900 ms)", cfg.GrantLifetime, cfg.RecordLifetime, cfg.TxTimeout)
	}
	if cfg.Keys != in.Keys || cfg.Appender == nil || cfg.Begin != in.Begin || cfg.Facts != in.Facts || cfg.Clock != in.Clock {
		t.Errorf("a seam was dropped or replaced: %+v", cfg)
	}
	if cfg.IDs == nil || cfg.Entropy == nil {
		t.Errorf("no ID source or entropy: %+v", cfg)
	}
}

func TestARecorderWithNoClockIsAnErrorAndNeverAnAdapterWithoutOne(t *testing.T) {
	in := decInputs(t, nil)
	in.Clock = nil
	p, err := buildDecisions(in)
	if err == nil || p.Recorder != nil || !strings.Contains(err.Error(), "clock") {
		t.Errorf("no clock: %v, %T; want an error naming the clock and no Recorder", err, p.Recorder)
	}
}

// A469: an invalid value is fatal even when another of the three is absent, so the loader's all-three check is not the only judge.
func TestAnInvalidLifetimeIsFatalWhileAnotherOfTheThreeIsAbsent(t *testing.T) {
	in := decInputs(t, map[string]string{EnvTrustGrantLifetimeMS: "abc", EnvTrustRecordLifetimeMS: "<unset>"})
	if _, err := buildDecisions(in); err == nil || !strings.Contains(err.Error(), EnvTrustGrantLifetimeMS) {
		t.Errorf("error %v; want one that names %s", err, EnvTrustGrantLifetimeMS)
	}
}

// fill never replaces a field with a gap: an adapter another source already set stays.
func TestFillOfAGappedLaneKeepsAFieldThatWasAlreadySet(t *testing.T) {
	p, err := buildDecisions(decisionInputs{Env: Env{Getenv: func(string) string { return "" }}})
	if err != nil {
		t.Fatal(err)
	}
	full, err := buildDecisions(decInputs(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	a := p.fill(TrustAdapters{Intents: full.Intents, Recorder: full.Recorder})
	if a.Intents != full.Intents || a.Recorder != full.Recorder {
		t.Errorf("a gapped lane replaced a field that was set: %+v", a)
	}
}
