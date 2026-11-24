package main

// STM-01 decisions lane, Confirmations and Planner (A537 interim, relayed, recorded in docs#229; A469, A486, A511, A531; REQ-TRU-009, REQ-TRU-002, REQ-STM-001; INV-03, INV-16,
// AGENT-16). Neither is built: each is a gap whose reasons are fixed text that a test pins, so that a part that arrives has to change the test (and the lane) with it. The
// Confirmations path lacks a read of a request by its id, the Record events of the confirmation store and the pack's confirmation terms. The Planner lacks the pack's action-graph
// reader, the stored intent's slot reader, the plan contexts and the pack's capability registry (noema#896 merged: it does not change these). intake.Service.Runnable and
// .Handoffs have no adapter, so the first evaluation plans the action graph's first steps as before (the intake's documented behaviour with no Runnable port). Every value is SYNTHETIC.

import (
	"reflect"
	"strings"
	"testing"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

func TestConfirmationsAndPlannerAreNamedGapsWithTheirFixedReasons(t *testing.T) {
	p, err := buildDecisions(decInputs(t, nil))
	if err != nil {
		t.Fatalf("buildDecisions: %v", err)
	}
	want := map[string][]string{
		"Confirmations":    {needRequestRead, needRequestEvents, needConfirmPolicy},
		"Planner":          {needGraphReader, needIntentSlots, needPlanContexts, needRegistry},
		gapServiceRunnable: {needRunnableAdapt},
		gapServiceHandoffs: {needHandoffAdapter},
	}
	for adapter, missing := range want {
		g, ok := decGap(p, adapter)
		if !ok || strings.Join(g.Missing, "|") != strings.Join(missing, "|") {
			t.Errorf("%s: gap %+v (named: %v); want lacking %q", adapter, g, ok, missing)
		}
	}
	if p.Intents == nil || p.Recorder == nil {
		t.Errorf("with every input given, Intents and Recorder are built: %T, %T", p.Intents, p.Recorder)
	}
}

func TestEveryGapNamesATrustAdaptersFieldOrAServiceFieldAndCarriesNoValue(t *testing.T) {
	p, err := buildDecisions(decisionInputs{Env: Env{Getenv: func(string) string { return "" }}})
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]bool{gapServiceRunnable: true, gapServiceHandoffs: true}
	at := reflect.TypeOf(TrustAdapters{})
	for i := 0; i < at.NumField(); i++ {
		fields[at.Field(i).Name] = true
	}
	seen := map[string]bool{}
	for _, g := range p.Gaps {
		if !fields[g.Adapter] || seen[g.Adapter] || len(g.Missing) == 0 {
			t.Errorf("gap %+v: not a TrustAdapters or Service field, repeated, or empty", g)
		}
		seen[g.Adapter] = true
		for _, m := range g.Missing {
			if strings.ContainsAny(m, "=\n") || strings.Contains(m, "/run/") || strings.Contains(m, "postgres") {
				t.Errorf("gap text %q looks like it carries a value", m)
			}
		}
	}
	for _, a := range []string{"Intents", "Confirmations", "Planner", "Recorder", gapServiceRunnable, gapServiceHandoffs} {
		if !seen[a] {
			t.Errorf("an empty input set does not name %s", a)
		}
	}
	order := ""
	for _, g := range p.Gaps {
		order += g.Adapter + ","
	}
	if want := "Intents,Confirmations,Planner,Recorder,Service.Runnable,Service.Handoffs,"; order != want {
		t.Errorf("gap order %q; want the TrustAdapters field order then the Service fields: %q", order, want)
	}
}

// fill is the one seam the final composition uses: it sets what the lane built and nothing else, and it never overwrites what another lane set.
func TestFillSetsOnlyWhatTheLaneBuiltAndKeepsTheOtherLanesFields(t *testing.T) {
	p, err := buildDecisions(decInputs(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	var theirs intake.Capabilities = capsMarker{}
	a := p.fill(TrustAdapters{Capabilities: theirs, PackVersion: "x"})
	if a.Capabilities != theirs || a.PackVersion != "x" {
		t.Errorf("fill changed a field of another lane: %+v", a)
	}
	if _, ok := a.Intents.(*trustport.StoredIntents); !ok {
		t.Errorf("Intents is %T after fill", a.Intents)
	}
	if _, ok := a.Recorder.(*trustport.RecorderAdapter); !ok {
		t.Errorf("Recorder is %T after fill", a.Recorder)
	}
	unset := strings.Join(unsetAdapters(a), ",")
	for _, n := range []string{"Confirmations", "Planner"} {
		if !strings.Contains(","+unset+",", ","+n+",") {
			t.Errorf("%s is not named unset after fill: %s", n, unset)
		}
	}
	for _, n := range []string{"Intents", "Recorder", "Capabilities"} {
		if strings.Contains(","+unset+",", ","+n+",") {
			t.Errorf("%s is named unset although it is set: %s", n, unset)
		}
	}
	// A gapped lane leaves the fields as they were: nothing is invented to fill them.
	empty, _ := buildDecisions(decisionInputs{Env: Env{Getenv: func(string) string { return "" }}})
	if b := empty.fill(TrustAdapters{}); b.Intents != nil || b.Recorder != nil || b.Confirmations != nil || b.Planner != nil {
		t.Errorf("fill of a gapped lane set a field: %+v", b)
	}
}

type capsMarker struct{ intake.Capabilities }
