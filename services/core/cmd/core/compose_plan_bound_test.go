package main

// A705 (interim, committed on docs main; REQ-STM-001, REQ-TRU-009; AGENT-16, A330, A469): NOEMA_PLAN_TEXT_MAX_BYTES is the bound of every string a slot gives the Planner. It is required and
// has no code default: absent, the Planner stays a named gap that names the setting and no value; given, the Planner is built; invalid, the process stops with an error that names the
// setting and never the value. Every number here is SYNTHETIC.

import (
	"strings"
	"testing"
)

func TestThePlannerIsBuiltWhenThePlanTextBoundIsGiven(t *testing.T) {
	in := twIn(t)
	twEnv(&in, map[string]string{EnvPlanTextMaxBytes: "256"})
	w, err := wireTrustInputs(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.fill(TrustAdapters{}); got.Planner == nil {
		t.Error("the Planner was not built although the text bound was given")
	}
	for _, g := range w.Unbuilt() {
		if g == "Planner" {
			t.Errorf("Planner is still named unbuilt: %v", w.Unbuilt())
		}
	}
}

func TestWithoutThePlanTextBoundThePlannerIsAGapThatNamesTheSetting(t *testing.T) {
	w, err := wireTrustInputs(twIn(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := w.fill(TrustAdapters{}); got.Planner != nil {
		t.Errorf("a Planner was built with no bound: %T", got.Planner)
	}
	named := false
	for _, r := range w.Reasons["Planner"] {
		if strings.Contains(r, EnvPlanTextMaxBytes) {
			named = true
		}
	}
	if !named {
		t.Errorf("the Planner's reasons %v do not name %s", w.Reasons["Planner"], EnvPlanTextMaxBytes)
	}
}

func TestAnInvalidPlanTextBoundStopsTheProcessAndNamesNoValue(t *testing.T) {
	for _, bad := range []string{"abc", "0", "-5", "1.5", "1e3"} {
		in := twIn(t)
		twEnv(&in, map[string]string{EnvPlanTextMaxBytes: bad})
		_, err := wireTrustInputs(in)
		if err == nil || !strings.Contains(err.Error(), EnvPlanTextMaxBytes) || strings.Contains(err.Error(), bad) {
			t.Errorf("%s=%q: err %v; want a refusal that names the setting and not the value", EnvPlanTextMaxBytes, bad, err)
		}
	}
}

// SR (A754 round): the bound of a plan text has an upper limit, 4096 bytes: the largest value is accepted and anything above it stops the process, naming the setting and never the value.
func TestThePlanTextBoundHasAnUpperLimitOf4096(t *testing.T) {
	in := twIn(t)
	twEnv(&in, map[string]string{EnvPlanTextMaxBytes: "4096"})
	w, err := wireTrustInputs(in)
	if err != nil {
		t.Fatalf("4096 was refused: %v", err)
	}
	if got := w.fill(TrustAdapters{}); got.Planner == nil {
		t.Error("the Planner was not built at the upper limit")
	}
	for _, bad := range []string{"4097", "65536", "1000000"} {
		in := twIn(t)
		twEnv(&in, map[string]string{EnvPlanTextMaxBytes: bad})
		_, err := wireTrustInputs(in)
		if err == nil || !strings.Contains(err.Error(), EnvPlanTextMaxBytes) || strings.Contains(err.Error(), bad) {
			t.Errorf("%s=%q: err %v; want a refusal that names the setting and not the value", EnvPlanTextMaxBytes, bad, err)
		}
	}
}
