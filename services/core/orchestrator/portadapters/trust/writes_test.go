package trustport_test

// The production adapter of intake.Writes (STM-01 follow-up; T3 of PERD 23.3; REQ-TRU-002, REQ-NFR-002; INV-01, INV-14): Enabled is the tenant's kill switch (real PostgreSQL),
// NeedsWrite is the pack's answer for the intent node, read through a WriteRule. Fail closed: no answer is an error, never "no write" and never "write". SYNTHETIC.

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"regexp"
	"testing"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// tableRule is a WriteRule over a table, the shape a pack-backed rule has: a node it lists has its answer, any other is ErrNoWriteAnswer. It records every node it is asked.
type tableRule struct {
	answers map[string]bool
	asked   []string
}

func (r *tableRule) MayWrite(_ context.Context, a intake.AdmittedIntent) (bool, error) {
	node := a.IntentNode
	r.asked = append(r.asked, node)
	v, ok := r.answers[node]
	if !ok {
		return false, trustport.ErrNoWriteAnswer
	}
	return v, nil
}

// badRule answers true and an error together, which a rule must not do: the adapter must still fail closed.
type badRule struct{ err error }

func (r badRule) MayWrite(context.Context, intake.AdmittedIntent) (bool, error) { return true, r.err }

// badSwitch answers true and an error together, which a switch must not do: the adapter must still fail closed.
type badSwitch struct{ err error }

func (s badSwitch) Enabled(context.Context, string) (bool, error) { return true, s.err }

func admitted(node string) intake.AdmittedIntent {
	return intake.AdmittedIntent{TenantID: tenantA, IntentID: "int_aaaa1", Version: 1, IntentNode: node, PrincipalID: user}
}

func newWrites(t *testing.T, sw trustport.WritesSwitch, rule trustport.WriteRule) *trustport.Writes {
	t.Helper()
	w, err := trustport.NewWrites(sw, rule)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestWritesEnabledIsTheTenantsOwnKillSwitch(t *testing.T) {
	w := newWorld(t)
	wr := newWrites(t, w.killSwitch(w.trustPools()), &tableRule{})
	for _, tenant := range []string{tenantA, tenantB} {
		if on, err := wr.Enabled(tctx(t), tenant); err != nil || !on {
			t.Fatalf("%s with the switch off: %v %v, want true", tenant, on, err)
		}
	}
	w.kill(tenantB)
	if on, err := wr.Enabled(tctx(t), tenantB); err != nil || on {
		t.Errorf("B with the switch on: %v %v, want false", on, err)
	}
	if on, err := wr.Enabled(tctx(t), tenantA); err != nil || !on {
		t.Errorf("A while only B is stopped: %v %v, want true", on, err)
	}
}

func TestWritesEnabledIsNeverTrueOnAnError(t *testing.T) {
	w := newWorld(t)
	wr := newWrites(t, w.killSwitch(w.trustPools()), &tableRule{})
	if on, err := wr.Enabled(tctx(t), "ten_cc"); on || !errors.Is(err, trustport.ErrNoPool) {
		t.Errorf("a tenant with no login: %v %v, want false and ErrNoPool", on, err)
	}
	if on, err := wr.Enabled(tctx(t), ""); on || !errors.Is(err, trustport.ErrSession) {
		t.Errorf("no tenant: %v %v, want false and ErrSession", on, err)
	}
	if _, err := w.admin.Exec(tctx(t), `REVOKE SELECT ON tenant FROM trust_rw`); err != nil {
		t.Fatal(err)
	}
	if on, err := wr.Enabled(tctx(t), tenantA); on || !errors.Is(err, trustport.ErrStore) {
		t.Errorf("a switch that cannot be read: %v %v, want false and ErrStore", on, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if on, err := wr.Enabled(cancelled, tenantA); on || err == nil {
		t.Errorf("a cancelled context: %v %v, want false and an error", on, err)
	}
	boom := errors.New("synthetic switch failure")
	if on, err := newWrites(t, badSwitch{boom}, &tableRule{}).Enabled(tctx(t), tenantA); on || !errors.Is(err, boom) {
		t.Errorf("a switch that answers true with an error: %v %v, want false and that error", on, err)
	}
}

func TestNeedsWriteIsThePacksAnswerPerNode(t *testing.T) {
	rule := &tableRule{answers: map[string]bool{"x.reads.only": false, "x.creates.case": true}}
	wr := newWrites(t, badSwitch{}, rule)
	for node, want := range rule.answers {
		got, err := wr.NeedsWrite(tctx(t), admitted(node))
		if err != nil || got != want {
			t.Errorf("%s: %v %v, want %v", node, got, err, want)
		}
	}
	if len(rule.asked) != 2 {
		t.Fatalf("the rule was asked %v, want one question per call", rule.asked)
	}
	rule.answers = map[string]bool{"x.reads.only": true, "x.creates.case": false}
	for node, want := range rule.answers {
		if got, err := wr.NeedsWrite(tctx(t), admitted(node)); err != nil || got != want {
			t.Errorf("changed pack, %s: %v %v, want %v (the answer is the rule's, never a literal)", node, got, err, want)
		}
	}
}

func TestNeedsWriteAsksTheRuleForTheIntentsNodeAndNothingElse(t *testing.T) {
	rule := &tableRule{answers: map[string]bool{"x.reads.only": false, "x.creates.case": true}}
	wr := newWrites(t, badSwitch{}, rule)
	a := admitted("x.creates.case")
	a.IntentID, a.CorrelationID, a.TargetRef = "x.reads.only", "x.reads.only", "x.reads.only"
	if got, err := wr.NeedsWrite(tctx(t), a); err != nil || !got {
		t.Errorf("got %v %v, want true", got, err)
	}
	if len(rule.asked) != 1 || rule.asked[0] != "x.creates.case" {
		t.Errorf("the rule was asked %v, want exactly the node", rule.asked)
	}
}

func TestNeedsWriteFailsClosedOnNoAnswer(t *testing.T) {
	rule := &tableRule{answers: map[string]bool{"x.reads.only": false, "x.creates.case": true}}
	wr := newWrites(t, badSwitch{}, rule)
	if got, err := wr.NeedsWrite(tctx(t), admitted("x.unknown")); got || !errors.Is(err, trustport.ErrNoWriteAnswer) {
		t.Errorf("an unknown node: %v %v, want false and ErrNoWriteAnswer", got, err)
	}
	boom := errors.New("synthetic rule failure")
	if got, err := newWrites(t, badSwitch{}, badRule{boom}).NeedsWrite(tctx(t), admitted("x.any")); got || !errors.Is(err, boom) {
		t.Errorf("a rule that answers true with an error: %v %v, want false and that error", got, err)
	}
	rule.asked = nil
	if got, err := wr.NeedsWrite(tctx(t), admitted("")); got || !errors.Is(err, trustport.ErrStoredIntent) {
		t.Errorf("an intent with no node: %v %v, want false and ErrStoredIntent", got, err)
	}
	if len(rule.asked) != 0 {
		t.Errorf("the rule was asked %v for an intent with no node", rule.asked)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := wr.NeedsWrite(cancelled, admitted("x.creates.case")); got || !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled context: %v %v, want false and Canceled", got, err)
	}
}

func TestTheWritesAdapterIsNotBuiltWithoutASwitchOrARule(t *testing.T) {
	if _, err := trustport.NewWrites(nil, &tableRule{}); !errors.Is(err, trustport.ErrConfig) {
		t.Errorf("no switch: got %v, want ErrConfig", err)
	}
	if _, err := trustport.NewWrites(badSwitch{}, nil); !errors.Is(err, trustport.ErrConfig) {
		t.Errorf("no rule: got %v, want ErrConfig", err)
	}
	var none *trustport.Writes
	if on, err := none.Enabled(context.Background(), tenantA); on || !errors.Is(err, trustport.ErrConfig) {
		t.Errorf("a nil adapter, Enabled: %v %v, want false and ErrConfig", on, err)
	}
	if got, err := none.NeedsWrite(context.Background(), admitted("x")); got || !errors.Is(err, trustport.ErrConfig) {
		t.Errorf("a nil adapter, NeedsWrite: %v %v, want false and ErrConfig", got, err)
	}
}

// INV-14: the adapter names no industry, pack, intent or question word in any string it holds. The answer comes from the pack, as data, through the rule.
func TestTheWritesAdapterSourceNamesNoIndustryOrIntent(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "writes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := regexp.MustCompile(`(?i)(fin\.|fintech|payment|investigat|escalat|status|complaint|case|transfer|may_write)`)
	literals := 0
	ast.Inspect(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			literals++
			if m := forbidden.FindString(lit.Value); m != "" {
				t.Errorf("writes.go has a string literal that names an industry, an intent or a field: %s", lit.Value)
			}
		}
		return true
	})
	if literals == 0 {
		t.Fatal("the scan found no string literal in writes.go: it scans nothing")
	}
}

// okSwitch is a switch that is on for every tenant.
type okSwitch struct{}

func (okSwitch) Enabled(context.Context, string) (bool, error) { return true, nil }

// slotRule records the intent it is asked about, slot values included (A543).
type slotRule struct{ got []intake.AdmittedIntent }

func (r *slotRule) MayWrite(_ context.Context, a intake.AdmittedIntent) (bool, error) {
	r.got = append(r.got, a)
	return a.Slots["requested_remedy"] != "", nil
}

// A543 (interim, relayed, not yet committed): the adapter gives the rule the intent with its resolved slot values, so the pack's `write_when` is evaluated over what the stored intent says.
func TestNeedsWriteGivesTheRuleTheIntentWithItsResolvedSlotValues(t *testing.T) {
	rule := &slotRule{}
	w := newWrites(t, okSwitch{}, rule)
	a := admitted("fin.payments.exception.investigate")
	a.Slots = map[string]string{"question_type": "STATUS_QUERY", "requested_remedy": "fin.cap.lookup_transaction"}
	may, err := w.NeedsWrite(tctx(t), a)
	if err != nil || !may {
		t.Fatalf("NeedsWrite: %v %v, want true", may, err)
	}
	if len(rule.got) != 1 || !reflect.DeepEqual(rule.got[0].Slots, a.Slots) || rule.got[0].IntentNode != a.IntentNode || rule.got[0].IntentID != a.IntentID {
		t.Errorf("the rule was given %+v, want the intent with its slot values", rule.got)
	}
	a.Slots = nil
	if may, err := w.NeedsWrite(tctx(t), a); err != nil || may {
		t.Errorf("with no slot value the answer is %v %v, want false and no error", may, err)
	}
}
