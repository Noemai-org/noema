package interpreter_test

// INP-01, slice 1 (A743, A404, A712; REQ-INP-004, REQ-INT-004; INV-02, INV-12; PUS-08): a follow-up turn is compiled alone and saved as version N+1 of the prior intent, with
// `supersedes` naming it. The identity of the version (intent id, version, supersedes) is the core's: whatever the model wrote there is overwritten. A prior that is not the
// session's own, or not of this investigation, is refused before the model service is called. No database: spies, as in interpreter_test.go. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

// firstTurn runs a first turn through the real gate and returns what the store was given: the prior of the follow-up.
func firstTurn(t *testing.T) intentstore.Admitted {
	t.Helper()
	w := newWorld(t, proposal(nil))
	if _, err := w.in.Interpret(context.Background(), input()); err != nil {
		t.Fatal(err)
	}
	return w.save.saved[0]
}

// withPrior is a prior of the given version: the first turn's row with its version moved (the document is rewritten, as a store of that version would hold it).
func withPrior(t *testing.T, version int) *intentstore.Admitted {
	t.Helper()
	p := firstTurn(t)
	var doc map[string]any
	if err := json.Unmarshal(p.JSON, &doc); err != nil {
		t.Fatal(err)
	}
	doc["version"] = version
	if version > 1 {
		doc["supersedes"] = fmt.Sprintf("%s@%d", p.IntentID, version-1)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	p.Version, p.JSON = version, b
	if version > 1 {
		p.Supersedes = fmt.Sprintf("%s@%d", p.IntentID, version-1)
	}
	return &p
}

func followUp(t *testing.T, prior *intentstore.Admitted, model []byte) (*world, interpreter.Result, error) {
	t.Helper()
	w := newWorld(t, model)
	in := input()
	in.Prior = prior
	res, err := w.in.Interpret(context.Background(), in)
	return w, res, err
}

// REQ-INT-004: the follow-up is version N+1 of the prior's intent, with supersedes naming the prior, under the chain's correlation id, whatever identity the model proposed.
func TestAFollowUpIsSavedAsTheNextVersionOfThePriorSupersedingIt(t *testing.T) {
	prior := withPrior(t, 1)
	model := proposal(func(m map[string]any) { m["intent_id"] = "int_MODELS_OWN"; m["version"] = 1 })
	w, res, err := followUp(t, prior, model)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.save.saved) != 1 {
		t.Fatalf("saved %d, want 1", len(w.save.saved))
	}
	a := w.save.saved[0]
	if a.IntentID != prior.IntentID || a.Version != 2 || a.Supersedes != prior.IntentID+"@1" || a.CorrelationID != corr || a.TenantID != session.TenantID {
		t.Errorf("saved %s v%d supersedes %q corr %s; want %s v2 supersedes %s@1 under %s", a.IntentID, a.Version, a.Supersedes, a.CorrelationID, prior.IntentID, prior.IntentID, corr)
	}
	if string(res.Intent.Object().IntentId) != prior.IntentID || int(res.Intent.Object().Version) != 2 {
		t.Errorf("the admitted intent is %s v%d", res.Intent.Object().IntentId, res.Intent.Object().Version)
	}
}

// INV-02: nothing the model wrote about the identity of the version survives, not even a version that would look right.
func TestWhatTheModelWroteAboutTheVersionChainIsOverwritten(t *testing.T) {
	prior := withPrior(t, 3)
	for name, mutate := range map[string]func(map[string]any){
		"version far ahead":         func(m map[string]any) { m["version"] = 9; m["supersedes"] = "int_01J9X3@8" },
		"another intent's link":     func(m map[string]any) { m["version"] = 4; m["supersedes"] = "int_OTHER@3" },
		"a link on a first version": func(m map[string]any) { m["version"] = 1; m["supersedes"] = "int_01J9X3@7" },
		"nothing":                   func(m map[string]any) {},
	} {
		w, _, err := followUp(t, prior, proposal(mutate))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if a := w.save.saved[0]; a.Version != 4 || a.Supersedes != prior.IntentID+"@3" || a.IntentID != prior.IntentID {
			t.Errorf("%s: saved %s v%d supersedes %q; want %s v4 supersedes %s@3", name, a.IntentID, a.Version, a.Supersedes, prior.IntentID, prior.IntentID)
		}
	}
}

// REQ-INT-004 property: for any prior version, the follow-up is exactly the next one and links to it.
func TestPropertyAFollowUpIsAlwaysThePriorsNextVersion(t *testing.T) {
	base := withPrior(t, 1)
	rapid.Check(t, func(rt *rapid.T) {
		v := rapid.IntRange(1, 100000).Draw(rt, "prior version")
		prior := *base
		var doc map[string]any
		if err := json.Unmarshal(prior.JSON, &doc); err != nil {
			rt.Fatal(err)
		}
		doc["version"] = v
		if v > 1 {
			doc["supersedes"] = fmt.Sprintf("%s@%d", prior.IntentID, v-1)
		}
		prior.JSON, prior.Version = mustJSON(rt, doc), v
		w, _, err := followUp(t, &prior, proposal(func(m map[string]any) { m["version"] = rapid.IntRange(1, 50).Draw(rt, "model version") }))
		if err != nil {
			rt.Fatal(err)
		}
		if a := w.save.saved[0]; a.Version != v+1 || a.Supersedes != fmt.Sprintf("%s@%d", prior.IntentID, v) {
			rt.Fatalf("prior v%d: saved v%d supersedes %q", v, a.Version, a.Supersedes)
		}
	})
}

func mustJSON(t interface{ Fatal(...any) }, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func replaceOnce(s, old, new string) string {
	i := strings.Index(s, old)
	if i < 0 {
		panic("replaceOnce: not found")
	}
	return s[:i] + new + s[i+len(old):]
}

// REQ-INP-004, INV-12: a prior that is another tenant's, another principal's or another investigation's is refused as an input error before the model service is called.
func TestAPriorThatIsNotThisSessionsOwnInvestigationIsRefusedBeforeTheModelIsCalled(t *testing.T) {
	cases := map[string]func(*intentstore.Admitted){
		"another tenant":        func(p *intentstore.Admitted) { p.TenantID = "ten_other" },
		"another investigation": func(p *intentstore.Admitted) { p.CorrelationID = "corr_other1" },
		"another principal": func(p *intentstore.Admitted) {
			p.JSON = []byte(replaceOnce(string(p.JSON), session.PrincipalID, "usr_99999"))
		},
		"a version below one": func(p *intentstore.Admitted) { p.Version = 0 },
		"no document":         func(p *intentstore.Admitted) { p.JSON = nil },
	}
	for name, mutate := range cases {
		prior := withPrior(t, 1)
		mutate(prior)
		w, _, err := followUp(t, prior, proposal(nil))
		if !errors.Is(err, interpreter.ErrInput) {
			t.Errorf("%s: err %v, want ErrInput", name, err)
		}
		if len(w.calls.order) != 0 || len(w.save.saved) != 0 {
			t.Errorf("%s: calls %v, saved %d; want nothing called and nothing saved", name, w.calls.order, len(w.save.saved))
		}
	}
}

// A first turn (no prior) is exactly what it was: the model's own identity stands (version 1, no link).
func TestATurnWithNoPriorKeepsTheFirstVersionIdentity(t *testing.T) {
	w := newWorld(t, proposal(nil))
	if _, err := w.in.Interpret(context.Background(), input()); err != nil {
		t.Fatal(err)
	}
	if a := w.save.saved[0]; a.Version != 1 || a.Supersedes != "" {
		t.Errorf("saved v%d supersedes %q", a.Version, a.Supersedes)
	}
}
