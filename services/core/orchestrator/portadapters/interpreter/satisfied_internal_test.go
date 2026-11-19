package interpreterport

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// REQ-INT-002, INV-02, AGENT-09 (EL's read of noema#661, P2): `satisfied` must not depend on the gate for a slot's status. A `status` member that is present and is not
// the JSON string "RESOLVED" means unresolved, whatever else it is (null, a number, an empty string, another case, a boolean, an object). A slot with no `status` member
// is resolved only when it says it is a Reference, the one slot type the intent-object contract gives no status; any other type with no status is unresolved.
func TestASlotIsResolvedOnlyByTheStringRESOLVEDOrByBeingAReference(t *testing.T) {
	cases := map[string]struct {
		slot string
		want bool
	}{
		"RESOLVED":                       {`{"type":"Resource","status":"RESOLVED","resolved_ref":"res_X"}`, true},
		"status null":                    {`{"type":"Resource","status":null,"value":"x"}`, false},
		"status a number":                {`{"type":"Resource","status":1,"value":"x"}`, false},
		"status an empty string":         {`{"type":"Resource","status":"","value":"x"}`, false},
		"status lower case":              {`{"type":"Resource","status":"resolved","value":"x"}`, false},
		"status true":                    {`{"type":"Resource","status":true,"value":"x"}`, false},
		"status an object":               {`{"type":"Resource","status":{"a":1},"value":"x"}`, false},
		"status an array":                {`{"type":"Resource","status":["RESOLVED"],"value":"x"}`, false},
		"AMBIGUOUS":                      {`{"type":"Resource","status":"AMBIGUOUS"}`, false},
		"UNRESOLVED":                     {`{"type":"Resource","status":"UNRESOLVED"}`, false},
		"a Reference with no status":     {`{"type":"Reference","value":"fin.q.status"}`, true},
		"a Reference with a null status": {`{"type":"Reference","status":null,"value":"fin.q.status"}`, false},
		"a Resource with no status":      {`{"type":"Resource","value":"x"}`, false},
		"a Quantity with no status":      {`{"type":"Quantity","value":1,"unit":"NGN"}`, false},
		"no type and no status":          {`{"value":"x"}`, false},
		"a type that is not a string":    {`{"type":7,"value":"x"}`, false},
		"not an object":                  {`"RESOLVED"`, false},
		"an array":                       {`[]`, false},
		"null":                           {`null`, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := satisfied(map[string]contracts.IntentSlotV2Slot{"s": contracts.IntentSlotV2Slot(c.slot)}, []string{"s"})
			if got != c.want {
				t.Errorf("satisfied(%s) = %v, want %v", c.slot, got, c.want)
			}
		})
	}
}

// A required slot that is not in the object is unresolved, and no required slot at all is satisfied (the pack requires nothing of that node).
func TestAMissingRequiredSlotIsNotSatisfiedAndNoRequirementIs(t *testing.T) {
	slots := map[string]contracts.IntentSlotV2Slot{"a": contracts.IntentSlotV2Slot(`{"type":"Reference","value":"x"}`)}
	if satisfied(slots, []string{"a", "b"}) {
		t.Error("a missing required slot was taken as satisfied")
	}
	if !satisfied(slots, nil) {
		t.Error("an empty requirement was not satisfied")
	}
}
