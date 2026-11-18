package publicstage_test

// EDG-01 / noema#601, ruling A243 (interim, relayed, not yet committed): the core owns the total map from the 21 internal investigation states to the six public stages of
// investigation-response.v1 (A187, A207, A211). Total: every state of transitions.yaml has a stage, a state outside it is an error and never a guess (fail closed), and no
// internal state name is ever a public value. REQ-API-001, REQ-UI-001, REQ-STM-001. Data is SYNTHETIC.

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/publicstage"
)

const (
	understanding = contracts.InvestigationResponseV1PublicStageUnderstanding
	needsInput    = contracts.InvestigationResponseV1PublicStageNeedsInput
	needsConfirm  = contracts.InvestigationResponseV1PublicStageNeedsConfirmation
	working       = contracts.InvestigationResponseV1PublicStageWorking
	answered      = contracts.InvestigationResponseV1PublicStageAnswered
	unavailable   = contracts.InvestigationResponseV1PublicStageUnavailable
)

// The agreed map, written out state by state so a reviewer reads it here and not only in the code (proposed on noema#601; A243).
var want = map[orchestrator.State]contracts.InvestigationResponseV1PublicStage{
	"RECEIVED": understanding, "INTERPRETING": understanding,
	"REJECTED_AT_GATE": needsInput, "AWAITING_CLARIFICATION": needsInput, "DORMANT": needsInput,
	"AWAITING_CONFIRMATION": needsConfirm,
	"EVALUATING":            working, "WAITING": working, "PLANNED": working, "EXECUTING": working, "AWAITING_VERIFICATION": working,
	"AWAITING_EXTERNAL_OUTCOME": working, "RECOVERING": working, "EXPLAINING": working, "EXPLAINING_BLOCKED": working,
	"BLOCKED_UNKNOWN": unavailable, "ESCALATING": unavailable, "ESCALATED": unavailable,
	"COMPLETED": answered, "REJECTED": answered, "EXPIRED": answered,
}

// Total: every state of the machine's own table has a stage, and the table here names no state the machine does not have.
func TestEveryInternalStateHasAPublicStage(t *testing.T) {
	tbl, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	states := tbl.States()
	if len(states) != len(want) {
		t.Fatalf("the machine has %d states and the agreed map names %d", len(states), len(want))
	}
	for _, s := range states {
		got, err := publicstage.Of(s)
		w, ok := want[s]
		if !ok {
			t.Errorf("%s: the machine has a state the agreed map does not name", s)
			continue
		}
		if err != nil || got != w {
			t.Errorf("%s: got %q, %v; want %q", s, got, err, w)
		}
	}
}

// A state outside the table is an error and never a stage: FROZEN is a flag and not a state (PERD 22.3), and case, blanks and unknown names are refused.
func TestAStateOutsideTheTableIsRefused(t *testing.T) {
	for _, s := range []orchestrator.State{"", "FROZEN", "received", " RECEIVED", "RECEIVED ", "UNKNOWN", "working", "NEW_STATE"} {
		if got, err := publicstage.Of(s); err == nil {
			t.Errorf("%q gave %q: want an error", s, got)
		}
	}
}

// All six public stages are reachable, and none is an internal state name (the client never sees the 21; A187).
func TestTheSixStagesAreAllUsedAndNoneIsAnInternalName(t *testing.T) {
	seen := map[contracts.InvestigationResponseV1PublicStage]bool{}
	for s, st := range want {
		seen[st] = true
		if string(st) == string(s) {
			t.Errorf("%s is its own public stage", s)
		}
	}
	if len(seen) != 6 {
		t.Errorf("%d public stages used, want all 6", len(seen))
	}
	for st := range seen {
		for s := range want {
			if string(st) == string(s) {
				t.Errorf("public stage %q equals internal state %q", st, s)
			}
		}
	}
}
