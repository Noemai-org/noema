package main

import (
	"github.com/Noemai-org/noema/services/core/edge/events"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// buildEventsSurface builds the events route over the Record reader (STM-01 surfaces; REQ-API-001; INV-12, AGENT-16; A330, A469, A486, interim, relayed, not yet committed). With no
// Record reader there is nothing to read from and no route (the reader is named as "record reader"). A setting that is given and invalid is an error that names it and never its value.
// With none given the route is the plain gap of Components.Events; with some absent the process starts and each absent setting is a gap line, "events route: NAME missing": fixed text,
// never a value. The settings are the eight of wire_events.go and none has a default.
func buildEventsSurface(env Env, clk clock.Clock, records reconstruct.TenantDBs) (*events.Handler, []string, error) {
	if records == nil {
		return nil, nil, nil
	}
	var absent []string
	for _, name := range eventsSettingNames {
		if env.Getenv(name) == "" {
			absent = append(absent, name)
			continue
		}
		if _, err := positive(env, name); err != nil {
			return nil, nil, err
		}
	}
	switch {
	case len(absent) == len(eventsSettingNames):
		return nil, nil, nil
	case len(absent) > 0:
		return nil, gapLines("events route", absent), nil
	}
	h, err := buildEvents(env, clk, records)
	if err != nil {
		return nil, nil, err
	}
	return h, nil, nil
}

// eventsSettingNames is every setting of the events route, in the one order its gaps are named.
var eventsSettingNames = []string{EnvEventsPollMS, EnvEventsMaxStreamMS, EnvEventsWriteTimeoutMS, EnvEventsReadTimeoutMS, EnvEventsMaxEventBytes, EnvEventsMaxStreams,
	EnvEventsMaxStreamsPerPrincipal, EnvEventsMaxBatch}

// gapLines names each absent setting of a part: "<part>: <NAME> missing". The text is fixed and carries no value.
func gapLines(part string, names []string) []string {
	lines := make([]string, 0, len(names))
	for _, n := range names {
		lines = append(lines, part+": "+n+" missing")
	}
	return lines
}
