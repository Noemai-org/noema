package trustlimits_test

// A544 (interim; REQ-RES-002, REQ-TRU-006; INV-14, AGENT-16): trust-limits.v1 gains four optional members that are routing data of the pack (the escalation reason of a classification,
// the capabilities a freeze allows, the reason of a clock phase, the entity a clock anchor is read from). The loader still reads exactly the contract: a document that carries them is
// accepted and its two limits are unchanged, and a member the contract does not have is still refused. Nothing here reads the routing: its consumers are other packages' (EL). SYNTHETIC.

import (
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/trustlimits"
)

const routed = `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","ambiguity_absolute_floor_bp":7000,"clock_at_risk_remaining_bp":2500,"confirmation_ttl_ceiling_ms":600000,` +
	`"escalation_reasons":[{"classification":"CONTRADICTORY_STATE","reason_code":"fin.reason.contradictory_state"}],` +
	`"allowed_under_freeze":["fin.cap.create_escalation_case"],` +
	`"phase_routes":[{"phase":"BREACHED","reason_code":"fin.reason.sla_breached"}],` +
	`"anchor_entities":[{"event":"complaint_logged","entity":"complaint_ref","field":"effective_at"}]}`

func TestADocumentWithTheRoutingMembersLoadsAndKeepsItsTwoLimits(t *testing.T) {
	l, err := trustlimits.Load(strings.NewReader(routed), "fin")
	if err != nil {
		t.Fatalf("a document with the routing members was refused: %v", err)
	}
	if floor, _ := l.AmbiguityAbsoluteFloorBP(); floor != 7000 {
		t.Errorf("floor %d; want 7000", floor)
	}
	if atRisk, _ := l.ClockAtRiskRemainingBP(); atRisk != 2500 {
		t.Errorf("at risk %d; want 2500", atRisk)
	}
}

func TestAMemberTheContractDoesNotHaveIsStillRefused(t *testing.T) {
	mustRefuse(t, "an unknown member next to the routing members", strings.Replace(routed, `"allowed_under_freeze"`, `"allowed_always":["x"],"allowed_under_freeze"`, 1))
	mustRefuse(t, "a routing member of the wrong type", strings.Replace(routed, `"allowed_under_freeze":["fin.cap.create_escalation_case"]`, `"allowed_under_freeze":"fin.cap.create_escalation_case"`, 1))
}

// JSON Schema cannot say "one entry per classification, phase or event" (uniqueItems compares whole items), so Load does, and packcheck says it again for the pack.
func TestOneEntryPerClassificationPhaseAndEventAndNoEmptyList(t *testing.T) {
	dupClass := `{"classification":"CONTRADICTORY_STATE","reason_code":"fin.reason.contradictory_state"}`
	mustRefuse(t, "a classification routed twice", strings.Replace(routed, dupClass, dupClass+`,{"classification":"CONTRADICTORY_STATE","reason_code":"fin.reason.execution_outcome_unknown"}`, 1))
	dupPhase := `{"phase":"BREACHED","reason_code":"fin.reason.sla_breached"}`
	mustRefuse(t, "a phase routed twice", strings.Replace(routed, dupPhase, dupPhase+`,{"phase":"BREACHED","reason_code":"fin.reason.sla_at_risk"}`, 1))
	dupEvent := `{"event":"complaint_logged","entity":"complaint_ref","field":"effective_at"}`
	mustRefuse(t, "an event with two anchors", strings.Replace(routed, dupEvent, dupEvent+`,{"event":"complaint_logged","entity":"case","field":"effective_at"}`, 1))
	mustRefuse(t, "an empty freeze list", strings.Replace(routed, `["fin.cap.create_escalation_case"]`, `[]`, 1))
}
