package workflows_test

// TRU-10 (REQ-TRU-010, REQ-TRU-002; INV-03, INV-30; A539, interim, relayed, not yet committed): the ESCALATE disposition decision ID is plain data in the workflow's input and goes,
// unchanged, into the route grant request. The workflow reads no Record and decides nothing. EscalateInput and RouteGrantRequest are Go types only (no /contracts document, A623).
// Temporal's test environment, scripted ports. Every value is SYNTHETIC.

import (
	"testing"
)

// The disposition of the input reaches the route grant request the issuer is asked with: the issuer (Trust) names it on the route action decision.
func TestTheDispositionOfTheInputReachesTheRouteGrantRequest(t *testing.T) {
	s := newScript()
	in := input()
	in.DispositionDecisionID = "dec_disposition1"
	if _, err := run(t, s, in); err != nil {
		t.Fatal(err)
	}
	if len(s.issueReqs) != 1 {
		t.Fatalf("IssueRouteGrant was asked %d times, want once", len(s.issueReqs))
	}
	if got := s.issueReqs[0].DispositionDecisionID; got != "dec_disposition1" {
		t.Fatalf("the route grant request names the disposition %q, want dec_disposition1", got)
	}
}

// With no disposition in the input (a recovery that lost it, or an older start) the request carries none: the issuer re-reads it from the Record by correlation, and the workflow
// invents nothing (INV-30).
func TestAnInputWithNoDispositionPassesNoneOn(t *testing.T) {
	s := newScript()
	if _, err := run(t, s, input()); err != nil {
		t.Fatal(err)
	}
	if len(s.issueReqs) != 1 || s.issueReqs[0].DispositionDecisionID != "" {
		t.Fatalf("requests %+v, want one with no disposition", s.issueReqs)
	}
}
