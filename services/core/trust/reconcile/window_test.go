package reconcile_test

// RES-05 (REQ-RES-005; INV-20, INV-21; A642, interim, relayed: inside the window no write and no verdict, at expiry ESCALATE). A fact pair declares a convergence window in the rule set (pack
// data). Inside it a disagreement is expected lag: the result waits, no grant can be built and no verdict is given. From the window's end a disagreement is CONTRADICTORY_STATE (the freeze and
// the escalation of the pack's routing). A pair with no window is CONTRADICTORY at once (fail closed). The windows below are SYNTHETIC test values; the pack declares none (an OPEN entry). All
// times are Unix seconds on the injected clock.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

const (
	pairLabel   = "switch_failed_credit_credited"
	syntheticWs = 600
	switchAt    = 1000
	creditAt    = 1200
	deadline    = switchAt + syntheticWs // the earlier fact starts the window
)

// ruleSetWith loads the real fintech rule set and sets the convergence window of one matrix pair (SYNTHETIC value; nil leaves it absent).
func ruleSetWith(t *testing.T, label string, seconds *int64) *reconcile.RuleSet {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packs", "fintech", "recon_rules", "rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if seconds != nil {
		for _, m := range doc["incompatible"].([]any) {
			if e := m.(map[string]any); e["label"] == label {
				e["convergence_window_seconds"] = *seconds
			}
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := reconcile.ParseRuleSet(out)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func win(s int64) *int64 { return &s }

func pair() []reconcile.Evidence {
	return []reconcile.Evidence{
		{ID: "ev_switch", Source: "fin.src.switch", Entity: "switch", Status: reconcile.Known, EffectiveAt: switchAt, ExpiresAt: 9000, Required: true,
			Fields: map[string]predicate.Value{"state": predicate.Enum("FAILED")}},
		{ID: "ev_credit", Source: "fin.src.ledger", Entity: "credit", Status: reconcile.Known, EffectiveAt: creditAt, ExpiresAt: 9000, Required: true,
			Fields: map[string]predicate.Value{"state": predicate.Enum("CREDITED")}},
	}
}

// Inside the window the answer waits: the boundary is the deadline itself (RES-05 done-when: boundary tests at the window edge on the simulated clock).
func TestRES05InsideTheWindowTheResultWaitsAndBeforeThatNothingIsFrozen(t *testing.T) {
	rs := ruleSetWith(t, pairLabel, win(syntheticWs))
	for _, now := range []int64{creditAt, deadline - 1} {
		res, err := reconcile.Reconcile(rs, pair(), now)
		if err != nil {
			t.Fatal(err)
		}
		if res.Wait == nil || res.Wait.Until != deadline {
			t.Fatalf("now=%d: want a wait until %d, got %+v", now, deadline, res.Wait)
		}
		if res.Classification == reconcile.ContradictoryState || res.Classification.IsTerminal() {
			t.Errorf("now=%d: inside the window the classification must be neither the freeze nor a verdict, got %s", now, res.Classification)
		}
		if res.Frozen() || !res.Waiting() {
			t.Errorf("now=%d: Frozen=%v Waiting=%v, want false/true", now, res.Frozen(), res.Waiting())
		}
		if res.ValidUntil > deadline {
			t.Errorf("now=%d: the answer may not hold past the window: %d > %d", now, res.ValidUntil, deadline)
		}
		if len(res.Contradictions) != 1 || res.Contradictions[0].Label != pairLabel || res.Contradictions[0].Deadline != deadline {
			t.Errorf("now=%d: the disagreement and its deadline are recorded, got %+v", now, res.Contradictions)
		}
	}
}

// At the deadline and after it the disagreement is the contradiction: the freeze, whose routing is ESCALATE (A642).
func TestRES05AtAndAfterTheDeadlineTheStateIsContradictory(t *testing.T) {
	rs := ruleSetWith(t, pairLabel, win(syntheticWs))
	for _, now := range []int64{deadline, deadline + 1, deadline + 100000} {
		res, err := reconcile.Reconcile(rs, pair(), now)
		if err != nil {
			t.Fatal(err)
		}
		if res.Classification != reconcile.ContradictoryState || res.Wait != nil || !res.Frozen() || res.Waiting() {
			t.Errorf("now=%d: want CONTRADICTORY_STATE, frozen, not waiting; got %s wait=%+v", now, res.Classification, res.Wait)
		}
	}
}

// No declared window (or zero) means no lag is allowed: fail closed, as before the window existed.
func TestRES05APairWithNoWindowIsContradictoryAtOnce(t *testing.T) {
	for name, rs := range map[string]*reconcile.RuleSet{"absent": ruleSetWith(t, pairLabel, nil), "zero": ruleSetWith(t, pairLabel, win(0))} {
		res, err := reconcile.Reconcile(rs, pair(), creditAt)
		if err != nil {
			t.Fatal(err)
		}
		if res.Classification != reconcile.ContradictoryState || res.Wait != nil {
			t.Errorf("%s: want CONTRADICTORY_STATE at once, got %s wait=%+v", name, res.Classification, res.Wait)
		}
	}
}

// A negative window is not a rule set.
func TestRES05ANegativeWindowDoesNotLoad(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packs", "fintech", "recon_rules", "rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	doc["incompatible"].([]any)[0].(map[string]any)["convergence_window_seconds"] = -1
	out, _ := json.Marshal(doc)
	if _, err := reconcile.ParseRuleSet(out); err == nil {
		t.Fatal("a negative convergence window loaded")
	}
}

// One expired disagreement is enough: while another pair is still inside its window, the expired one makes the state contradictory (the freeze is not postponed by a younger pair).
func TestRES05AnExpiredPairWinsOverAPairInsideItsWindow(t *testing.T) {
	rs := ruleSetWith(t, pairLabel, win(syntheticWs))
	ev := append(pair(),
		reconcile.Evidence{ID: "ev_debit", Source: "fin.src.ledger", Entity: "debit", Status: reconcile.Known, EffectiveAt: switchAt, ExpiresAt: 9000, Required: true,
			Fields: map[string]predicate.Value{"state": predicate.Enum("NOT_POSTED")}})
	// credit CREDITED with no debit and no reversal is "credit_without_debit"; it has no window, so it is contradictory at once, whatever the first pair does.
	res, err := reconcile.Reconcile(rs, ev, deadline-1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Classification != reconcile.ContradictoryState || res.Wait != nil {
		t.Fatalf("want CONTRADICTORY_STATE, got %s wait=%+v", res.Classification, res.Wait)
	}
}

// Same evidence in any order gives the same windowed answer (INV-21).
func TestRES05TheWindowedAnswerDoesNotDependOnArrivalOrder(t *testing.T) {
	rs := ruleSetWith(t, pairLabel, win(syntheticWs))
	a, err := reconcile.Reconcile(rs, pair(), deadline-1)
	if err != nil {
		t.Fatal(err)
	}
	rev := pair()
	rev[0], rev[1] = rev[1], rev[0]
	b, err := reconcile.Reconcile(rs, rev, deadline-1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("order changed the answer:\n%+v\n%+v", a, b)
	}
}

// Inside the window no write is allowed at all, not even the capabilities a freeze allows; from the deadline the freeze's own allow-list applies (REQ-RES-002).
func TestRES05NoGrantInsideTheWindowAndTheFreezeListAfterIt(t *testing.T) {
	rs := ruleSetWith(t, pairLabel, win(syntheticWs))
	gate := reconcile.NewFreezeGate([]string{"cap.escalate"})
	inside, _ := reconcile.Reconcile(rs, pair(), deadline-1)
	for _, c := range []string{"cap.escalate", "cap.other"} {
		if p, err := gate.PermitResult(inside, c, "txn_a"); err == nil || p.Valid() || err != reconcile.ErrWaiting {
			t.Errorf("inside the window %s: want ErrWaiting and no permission, got %v valid=%v", c, err, p.Valid())
		}
	}
	after, _ := reconcile.Reconcile(rs, pair(), deadline)
	if p, err := gate.PermitResult(after, "cap.escalate", "txn_a"); err != nil || !p.Valid() || p.Target() != "txn_a" {
		t.Errorf("after the window the allowed capability is permitted: %v", err)
	}
	if _, err := gate.PermitResult(after, "cap.other", "txn_a"); err != reconcile.ErrFrozen {
		t.Errorf("after the window another capability is frozen: %v", err)
	}
	if _, err := gate.PermitResult(after, "cap.escalate", ""); err != reconcile.ErrNoTarget {
		t.Errorf("a permission still names its target: %v", err)
	}
}

// A rule that would end the question while evidence disagrees must not speak inside the window: with the disagreement set aside the reversal rule would give REVERSED_CONFIRMED, a verdict (A642: no
// verdict inside the window). It falls back to INSUFFICIENT_EVIDENCE, and from the deadline the real contradiction stands.
func TestRES05NoVerdictInsideTheWindowEvenIfARuleWouldGiveOne(t *testing.T) {
	rs := ruleSetWith(t, "reversal_conflict", win(syntheticWs))
	ev := []reconcile.Evidence{
		{ID: "ev_reversal", Source: "fin.src.ledger", Entity: "reversal", Status: reconcile.Known, EffectiveAt: switchAt, ExpiresAt: 9000, Required: true,
			Fields: map[string]predicate.Value{"state": predicate.Enum("POSTED"), "original_debit_linked": predicate.Bool(true)}},
		{ID: "ev_debit", Source: "fin.src.ledger", Entity: "debit", Status: reconcile.Known, EffectiveAt: creditAt, ExpiresAt: 9000, Required: true,
			Fields: map[string]predicate.Value{"state": predicate.Enum("NOT_POSTED")}},
	}
	inside, err := reconcile.Reconcile(rs, ev, deadline-1)
	if err != nil {
		t.Fatal(err)
	}
	if inside.Classification != reconcile.InsufficientEvidence || inside.Wait == nil || inside.Classification.IsTerminal() {
		t.Fatalf("inside the window want INSUFFICIENT_EVIDENCE and a wait, got %s wait=%+v", inside.Classification, inside.Wait)
	}
	after, err := reconcile.Reconcile(rs, ev, deadline)
	if err != nil {
		t.Fatal(err)
	}
	if after.Classification != reconcile.ContradictoryState || after.Wait != nil {
		t.Fatalf("at the deadline want CONTRADICTORY_STATE, got %s wait=%+v", after.Classification, after.Wait)
	}
}
