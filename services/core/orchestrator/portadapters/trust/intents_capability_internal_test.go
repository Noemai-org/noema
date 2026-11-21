package trustport

// A370: the pure part of reading the capability an admitted intent asks for, without a database, so that every shape of a capability slot is checked on every run. REQ-TRU-004;
// INV-14, INV-16. The database-backed tests of the same behaviour are in intents_capability_test.go. SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

func rcObj(slots map[string]string) contracts.IntentObjectV3 {
	o := contracts.IntentObjectV3{Slots: map[string]contracts.IntentSlotV2Slot{}}
	for name, raw := range slots {
		o.Slots[name] = contracts.IntentSlotV2Slot(raw)
	}
	return o
}

func TestRequestedCapabilityOfEachShapeOfSlot(t *testing.T) {
	node := nodeInfo{capabilitySlots: []string{"first", "second"}}
	const ref = `{"type":"Reference","value":"x.cap.one"}`
	cases := map[string]struct {
		slots map[string]string
		want  string
		err   bool
	}{
		"no slot": {slots: map[string]string{}},
		"a slot the pack does not call a capability": {slots: map[string]string{"other": ref}},
		"a reference, no status":                     {slots: map[string]string{"first": ref}, want: "x.cap.one"},
		"resolved":                                   {slots: map[string]string{"first": `{"type":"Reference","value":"x.cap.one","status":"RESOLVED"}`}, want: "x.cap.one"},
		"absent":                                     {slots: map[string]string{"first": `{"type":"Reference","value":"x.cap.one","status":"ABSENT"}`}},
		"the second only":                            {slots: map[string]string{"second": ref}, want: "x.cap.one"},
		"one value twice":                            {slots: map[string]string{"first": ref, "second": ref}, want: "x.cap.one"},
		"absent and present":                         {slots: map[string]string{"first": `{"type":"Reference","value":"x.cap.two","status":"ABSENT"}`, "second": ref}, want: "x.cap.one"},
		"two values":                                 {slots: map[string]string{"first": ref, "second": `{"type":"Reference","value":"x.cap.two"}`}, err: true},
		"empty value":                                {slots: map[string]string{"first": `{"type":"Reference","value":""}`}, err: true},
		"number":                                     {slots: map[string]string{"first": `{"type":"Reference","value":7}`}, err: true},
		"no value":                                   {slots: map[string]string{"first": `{"type":"Reference"}`}, err: true},
		"wrong type":                                 {slots: map[string]string{"first": `{"type":"Resource","value":"x.cap.one","status":"RESOLVED"}`}, err: true},
		"wrong type, no status":                      {slots: map[string]string{"first": `{"type":"Resource","value":"x.cap.one"}`}, err: true},
		"ambiguous":                                  {slots: map[string]string{"first": `{"type":"Reference","value":"x.cap.one","status":"AMBIGUOUS"}`}, err: true},
		"unresolved":                                 {slots: map[string]string{"first": `{"type":"Reference","value":"x.cap.one","status":"UNRESOLVED"}`}, err: true},
		"a status that is no status":                 {slots: map[string]string{"first": `{"type":"Reference","value":"x.cap.one","status":"MAYBE"}`}, err: true},
		"null":                                       {slots: map[string]string{"first": `null`}, err: true},
		"a bare string":                              {slots: map[string]string{"first": `"x.cap.one"`}, err: true},
		"a second slot unreadable":                   {slots: map[string]string{"first": ref, "second": `{"type":"Reference","value":""}`}, err: true},
	}
	for name, c := range cases {
		got, err := requestedCapability(node, rcObj(c.slots))
		switch {
		case c.err && !errors.Is(err, ErrStoredIntent):
			t.Errorf("%s: got %q, %v; want ErrStoredIntent", name, got, err)
		case !c.err && (err != nil || got != c.want):
			t.Errorf("%s: got %q, %v; want %q", name, got, err, c.want)
		case c.err && got != "":
			t.Errorf("%s: an error returned %q", name, got)
		}
	}
}

// The pack decides which slots hold capabilities: a node with none asks for none whatever its slots hold.
func TestANodeWithNoCapabilitySlotAsksForNone(t *testing.T) {
	got, err := requestedCapability(nodeInfo{}, rcObj(map[string]string{"requested_remedy": `{"type":"Reference","value":"x.cap.one"}`}))
	if err != nil || got != "" {
		t.Errorf("got %q, %v; want none", got, err)
	}
}

func TestReadPackFindsTheCapabilitySlotsOfANodeAndNoOther(t *testing.T) {
	_, _, nodes, err := readPack([]byte(`{"pack_id":"fin","pack_version":"1.0.0",
	  "intent_nodes":[{"node_id":"n.one","trust_profile":null,"slots":[
	    {"name":"b_cap","type":"Reference","required":false,"references":"capability"},
	    {"name":"a_cap","type":"Reference","required":false,"references":"capability"},
	    {"name":"vocab","type":"Reference","required":true}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := nodes["n.one"].capabilitySlots
	if len(got) != 2 || got[0] != "a_cap" || got[1] != "b_cap" {
		t.Errorf("capabilitySlots = %v, want [a_cap b_cap]", got)
	}
}
