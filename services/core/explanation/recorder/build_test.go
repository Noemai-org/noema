package recorder

// The rules that decide which event is written, tested without a database (the Record-backed tests in recorder_test.go run against PostgreSQL with pgsodium).
// REQ-AUD-002, REQ-AUD-003, ruling A154. SYNTHETIC.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

func pl(ev chain.Event) map[string]any { return ev.Payload.(map[string]any) }

const (
	version = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tplID   = "en-NG/fin.outcome.exception.status_explained/SUCCESS_CONFIRMED"
)

var subj = explanation.Subject{TenantID: "ten_aa", PrincipalID: "usr_88213"}

func ok(text string, facts ...string) explanation.Event {
	return explanation.Event{Rendered: true, CorrelationID: "corr_c1", Text: text, FactIDs: facts, TemplateID: tplID, TemplateVersion: version}
}

func TestTheRenderedEventIsTheOneTheReconstructionReads(t *testing.T) {
	ev, err := build(subj, ok("Your transfer of NGN 5,000 has settled.", "f1"))
	if err != nil {
		t.Fatal(err)
	}
	if ev.EventType != "EXPLANATION_RENDERED" || ev.WrittenBy != "core-edge" || ev.Actor != "core-edge" || ev.ObjectRef != "exp_c1" || ev.TenantID != "ten_aa" || ev.CorrelationID != "corr_c1" {
		t.Fatalf("event %+v", ev)
	}
	claim := pl(ev)["claims"].([]any)[0].(map[string]any)
	want := map[string]any{"text": "Your transfer of NGN 5,000 has settled.", "disclosed_refs": []string{}, "bound_fact_status": "KNOWN", "fact_ids": []string{"f1"}, "fact_id": "f1"}
	if !reflect.DeepEqual(claim, want) {
		t.Fatalf("claim %v, want %v", claim, want)
	}
	if pl(ev)["validator_result"] != "PASSED" || pl(ev)["template_id"] != tplID || pl(ev)["template_version"] != version {
		t.Fatalf("payload %v", pl(ev))
	}
}

func TestAnEventBoundToNoFactOrToSeveralFacts(t *testing.T) {
	none, _ := build(subj, ok("Your transfer has settled."))
	c := pl(none)["claims"].([]any)[0].(map[string]any)
	if v, has := c["bound_fact_status"]; !has || v != nil {
		t.Errorf("no fact: bound_fact_status %v (present %v)", v, has)
	}
	for _, k := range []string{"fact_id", "fact_ids"} {
		if _, has := c[k]; has {
			t.Errorf("no fact: %s present", k)
		}
	}
	two, _ := build(subj, ok("Your transfer of NGN 5,000 to the account ending 0001 has settled.", "f1", "f2"))
	c = pl(two)["claims"].([]any)[0].(map[string]any)
	if _, has := c["fact_id"]; has || c["bound_fact_status"] != "KNOWN" || !reflect.DeepEqual(c["fact_ids"], []string{"f1", "f2"}) {
		t.Errorf("two facts: %v", c)
	}
}

func TestTheBlockedEventCarriesRuleCodesAndNoText(t *testing.T) {
	ev, err := build(subj, explanation.Event{CorrelationID: "corr_c1", Rules: []string{"FORBIDDEN_CLAIM", "UNSUPPORTED_NAME"}})
	if err != nil {
		t.Fatal(err)
	}
	if ev.EventType != "EXPLANATION_BLOCKED" || pl(ev)["rule"] != "FORBIDDEN_CLAIM" || !reflect.DeepEqual(pl(ev)["rules"], []string{"FORBIDDEN_CLAIM", "UNSUPPORTED_NAME"}) {
		t.Fatalf("event %+v", ev)
	}
	if len(pl(ev)) != 2 {
		t.Fatalf("payload %v: rule and rules only", pl(ev))
	}
}

func TestWhatTheReconstructionCouldNotServeIsRefusedBeforeItIsBuilt(t *testing.T) {
	long := strings.Repeat("a ", 150) + "b"  // 301 characters
	edge := strings.Repeat("a ", 149) + "bb" // 300 characters
	wide := strings.Repeat("é", 300)         // 300 characters, 600 bytes: the limit counts characters
	bad := []struct {
		name string
		ev   explanation.Event
	}{
		{"a text of 301 characters", ok(long)},
		{"an empty text", ok("")},
		{"a digit run of 5", ok("Your transfer of 12345 has settled.")},
		{"a digit run of 5 in another script", ok("Your transfer of ١٢٣٤٥ has settled.")},
		{"a template id of the wrong shape", func() explanation.Event { e := ok("x y"); e.TemplateID = "nope"; return e }()},
		{"a version of the wrong shape", func() explanation.Event { e := ok("x y"); e.TemplateVersion = "sha256:abc"; return e }()},
		{"a rule on a rendered event", func() explanation.Event { e := ok("x y"); e.Rules = []string{"X"}; return e }()},
		{"a repeated fact id", ok("x y", "f1", "f1")},
		{"a fact id with a space", ok("x y", "f 1")},
		{"a blocked event without a rule", explanation.Event{CorrelationID: "corr_c1"}},
		{"a blocked event with a lower-case rule", explanation.Event{CorrelationID: "corr_c1", Rules: []string{"x"}}},
		{"a blocked event with text", explanation.Event{CorrelationID: "corr_c1", Rules: []string{"X"}, Text: "x y"}},
		{"a blocked event with a fact", explanation.Event{CorrelationID: "corr_c1", Rules: []string{"X"}, FactIDs: []string{"f1"}}},
		{"a blocked event with a template", explanation.Event{CorrelationID: "corr_c1", Rules: []string{"X"}, TemplateID: tplID}},
		{"no correlation id", explanation.Event{Rules: []string{"X"}}},
		{"a bad correlation id", explanation.Event{CorrelationID: "corr 1", Rules: []string{"X"}}},
		{"a correlation id with a trailing newline", explanation.Event{CorrelationID: "corr_c1\n", Rules: []string{"X"}}},
	}
	for _, c := range bad {
		if _, err := build(subj, c.ev); err == nil {
			t.Errorf("%s: built", c.name)
		}
	}
	for name, text := range map[string]string{"exactly 300 characters": edge, "300 wide characters": wide, "a digit run of 4": "Your transfer ending 0001 has settled."} {
		if _, err := build(subj, ok(text)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
