package main

// STM-01 surfaces, cases added after the red push (found while writing the green; not seen red in CI): the shared run timeout does not ask for recovery, a gateway with no Recorder is a
// named gap, and a recovery that is not configured is still named by /readyz. REQ-STM-001, REQ-VER-004; AGENT-16; A486. Every value is SYNTHETIC.

import (
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
)

// NOEMA_INVESTIGATION_RUN_TIMEOUT_MS is the workers' setting too (A477): given alone it does not ask for recovery and is no gap of its own.
func TestTheSharedRunTimeoutAloneDoesNotAskForRecovery(t *testing.T) {
	s, gaps, err, _ := sfBuildRecovery(t, envMap{recovery.EnvRunTimeoutMS: "30000"}, SurfaceSeams{Recovery: &sfReader{}}, "ten_aa")
	if s != nil || len(gaps) != 0 || err != nil {
		t.Fatalf("surface %v, gaps %v, error %v; want none", s, gaps, err)
	}
}

// Every setting of the gateway and no Recorder (the orchestrator's login is not configured): a named gap, no worker started.
func TestAGatewayWithNoRecorderIsANamedGap(t *testing.T) {
	e, records, _ := sfGatewayWorld(t)
	svc, gaps, err := sfBuildGateway(t, e, records, nil, &fakeRunner{})
	if svc != nil || err != nil || len(gaps) != 1 || gaps[0] != "input gateway: recorder not built" {
		t.Fatalf("service %v, gaps %v, error %v; want [input gateway: recorder not built]", svc, gaps, err)
	}
}

// With no recovery setting at all, /readyz still names "recovery" (it is part of the slice), once, and never twice when a setting gap names it already.
func TestReadinessNamesRecoveryPlainlyWhenItIsNotConfigured(t *testing.T) {
	ready := func() (bool, string) { return true, "" }
	if ok, reason := withSurfaceReadiness(ready, &Surfaces{})(); ok || reason != "not configured: recovery" {
		t.Fatalf("ready %v, reason %q; want not ready and the plain name", ok, reason)
	}
	s := &Surfaces{Gaps: []string{"recovery: reader not built"}}
	if ok, reason := withSurfaceReadiness(ready, s)(); ok || strings.Count(reason, "recovery") != 1 {
		t.Fatalf("ready %v, reason %q; want recovery named once", ok, reason)
	}
}

// The body bound alone is the edge's setting: it does not configure a gateway, and is no gap of its own.
func TestTheBodyBoundAloneDoesNotConfigureAGateway(t *testing.T) {
	svc, gaps, err := sfBuildGateway(t, envMap{edge.EnvMaxBody: "16384"}, &failingDBs{}, nil, &fakeRunner{})
	if svc != nil || len(gaps) != 0 || err != nil {
		t.Fatalf("service %v, gaps %v, error %v; want none", svc, gaps, err)
	}
}
