package intake_test

import (
	"slices"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// REQ-TRU-002 "refuse before read", PERD 23.3: T1 to T6 never read about a target. The three ports that may (T7 candidates, T8 ownership, T9 evidence) are called by no
// step before T7, whatever refuses the request, and in a full evaluation only after the role check of T6.

var targetPorts = []string{"Candidates.ScopedCandidates", "Ownership.OwnsTarget", "Evidence.SnapshotFreshness"}

func TestNoStepBeforeT7CallsATargetPort(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(s *spy, r *intake.Request)
	}{
		{"T1 refuses", func(s *spy, r *intake.Request) { s.getErr = intake.ErrNotFound }},
		{"T1 refuses a malformed reference", func(s *spy, r *intake.Request) { r.IntentRef = "nope" }},
		{"T2 refuses", func(s *spy, r *intake.Request) { r.Session.PrincipalID = "" }},
		{"T3 refuses", func(s *spy, r *intake.Request) { s.needsWrite, s.enabled = true, false }},
		{"T4 refuses", func(s *spy, r *intake.Request) { s.capability.Prohibited = true }},
		{"T5 refuses", func(s *spy, r *intake.Request) { s.inScope = false }},
		{"T6 refuses", func(s *spy, r *intake.Request) { s.roleAllowed = false }},
		{"T6 refuses an unresolved intent", func(s *spy, r *intake.Request) {
			s.roleAllowed = false
			s.intent.View.RequiredSlots = map[string]decide.SlotStatus{"counterparty": decide.SlotAmbiguous}
		}},
		{"T5 refuses a request that names an unavailable source", func(s *spy, r *intake.Request) { s.inScope, r.EvidenceUnavailable = false, true }},
	} {
		s, r := newSpy(), request()
		c.set(s, &r)
		eval(t, s, r)
		for _, p := range targetPorts {
			if slices.Contains(s.calls, p) {
				t.Errorf("%s: %s was called before T7; calls %v", c.name, p, s.calls)
			}
		}
	}
}

func TestTheTargetPortsAreCalledOnlyAfterTheRoleCheck(t *testing.T) {
	s := newSpy()
	eval(t, s, request())
	role := slices.Index(s.calls, "Authority.RoleAllowed")
	if role < 0 {
		t.Fatalf("T6 never ran; calls %v", s.calls)
	}
	for _, p := range targetPorts[1:] { // a resolved intent needs no candidates
		i := slices.Index(s.calls, p)
		if i < 0 || i < role {
			t.Errorf("%s: at %d, want after the role check at %d; calls %v", p, i, role, s.calls)
		}
	}
	if slices.Contains(s.calls, targetPorts[0]) {
		t.Errorf("a resolved intent read candidates; calls %v", s.calls)
	}
}

// A missing T7, T8 or T9 port is a configuration error, never a decision that skips the step (REQ-NFR-002).
func TestAMissingTargetPortIsAnErrorNotADecision(t *testing.T) {
	for _, c := range []struct {
		name string
		drop func(p *intake.Ports)
	}{
		{"Candidates", func(p *intake.Ports) { p.Candidates = nil }},
		{"Ownership", func(p *intake.Ports) { p.Ownership = nil }},
		{"Evidence", func(p *intake.Ports) { p.Evidence = nil }},
	} {
		svc := newSpy().service()
		c.drop(&svc.Ports)
		if _, err := svc.Evaluate(ctx(t), request()); err == nil {
			t.Errorf("a nil %s port was accepted", c.name)
		}
	}
}
