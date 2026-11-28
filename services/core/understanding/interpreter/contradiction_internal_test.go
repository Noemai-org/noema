package interpreter

// INP-01, slice 3: the comparison itself, on raw documents (the gate's currency rule keeps a unit other than the pack's out of an end-to-end case). SYNTHETIC.

import (
	"encoding/json"
	"testing"
)

func docOf(t *testing.T, slots string) map[string]json.RawMessage {
	t.Helper()
	var d map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`{"slots":`+slots+`}`), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestWhichChangesOfASlotAreAContradiction(t *testing.T) {
	for name, c := range map[string]struct {
		prior, next string
		want        bool
	}{
		"a different unit":                            {`{"amount":{"type":"Quantity","value":5,"unit":"NGN"}}`, `{"amount":{"type":"Quantity","value":5,"unit":"USD"}}`, true},
		"a different value":                           {`{"amount":{"type":"Quantity","value":5,"unit":"NGN"}}`, `{"amount":{"type":"Quantity","value":6,"unit":"NGN"}}`, true},
		"the same, another confidence":                {`{"amount":{"type":"Quantity","value":5,"unit":"NGN","confidence":9000}}`, `{"amount":{"type":"Quantity","value":5,"unit":"NGN","confidence":9900}}`, false},
		"a different name said":                       {`{"counterparty":{"type":"Party","surface_form":"Tunde"}}`, `{"counterparty":{"type":"Party","surface_form":"Femi"}}`, true},
		"the same name":                               {`{"counterparty":{"type":"Party","surface_form":"Tunde"}}`, `{"counterparty":{"type":"Party","surface_form":"Tunde"}}`, false},
		"a window said differently":                   {`{"when":{"type":"TimeRef","surface_form":"this morning","window":["a","b"]}}`, `{"when":{"type":"TimeRef","surface_form":"earlier today","window":["a","b"]}}`, false},
		"a window moved":                              {`{"when":{"type":"TimeRef","window":["a","b"]}}`, `{"when":{"type":"TimeRef","window":["a","c"]}}`, true},
		"the slot is gone":                            {`{"amount":{"type":"Quantity","value":5,"unit":"NGN"}}`, `{}`, false},
		"the value is null now":                       {`{"amount":{"type":"Quantity","value":5,"unit":"NGN"}}`, `{"amount":{"type":"Quantity","value":null,"unit":"NGN"}}`, false},
		"the prior held none":                         {`{"amount":{"type":"Quantity","value":null,"unit":null}}`, `{"amount":{"type":"Quantity","value":6,"unit":"NGN"}}`, false},
		"another type under the name":                 {`{"amount":{"type":"Quantity","value":5,"unit":"NGN"}}`, `{"amount":{"type":"Party","surface_form":"x"}}`, false},
		"a resource, both resolved":                   {`{"r":{"type":"Resource","status":"RESOLVED","resolved_ref":"a:1"}}`, `{"r":{"type":"Resource","status":"RESOLVED","resolved_ref":"a:2"}}`, true},
		"a resource, now unresolved":                  {`{"r":{"type":"Resource","status":"RESOLVED","resolved_ref":"a:1"}}`, `{"r":{"type":"Resource","status":"UNRESOLVED"}}`, false},
		"a resource, unresolved with a stale ref":     {`{"r":{"type":"Resource","status":"UNRESOLVED","resolved_ref":"a:1"}}`, `{"r":{"type":"Resource","status":"RESOLVED","resolved_ref":"a:2"}}`, false},
		"a resource, now unresolved with a stale ref": {`{"r":{"type":"Resource","status":"RESOLVED","resolved_ref":"a:1"}}`, `{"r":{"type":"Resource","status":"UNRESOLVED","resolved_ref":"a:2"}}`, false},
		"another type under the name, same member":    {`{"x":{"type":"Quantity","value":5}}`, `{"x":{"type":"Reference","value":"STATUS"}}`, false},
		"a resource, was unresolved":                  {`{"r":{"type":"Resource","status":"UNRESOLVED"}}`, `{"r":{"type":"Resource","status":"RESOLVED","resolved_ref":"a:2"}}`, false},
	} {
		if got := contradicts(docOf(t, c.prior), docOf(t, c.next)); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}
