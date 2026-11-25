package main

// STM-01 surfaces, the approved ranges of the eight events settings (REQ-API-001; INV-12, AGENT-16; A330, A469, A486, A658, interim, relayed, not yet committed): each setting has its own
// range; a value out of its range, or two values that break a cross-check, is fatal and names the setting (never the value); an absent one stays a named gap; nothing has a default. The
// values are SYNTHETIC resource limits.

import (
	"strconv"
	"strings"
	"testing"
)

type sfRange struct {
	name     string
	min, max int64
	with     envMap // what else must change so that the edge itself passes the cross-checks
}

var sfRanges = []sfRange{
	{EnvEventsPollMS, 2000, 60000, envMap{}},
	{EnvEventsMaxStreamMS, 60000, 900000, envMap{EnvEventsPollMS: "2000"}},
	{EnvEventsWriteTimeoutMS, 1000, 30000, envMap{}},
	{EnvEventsReadTimeoutMS, 500, 10000, envMap{EnvEventsPollMS: "10000"}},
	{EnvEventsMaxEventBytes, 1024, 1048576, envMap{}},
	{EnvEventsMaxStreams, 1, 1000, envMap{EnvEventsMaxStreamsPerPrincipal: "1"}},
	{EnvEventsMaxStreamsPerPrincipal, 1, 10, envMap{}},
	{EnvEventsMaxBatch, 1, 500, envMap{}},
}

func sfWith(name string, v int64, extra envMap) envMap {
	e := sfEventsEnv()
	for k, x := range extra {
		e[k] = x
	}
	e[name] = strconv.FormatInt(v, 10)
	return e
}

// Both edges of every range are accepted: the route builds.
func TestEachEventsSettingAcceptsBothEdgesOfItsRange(t *testing.T) {
	for _, r := range sfRanges {
		for _, v := range []int64{r.min, r.max} {
			e := sfWith(r.name, v, r.with)
			if r.name == EnvEventsPollMS && v == r.max {
				e[EnvEventsMaxStreamMS] = "300000"
			}
			if h, gaps, err := buildEventsSurface(osEnv(e), sfClock(), &failingDBs{}); h == nil || len(gaps) != 0 || err != nil {
				t.Errorf("%s=%d: handler %v, gaps %v, error %v; want a handler", r.name, v, h, gaps, err)
			}
		}
	}
}

// One step outside either edge is fatal, names the setting and never the value, whether the others are given or absent (A469, A658).
func TestEachEventsSettingOutsideItsRangeIsFatalAndNamesOnlyTheSetting(t *testing.T) {
	for _, r := range sfRanges {
		for _, v := range []int64{r.min - 1, r.max + 1} {
			if v < 1 {
				continue // zero is covered by the whole-number test
			}
			for _, others := range []bool{true, false} {
				e := envMap{r.name: strconv.FormatInt(v, 10)}
				if others {
					e = sfWith(r.name, v, r.with)
				}
				h, gaps, err := buildEventsSurface(osEnv(e), sfClock(), &failingDBs{})
				if h != nil || len(gaps) != 0 || err == nil || !strings.Contains(err.Error(), r.name) || strings.Contains(err.Error(), strconv.FormatInt(v, 10)) {
					t.Errorf("%s=%d (others given %v): handler %v, gaps %v, error %v; want a fatal error naming the setting only", r.name, v, others, h, gaps, err)
				}
			}
		}
	}
}

// buildEvents (the loader) enforces the same ranges, so the route cannot be built around the surface.
func TestTheEventsLoaderEnforcesTheRangesToo(t *testing.T) {
	for _, r := range sfRanges {
		e := sfWith(r.name, r.max+1, r.with)
		if h, err := buildEvents(osEnv(e), sfClock(), &failingDBs{}); h != nil || err == nil || !strings.Contains(err.Error(), r.name) {
			t.Errorf("%s above its range: handler %v, error %v; want a fatal error naming it", r.name, h, err)
		}
	}
}

// The cross-checks fail closed and name both settings: POLL below MAX_STREAM, READ not above POLL, PER_PRINCIPAL not above MAX_STREAMS.
func TestTheEventsCrossChecksFailClosedAndNameBothSettings(t *testing.T) {
	for name, c := range map[string]struct {
		set  envMap
		both []string
		ok   bool
	}{
		"poll equal to the stream ceiling":      {envMap{EnvEventsPollMS: "60000", EnvEventsMaxStreamMS: "60000"}, []string{EnvEventsPollMS, EnvEventsMaxStreamMS}, false},
		"poll just below the stream ceiling":    {envMap{EnvEventsPollMS: "59999", EnvEventsMaxStreamMS: "60000"}, nil, true},
		"read above poll":                       {envMap{EnvEventsPollMS: "2000", EnvEventsReadTimeoutMS: "2001"}, []string{EnvEventsReadTimeoutMS, EnvEventsPollMS}, false},
		"read equal to poll":                    {envMap{EnvEventsPollMS: "2000", EnvEventsReadTimeoutMS: "2000"}, nil, true},
		"per principal above the global cap":    {envMap{EnvEventsMaxStreams: "2", EnvEventsMaxStreamsPerPrincipal: "3"}, []string{EnvEventsMaxStreamsPerPrincipal, EnvEventsMaxStreams}, false},
		"per principal equal to the global cap": {envMap{EnvEventsMaxStreams: "3", EnvEventsMaxStreamsPerPrincipal: "3"}, nil, true},
	} {
		e := sfEventsEnv()
		for k, v := range c.set {
			e[k] = v
		}
		h, gaps, err := buildEventsSurface(osEnv(e), sfClock(), &failingDBs{})
		if c.ok {
			if h == nil || len(gaps) != 0 || err != nil {
				t.Errorf("%s: handler %v, gaps %v, error %v; want a handler", name, h, gaps, err)
			}
			continue
		}
		if h != nil || len(gaps) != 0 || err == nil {
			t.Errorf("%s: handler %v, gaps %v, error %v; want a fatal error", name, h, gaps, err)
			continue
		}
		for _, n := range c.both {
			if !strings.Contains(err.Error(), n) {
				t.Errorf("%s: error %q does not name %s", name, err, n)
			}
		}
		for _, v := range c.set {
			if strings.Contains(err.Error(), v) {
				t.Errorf("%s: error %q carries the value %s", name, err, v)
			}
		}
	}
}

// A missing setting stays a named gap even when the ones that are given sit at their ranges' edges, and no setting has a default: with one absent the route is not built.
func TestAnAbsentEventsSettingStaysAGapNextToValuesAtTheEdges(t *testing.T) {
	e := sfEventsEnv()
	e[EnvEventsPollMS] = "60000"
	delete(e, EnvEventsMaxStreamMS) // the cross-check cannot run without it, so it is a gap, not an error
	h, gaps, err := buildEventsSurface(osEnv(e), sfClock(), &failingDBs{})
	if want := "events route: " + EnvEventsMaxStreamMS + " missing"; h != nil || err != nil || len(gaps) != 1 || gaps[0] != want {
		t.Fatalf("handler %v, gaps %v, error %v; want no handler and [%q]", h, gaps, err, want)
	}
}

// The approved fixture values (A658) are all inside their ranges and build the route.
func TestTheApprovedEventsValuesBuildTheRoute(t *testing.T) {
	e := envMap{EnvEventsPollMS: "2000", EnvEventsMaxStreamMS: "300000", EnvEventsWriteTimeoutMS: "5000", EnvEventsReadTimeoutMS: "2000", EnvEventsMaxEventBytes: "65536",
		EnvEventsMaxStreams: "100", EnvEventsMaxStreamsPerPrincipal: "3", EnvEventsMaxBatch: "100"}
	if h, gaps, err := buildEventsSurface(osEnv(e), sfClock(), &failingDBs{}); h == nil || len(gaps) != 0 || err != nil {
		t.Fatalf("handler %v, gaps %v, error %v; want a handler", h, gaps, err)
	}
}
