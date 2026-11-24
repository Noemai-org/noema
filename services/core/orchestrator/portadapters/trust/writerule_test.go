package trustport_test

// TRU-02 (A543, A364, interim, relayed, not yet committed): the production WriteRule answers from the pack's `write_when`, a predicate over the intent node's slot VALUES.
// REQ-TRU-004, REQ-NFR-002; INV-01 (no model, no clock, no I/O: a pure function of the pack and the slot values), INV-14 (no node or slot name in code), AGENT-09 (a slot value is
// data), AGENT-13 (no slot value in an error). Fail closed: an absent write_when never writes; a predicate that cannot be evaluated is an error and false, never true. All data is
// SYNTHETIC (AGENT-25); the real-pack tests read packs/fintech_v1.json and compare with an independent reader of the same file.

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

func wrSlots() []trustpack.WriteSlot {
	return []trustpack.WriteSlot{
		{Name: "kind", Type: "Reference", Required: true, Vocabulary: []string{"A", "B", "C"}},
		{Name: "tag", Type: "Reference"},
		{Name: "who", Type: "Party"},
	}
}

func wrNode(predicate string) trustpack.WriteWhen {
	return trustpack.WriteWhen{Predicate: predicate, Slots: wrSlots()}
}

func wrRule(t *testing.T, nodes map[string]trustpack.WriteWhen) *trustport.PackWriteRule {
	t.Helper()
	r, err := trustport.NewPackWriteRule(nodes)
	if err != nil {
		t.Fatalf("NewPackWriteRule: %v", err)
	}
	return r
}

func wrIntent(node string, slots map[string]string) intake.AdmittedIntent {
	a := admitted(node)
	a.Slots = slots
	return a
}

// One case per operator of the predicate language that a slot can meet (a slot is a Reference, an Enum of its vocabulary or opaque text, so ordering and arithmetic are refused at
// build, below).
func TestWriteRuleAnswersEachOperatorOfThePredicate(t *testing.T) {
	cases := []struct {
		name, predicate string
		slots           map[string]string
		want            bool
	}{
		{"== true", "kind == 'A'", map[string]string{"kind": "A"}, true},
		{"== false", "kind == 'A'", map[string]string{"kind": "B"}, false},
		{"!= true", "kind != 'A'", map[string]string{"kind": "B"}, true},
		{"!= false", "kind != 'A'", map[string]string{"kind": "A"}, false},
		{"in true", "kind in ['A', 'B']", map[string]string{"kind": "B"}, true},
		{"in false", "kind in ['A', 'B']", map[string]string{"kind": "C"}, false},
		{"not in true", "kind not in ['A', 'B']", map[string]string{"kind": "C"}, true},
		{"not in false", "kind not in ['A', 'B']", map[string]string{"kind": "A"}, false},
		{"and true", "kind == 'A' and tag != null", map[string]string{"kind": "A", "tag": "x"}, true},
		{"and false left", "kind == 'A' and tag != null", map[string]string{"kind": "B", "tag": "x"}, false},
		{"and false right", "kind == 'A' and tag != null", map[string]string{"kind": "A"}, false},
		{"or true left", "kind == 'A' or tag != null", map[string]string{"kind": "A"}, true},
		{"or true right", "kind == 'A' or tag != null", map[string]string{"kind": "B", "tag": "x"}, true},
		{"or false", "kind == 'A' or tag != null", map[string]string{"kind": "B"}, false},
		{"not true", "not (kind == 'A')", map[string]string{"kind": "B"}, true},
		{"not false", "not (kind == 'A')", map[string]string{"kind": "A"}, false},
		{"parentheses true", "kind == 'A' and (tag == 'x' or who != null)", map[string]string{"kind": "A", "who": "p"}, true},
		{"parentheses false", "kind == 'A' and (tag == 'x' or who != null)", map[string]string{"kind": "A"}, false},
		{"null == absent optional slot", "tag == null", map[string]string{"kind": "A"}, true},
		{"null == present optional slot", "tag == null", map[string]string{"kind": "A", "tag": "x"}, false},
		{"an empty value of an optional slot reads as null", "tag == null", map[string]string{"kind": "A", "tag": ""}, true},
		{"an opaque slot compares with null", "who != null", map[string]string{"kind": "A", "who": "p"}, true},
		{"an Enum slot compares with a text literal", "kind == 'C'", map[string]string{"kind": "C"}, true},
		{"literal true", "true", map[string]string{"kind": "A"}, true},
		{"literal false", "false", map[string]string{"kind": "A"}, false},
		{"an entry for a slot the node does not declare is ignored", "kind == 'A'", map[string]string{"kind": "A", "extra": "x"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := wrRule(t, map[string]trustpack.WriteWhen{"n": wrNode(c.predicate)})
			got, err := r.MayWrite(context.Background(), wrIntent("n", c.slots))
			if err != nil || got != c.want {
				t.Fatalf("MayWrite(%q over %v) = %v, %v; want %v, nil", c.predicate, c.slots, got, err, c.want)
			}
		})
	}
}

// Fail closed (REQ-NFR-002): a node with no write_when never writes, and says so without an error; a node the pack does not have has no answer.
func TestWriteRuleAbsentWriteWhenNeverWritesAndAnUnknownNodeHasNoAnswer(t *testing.T) {
	r := wrRule(t, map[string]trustpack.WriteWhen{"no_predicate": wrNode(""), "has": wrNode("true")})
	got, err := r.MayWrite(context.Background(), wrIntent("no_predicate", map[string]string{"kind": "A"}))
	if err != nil || got {
		t.Fatalf("a node with no write_when: MayWrite = %v, %v; want false, nil", got, err)
	}
	for _, node := range []string{"not_in_the_pack", "", "HAS", " has"} {
		got, err := r.MayWrite(context.Background(), wrIntent(node, map[string]string{"kind": "A"}))
		if !errors.Is(err, trustport.ErrNoWriteAnswer) || got {
			t.Fatalf("MayWrite(node %q) = %v, %v; want false and ErrNoWriteAnswer", node, got, err)
		}
	}
}

// A predicate that cannot be evaluated is not a write: false with ErrWriteUnevaluable, never true, whatever the rest of the predicate would say.
func TestWriteRuleAnUnevaluablePredicateIsNotAWrite(t *testing.T) {
	cases := []struct {
		name, predicate string
		slots           map[string]string
	}{
		{"a required slot has no value", "kind == 'A' or true", map[string]string{"tag": "x"}},
		{"a required slot has an empty value (unresolved)", "true", map[string]string{"kind": ""}},
		{"no slots at all", "true", nil},
		{"an Enum slot holds a value outside its vocabulary (the wrong kind)", "kind != 'A'", map[string]string{"kind": "ZZZ"}},
		{"an Enum slot holds a value of another case", "kind != 'A'", map[string]string{"kind": "a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := wrRule(t, map[string]trustpack.WriteWhen{"n": wrNode(c.predicate)})
			got, err := r.MayWrite(context.Background(), wrIntent("n", c.slots))
			if !errors.Is(err, trustport.ErrWriteUnevaluable) || got {
				t.Fatalf("MayWrite = %v, %v; want false and ErrWriteUnevaluable", got, err)
			}
		})
	}
}

// AGENT-13, AGENT-09: no slot value is written in any error of the rule.
func TestWriteRuleNoSlotValueAppearsInAnyError(t *testing.T) {
	r := wrRule(t, map[string]trustpack.WriteWhen{"n": wrNode("kind != 'A'")})
	secret := "SYNTH_SLOT_VALUE_7731"
	for name, slots := range map[string]map[string]string{
		"a value outside the vocabulary":      {"kind": secret},
		"a required slot missing, others set": {"tag": secret, "who": secret},
	} {
		got, err := r.MayWrite(context.Background(), wrIntent("n", slots))
		if err == nil || got {
			t.Fatalf("%s: MayWrite = %v, %v; want false and an error", name, got, err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("%s: the error names a slot value: %q", name, err)
		}
	}
	if _, err := r.MayWrite(context.Background(), wrIntent(secret, nil)); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("an unknown node: err = %v; want an error that does not name the node text", err)
	}
}

// A rule that cannot be built soundly is no rule (INV-16): a predicate that does not parse, names a slot the node does not have, is not Boolean, orders a slot, or names a value
// outside a vocabulary is refused whole, at build and not at run time.
func TestNewPackWriteRuleRefusesWhatCannotBeEvaluatedSoundly(t *testing.T) {
	for name, predicate := range map[string]string{
		"does not parse":                      "kind ==",
		"names a slot the node lacks":         "ghost == 'A'",
		"is not Boolean":                      "kind",
		"orders a slot":                       "tag < 'x'",
		"adds slots":                          "tag + tag == 'x'",
		"names a value out of the vocabulary": "kind == 'Q'",
		"calls the clock":                     "now() == now()",
		"counts an entity":                    "exists(case where true)",
	} {
		t.Run(name, func(t *testing.T) {
			r, err := trustport.NewPackWriteRule(map[string]trustpack.WriteWhen{"ok": wrNode("true"), "bad": wrNode(predicate)})
			if !errors.Is(err, trustport.ErrConfig) || r != nil {
				t.Fatalf("NewPackWriteRule = %v, %v; want no rule and ErrConfig", r, err)
			}
		})
	}
	for name, nodes := range map[string]map[string]trustpack.WriteWhen{"nil": nil, "empty": {}} {
		if r, err := trustport.NewPackWriteRule(nodes); !errors.Is(err, trustport.ErrConfig) || r != nil {
			t.Fatalf("%s map: NewPackWriteRule = %v, %v; want no rule and ErrConfig", name, r, err)
		}
	}
}

func TestWriteRuleZeroValueAndDoneContextAnswerNothing(t *testing.T) {
	var zero *trustport.PackWriteRule
	if got, err := zero.MayWrite(context.Background(), wrIntent("n", nil)); !errors.Is(err, trustport.ErrConfig) || got {
		t.Fatalf("nil rule: MayWrite = %v, %v; want false and ErrConfig", got, err)
	}
	if got, err := (&trustport.PackWriteRule{}).MayWrite(context.Background(), wrIntent("n", nil)); !errors.Is(err, trustport.ErrConfig) || got {
		t.Fatalf("zero rule: MayWrite = %v, %v; want false and ErrConfig", got, err)
	}
	r := wrRule(t, map[string]trustpack.WriteWhen{"n": wrNode("true")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := r.MayWrite(ctx, wrIntent("n", map[string]string{"kind": "A"})); !errors.Is(err, context.Canceled) || got {
		t.Fatalf("done context: MayWrite = %v, %v; want false and context.Canceled", got, err)
	}
}

// The rule is data the pack gave: changing the map it was built from afterwards changes nothing it answers.
func TestWriteRuleKeepsItsOwnCopyOfThePack(t *testing.T) {
	nodes := map[string]trustpack.WriteWhen{"n": wrNode("kind == 'A'")}
	r := wrRule(t, nodes)
	nodes["n"] = wrNode("true")
	nodes["n"].Slots[0].Name = "other"
	delete(nodes, "n")
	got, err := r.MayWrite(context.Background(), wrIntent("n", map[string]string{"kind": "B"}))
	if err != nil || got {
		t.Fatalf("MayWrite = %v, %v; want false, nil (kind == 'A' over B)", got, err)
	}
}

// --- the real pack ---

type wrRawNode struct {
	ID        string  `json:"node_id"`
	WriteWhen *string `json:"write_when"`
}

func wrRealPack(t testing.TB) (*trustpack.Pack, []wrRawNode) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "..", "packs")
	p, err := trustpack.LoadPack(os.DirFS(root), "fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "fintech_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Nodes []wrRawNode `json:"intent_nodes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return p, doc.Nodes
}

func wrRealRule(t testing.TB) (*trustport.PackWriteRule, string, string) {
	t.Helper()
	p, nodes := wrRealPack(t)
	ww, err := p.WriteWhens()
	if err != nil {
		t.Fatal(err)
	}
	r, err := trustport.NewPackWriteRule(ww)
	if err != nil {
		t.Fatalf("NewPackWriteRule(the real pack): %v", err)
	}
	with, without := "", ""
	for _, n := range nodes {
		if n.WriteWhen != nil && with == "" {
			with = n.ID
		}
		if n.WriteWhen == nil && without == "" {
			without = n.ID
		}
	}
	if with == "" || without == "" {
		t.Fatal("the pack must give one node with a write_when and one without")
	}
	return r, with, without
}

// The expected answers below is written from the pack's own predicate; if the pack changes it, this guard fails first and says so.
const wrRealPredicate = "question_type == 'ESCALATION_REQUEST' or requested_remedy != null"

func TestWriteRuleRealPackNodeWithWriteWhenAndNodeWithout(t *testing.T) {
	r, with, without := wrRealRule(t)
	_, nodes := wrRealPack(t)
	for _, n := range nodes {
		if n.ID == with && *n.WriteWhen != wrRealPredicate {
			t.Fatalf("the pack's write_when changed to %q: update the expected answers of this test", *n.WriteWhen)
		}
	}
	base := map[string]string{"subject_resource": "res_synth_1"}
	cases := []struct {
		name string
		put  map[string]string
		want bool
	}{
		{"escalation request", map[string]string{"question_type": "ESCALATION_REQUEST"}, true},
		{"other question, no remedy", map[string]string{"question_type": "STATUS_QUESTION"}, false},
		{"other question, a remedy", map[string]string{"question_type": "STATUS_QUESTION", "requested_remedy": "cap_synth_1"}, true},
		{"escalation request and a remedy", map[string]string{"question_type": "ESCALATION_REQUEST", "requested_remedy": "cap_synth_1"}, true},
		{"lower case is another value", map[string]string{"question_type": "escalation_request"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			slots := map[string]string{}
			for k, v := range base {
				slots[k] = v
			}
			for k, v := range c.put {
				slots[k] = v
			}
			got, err := r.MayWrite(context.Background(), wrIntent(with, slots))
			if err != nil || got != c.want {
				t.Fatalf("MayWrite = %v, %v; want %v, nil", got, err, c.want)
			}
		})
	}
	if got, err := r.MayWrite(context.Background(), wrIntent(without, base)); err != nil || got {
		t.Fatalf("the node with no write_when: MayWrite = %v, %v; want false, nil", got, err)
	}
	// A required slot of the node is missing: not evaluable, not a write, however the other slots read.
	for name, slots := range map[string]map[string]string{
		"no question_type":    {"subject_resource": "res_synth_1", "requested_remedy": "cap_synth_1"},
		"no subject_resource": {"question_type": "ESCALATION_REQUEST"},
	} {
		if got, err := r.MayWrite(context.Background(), wrIntent(with, slots)); !errors.Is(err, trustport.ErrWriteUnevaluable) || got {
			t.Fatalf("%s: MayWrite = %v, %v; want false and ErrWriteUnevaluable", name, got, err)
		}
	}
	if got, err := r.MayWrite(context.Background(), wrIntent("fin.synthetic.no_such_node", base)); !errors.Is(err, trustport.ErrNoWriteAnswer) || got {
		t.Fatalf("an unknown node: MayWrite = %v, %v; want false and ErrNoWriteAnswer", got, err)
	}
}

// INV-01, INV-21: the answer is a pure function of the pack and the slot values. Over any values it equals the expected answer, twice in a row, and no value reaches an error text.
func TestWriteRuleRealPackIsDeterministicAndEqualsTheExpectedAnswer(t *testing.T) {
	r, with, _ := wrRealRule(t)
	rapid.Check(t, func(t *rapid.T) {
		qt := rapid.OneOf(rapid.Just("ESCALATION_REQUEST"), rapid.StringMatching(`SYNTH_[A-Z_]{0,10}`), rapid.Just("")).Draw(t, "question_type")
		remedy := rapid.OneOf(rapid.Just(""), rapid.StringMatching(`SYNTH_cap_[a-z_]{1,10}`)).Draw(t, "requested_remedy")
		slots := map[string]string{"subject_resource": "res_synth_1", "question_type": qt}
		if remedy != "" || rapid.Bool().Draw(t, "empty remedy entry present") {
			slots["requested_remedy"] = remedy
		}
		a := wrIntent(with, slots)
		got1, err1 := r.MayWrite(context.Background(), a)
		got2, err2 := r.MayWrite(context.Background(), a)
		if got1 != got2 || (err1 == nil) != (err2 == nil) {
			t.Fatalf("two calls differ: %v, %v and %v, %v", got1, err1, got2, err2)
		}
		if qt == "" {
			if !errors.Is(err1, trustport.ErrWriteUnevaluable) || got1 {
				t.Fatalf("no question_type: MayWrite = %v, %v; want false and ErrWriteUnevaluable", got1, err1)
			}
		} else if want := qt == "ESCALATION_REQUEST" || remedy != ""; err1 != nil || got1 != want {
			t.Fatalf("MayWrite(%q, %q) = %v, %v; want %v, nil", qt, remedy, got1, err1, want)
		}
		if err1 != nil && remedy != "" && strings.Contains(err1.Error(), remedy) {
			t.Fatalf("the error names a slot value: %q", err1)
		}
		if a.Slots["question_type"] != qt {
			t.Fatal("the rule changed the intent's slots")
		}
	})
}

// The adapter and the rule together: Writes.NeedsWrite is the rule's answer, and an unevaluable predicate is an error, never a write.
func TestWriteRuleThroughWritesNeedsWrite(t *testing.T) {
	r, with, without := wrRealRule(t)
	w, err := trustport.NewWrites(badSwitch{}, r)
	if err != nil {
		t.Fatal(err)
	}
	slots := map[string]string{"subject_resource": "res_synth_1", "question_type": "ESCALATION_REQUEST"}
	if got, err := w.NeedsWrite(context.Background(), wrIntent(with, slots)); err != nil || !got {
		t.Fatalf("NeedsWrite(escalation) = %v, %v; want true, nil", got, err)
	}
	if got, err := w.NeedsWrite(context.Background(), wrIntent(without, slots)); err != nil || got {
		t.Fatalf("NeedsWrite(no write_when) = %v, %v; want false, nil", got, err)
	}
	if got, err := w.NeedsWrite(context.Background(), wrIntent(with, nil)); !errors.Is(err, trustport.ErrWriteUnevaluable) || got {
		t.Fatalf("NeedsWrite(no slots) = %v, %v; want false and ErrWriteUnevaluable", got, err)
	}
}

// INV-01, INV-14: the rule file imports no clock, I/O, randomness or model, and writes no node or slot name of the pack.
func TestWriteRuleSourceHasNoClockIONorPackName(t *testing.T) {
	src, err := os.ReadFile("writerule.go")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "writerule.go", src, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Imports) == 0 {
		t.Fatal("no imports read: the scan is vacuous")
	}
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		for _, banned := range []string{"time", "os", "io", "net", "database/sql", "math/rand", "crypto/rand", "reflect", "log"} {
			if path == banned || strings.HasPrefix(path, banned+"/") {
				t.Fatalf("writerule.go imports %q", path)
			}
		}
		if strings.Contains(path, "provider") || strings.Contains(path, "model") {
			t.Fatalf("writerule.go imports %q", path)
		}
	}
	_, nodes := wrRealPack(t)
	names := []string{"question_type", "requested_remedy", "subject_resource", "ESCALATION_REQUEST"}
	for _, n := range nodes {
		names = append(names, n.ID)
	}
	for _, name := range names {
		if strings.Contains(string(src), name) {
			t.Fatalf("writerule.go writes the pack name %q (INV-14)", name)
		}
	}
}
