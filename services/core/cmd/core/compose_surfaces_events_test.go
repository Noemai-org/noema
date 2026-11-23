package main

// STM-01 surfaces, the events route (REQ-API-001; INV-12, AGENT-16; A330, A469, A486, interim, relayed, not yet committed): a route whose settings are not all given is a named gap (the
// process starts, /readyz names it in fixed text, never a value); a setting that is given and invalid is fatal and names itself, never its value. Every value is SYNTHETIC.

import (
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

var sfEventsOrder = []string{EnvEventsPollMS, EnvEventsMaxStreamMS, EnvEventsWriteTimeoutMS, EnvEventsReadTimeoutMS, EnvEventsMaxEventBytes, EnvEventsMaxStreams, EnvEventsMaxStreamsPerPrincipal, EnvEventsMaxBatch}

func sfEventsEnv() envMap {
	return envMap{EnvEventsPollMS: "1000", EnvEventsMaxStreamMS: "60000", EnvEventsWriteTimeoutMS: "5000", EnvEventsReadTimeoutMS: "2000", EnvEventsMaxEventBytes: "65536",
		EnvEventsMaxStreams: "100", EnvEventsMaxStreamsPerPrincipal: "2", EnvEventsMaxBatch: "50"}
}

func sfClock() clock.Clock { return clock.NewManual(time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)) }

// With no Record reader there is nothing to read from: no route, no gap of its own (the reader is named by "record reader"), no error.
func TestNoRecordReaderMeansNoEventsRouteAndNoExtraGap(t *testing.T) {
	h, gaps, err := buildEventsSurface(osEnv(sfEventsEnv()), sfClock(), nil)
	if h != nil || len(gaps) != 0 || err != nil {
		t.Fatalf("handler %v, gaps %v, error %v; want none", h, gaps, err)
	}
}

// None of the settings: the route is a gap named by its plain name (Components.Events nil), no extra line, no error.
func TestNoEventsSettingIsAPlainGapNotAnError(t *testing.T) {
	h, gaps, err := buildEventsSurface(osEnv(envMap{}), sfClock(), &failingDBs{})
	if h != nil || len(gaps) != 0 || err != nil {
		t.Fatalf("handler %v, gaps %v, error %v; want none", h, gaps, err)
	}
}

// All eight settings and the Record reader build the route.
func TestAllTheEventsSettingsBuildTheRoute(t *testing.T) {
	h, gaps, err := buildEventsSurface(osEnv(sfEventsEnv()), sfClock(), &failingDBs{})
	if h == nil || len(gaps) != 0 || err != nil {
		t.Fatalf("handler %v, gaps %v, error %v; want a handler", h, gaps, err)
	}
}

// Each setting that is absent while the others are given is named, in fixed text, and is no error (A486).
func TestAnAbsentEventsSettingIsANamedGap(t *testing.T) {
	for _, drop := range sfEventsOrder {
		e := sfEventsEnv()
		delete(e, drop)
		h, gaps, err := buildEventsSurface(osEnv(e), sfClock(), &failingDBs{})
		if want := "events route: " + drop + " missing"; h != nil || err != nil || len(gaps) != 1 || gaps[0] != want {
			t.Errorf("%s absent: handler %v, gaps %v, error %v; want no handler, [%q] and no error", drop, h, gaps, err, want)
		}
	}
}

// Every absent setting is named, in the one order, not only the first.
func TestEveryAbsentEventsSettingIsNamedInOrder(t *testing.T) {
	h, gaps, err := buildEventsSurface(osEnv(envMap{EnvEventsPollMS: "1000"}), sfClock(), &failingDBs{})
	var want []string
	for _, n := range sfEventsOrder[1:] {
		want = append(want, "events route: "+n+" missing")
	}
	if h != nil || err != nil || strings.Join(gaps, "|") != strings.Join(want, "|") {
		t.Fatalf("handler %v, gaps %v, error %v; want %v", h, gaps, err, want)
	}
}

// A setting that is given and invalid is fatal, whether or not others are absent, and the error names the setting and never the value (A469).
func TestAnInvalidEventsSettingIsFatalAndNamesItNotItsValue(t *testing.T) {
	for _, name := range sfEventsOrder {
		for _, bad := range []string{"0", "-1", "ninety", "1.5", "99999999999"} {
			for _, others := range []bool{true, false} {
				e := envMap{name: bad}
				if others {
					e = sfEventsEnv()
					e[name] = bad
				}
				h, gaps, err := buildEventsSurface(osEnv(e), sfClock(), &failingDBs{})
				if h != nil || len(gaps) != 0 || err == nil || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), bad) {
					t.Errorf("%s=%q (others given %v): handler %v, gaps %v, error %v; want a fatal error naming the setting only", name, bad, others, h, gaps, err)
				}
			}
		}
	}
}

// The per-principal cap above the global one is a configuration error, not a gap: the handler refuses it (and the error names no value).
func TestAPerPrincipalCapAboveTheGlobalCapIsFatal(t *testing.T) {
	e := sfEventsEnv()
	e[EnvEventsMaxStreamsPerPrincipal] = "101"
	if h, _, err := buildEventsSurface(osEnv(e), sfClock(), &failingDBs{}); h != nil || err == nil || strings.Contains(err.Error(), "101") {
		t.Fatalf("handler %v, error %v; want a fatal error without the value", h, err)
	}
}
