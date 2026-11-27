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

// ruleSetWith loads the real fintech rule set and sets the convergence window of one matrix pair (SYNTHETIC value; nil removes it, so the pair declares none).
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
	for _, m := range doc["incompatible"].([]any) {
		if e := m.(map[string]any); e["label"] == label {
			if seconds != nil {
				e["convergence_window_seconds"] = *seconds
			} else {
				delete(e, "convergence_window_seconds") // the shipped rule set declares its windows (PCK-05): nil means this pair declares none
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
		// The pair inside its window is recorded in its own field, with its deadline; the set that causes a freeze is empty (nothing is frozen inside a window).
		if len(res.InWindow) != 1 || res.InWindow[0].Label != pairLabel || res.InWindow[0].Deadline != deadline {
			t.Errorf("now=%d: the disagreement and its deadline are recorded in InWindow, got %+v", now, res.InWindow)
		}
		if len(res.Contradictions) != 0 {
			t.Errorf("now=%d: a pair inside its window is not a freezing contradiction, got %+v", now, res.Contradictions)
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

// A659 (interim, relayed): a fact dated in the future has NO WINDOW: the disagreement is a contradiction at once (the freeze and the escalation of the pack's routing), never a wait, and the
// instant is never clamped to now (a deadline computed from a future instant, or from a clamped one, would let a forward-dated fact buy time).
func TestA659AFutureDatedFactHasNoWindow(t *testing.T) {
	rs := ruleSetWith(t, pairLabel, win(syntheticWs))
	const now = int64(5000)
	for name, future := range map[string]int64{"one second ahead": now + 1, "a large amount ahead": now + 10*365*24*3600} {
		for _, which := range []string{"switch", "credit"} {
			ev := pair()
			ev[0].EffectiveAt, ev[1].EffectiveAt = now-5, now-5 // the normal fact would keep a 600 s window open
			if which == "switch" {
				ev[0].EffectiveAt = future
			} else {
				ev[1].EffectiveAt = future
			}
			res, err := reconcile.Reconcile(rs, ev, now)
			if err != nil {
				t.Fatal(err)
			}
			if res.Classification != reconcile.ContradictoryState || res.Wait != nil || res.Waiting() || !res.Frozen() {
				t.Errorf("%s (%s future): want CONTRADICTORY_STATE and no wait, got %s wait=%+v", name, which, res.Classification, res.Wait)
			}
			for _, c := range res.Contradictions {
				if c.Deadline != 0 {
					t.Errorf("%s (%s future): a deadline %d was computed for a future-dated pair (never clamp, never count from a future instant)", name, which, c.Deadline)
				}
			}
			gate := reconcile.NewFreezeGate([]string{"cap.escalate"})
			if p, err := gate.PermitResult(res, "cap.other", "txn_a"); err != reconcile.ErrFrozen || p.Valid() {
				t.Errorf("%s (%s future): a write must be refused as frozen, got %v valid=%v", name, which, err, p.Valid())
			}
		}
	}
}

// The edge of "future": a fact dated exactly now, or one second ago, is not future-dated and opens its window as before.
func TestA659AFactDatedNowOrEarlierStillOpensItsWindow(t *testing.T) {
	rs := ruleSetWith(t, pairLabel, win(syntheticWs))
	const now = int64(5000)
	for name, at := range map[string]int64{"exactly now": now, "one second ago": now - 1} {
		ev := pair()
		ev[0].EffectiveAt, ev[1].EffectiveAt = at, now-5
		if at == now-1 {
			ev[1].EffectiveAt = at
		}
		res, err := reconcile.Reconcile(rs, ev, now)
		if err != nil {
			t.Fatal(err)
		}
		if res.Wait == nil || res.Wait.Until != min64(ev[0].EffectiveAt, ev[1].EffectiveAt)+syntheticWs {
			t.Errorf("%s: want a wait until the earlier fact plus the window, got %+v", name, res.Wait)
		}
	}
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// A waiting answer is always INSUFFICIENT_EVIDENCE (the fallback), whatever the rest of the evidence would say: with the disagreement set aside a pending settlement would give
// SETTLEMENT_PENDING, a classification the Trust intake lets go on; it must not come out of a window.
func TestA659AWaitingAnswerIsAlwaysInsufficientEvidence(t *testing.T) {
	rs := ruleSetWith(t, "switch_successful_credit_not_credited", win(syntheticWs))
	ev := []reconcile.Evidence{
		{ID: "ev_switch", Source: "fin.src.switch", Entity: "switch", Status: reconcile.Known, EffectiveAt: switchAt, ExpiresAt: 99999, Required: true,
			Fields: map[string]predicate.Value{"state": predicate.Enum("SUCCESSFUL")}},
		{ID: "ev_credit", Source: "fin.src.ledger", Entity: "credit", Status: reconcile.Known, EffectiveAt: creditAt, ExpiresAt: 99999, Required: true,
			Fields: map[string]predicate.Value{"state": predicate.Enum("NOT_CREDITED"), "terminal": predicate.Bool(false)}},
		{ID: "ev_settlement", Source: "fin.src.ledger", Entity: "settlement", Status: reconcile.Known, EffectiveAt: creditAt, ExpiresAt: 99999, Required: false,
			Fields: map[string]predicate.Value{"state": predicate.Enum("PENDING"), "expected_by": predicate.Time(deadline + 1000)}},
	}
	for _, now := range []int64{creditAt, deadline - 1} {
		res, err := reconcile.Reconcile(rs, ev, now)
		if err != nil {
			t.Fatal(err)
		}
		if res.Wait == nil || res.Classification != reconcile.InsufficientEvidence {
			t.Errorf("now=%d: want a wait and INSUFFICIENT_EVIDENCE, got %s wait=%+v", now, res.Classification, res.Wait)
		}
	}
}

// Codex's P2 on #949: with one pair expired and another still inside its window, the freezing set holds only the expired pair (its label and its evidence), and the waiting pair is visible in
// its own field. Otherwise a freeze row would carry the waiting pair's label with none of its evidence.
func TestRES05AMixedResultKeepsTheWaitingPairOutOfTheFreezingSet(t *testing.T) {
	const expiredPair, waitingPair = "switch_failed_credit_credited", "reversal_conflict"
	rs := ruleSetWithWindows(t, map[string]int64{expiredPair: 100, waitingPair: 600})
	ev := []reconcile.Evidence{
		{ID: "ev_switch", Source: "fin.src.switch", Entity: "switch", Status: reconcile.Known, EffectiveAt: 1000, ExpiresAt: 99999, Required: true,
			Fields: map[string]predicate.Value{"state": predicate.Enum("FAILED")}},
		{ID: "ev_credit", Source: "fin.src.ledger", Entity: "credit", Status: reconcile.Known, EffectiveAt: 1200, ExpiresAt: 99999, Required: true,
			Fields: map[string]predicate.Value{"state": predicate.Enum("CREDITED")}},
		{ID: "ev_reversal", Source: "fin.src.ledger", Entity: "reversal", Status: reconcile.Known, EffectiveAt: 1050, ExpiresAt: 99999, Required: true,
			Fields: map[string]predicate.Value{"state": predicate.Enum("POSTED"), "original_debit_linked": predicate.Bool(true)}},
		{ID: "ev_debit", Source: "fin.src.ledger", Entity: "debit", Status: reconcile.Known, EffectiveAt: 1100, ExpiresAt: 99999, Required: true,
			Fields: map[string]predicate.Value{"state": predicate.Enum("NOT_POSTED")}},
	}
	const now = int64(1200) // the first pair's window ended at 1100; the second's ends at 1650
	res, err := reconcile.Reconcile(rs, ev, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Classification != reconcile.ContradictoryState || res.Wait != nil || !res.Frozen() {
		t.Fatalf("want CONTRADICTORY_STATE and no wait, got %s wait=%+v", res.Classification, res.Wait)
	}
	if len(res.Contradictions) != 1 || res.Contradictions[0].Label != expiredPair {
		t.Fatalf("the freezing set holds only the expired pair, got %+v", res.Contradictions)
	}
	if got := res.Contributing; !reflect.DeepEqual(got, []string{"ev_credit", "ev_switch"}) {
		t.Errorf("the evidence is the expired pair's alone, got %v", got)
	}
	if len(res.InWindow) != 1 || res.InWindow[0].Label != waitingPair || res.InWindow[0].Deadline != 1050+syntheticWs || res.InWindow[0].A == "" {
		t.Errorf("the waiting pair is visible in its own field with its deadline, got %+v", res.InWindow)
	}
	for _, c := range res.Contradictions {
		if c.Label == waitingPair {
			t.Errorf("the waiting pair's label is in the freezing set: %+v", c)
		}
	}
}

// ruleSetWithWindows is ruleSetWith for several pairs (SYNTHETIC windows).
func ruleSetWithWindows(t *testing.T, windows map[string]int64) *reconcile.RuleSet {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packs", "fintech", "recon_rules", "rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, m := range doc["incompatible"].([]any) {
		e := m.(map[string]any)
		if w, ok := windows[e["label"].(string)]; ok {
			e["convergence_window_seconds"] = w
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

// A708 (REQ-RES-005): a version 2 rule set may declare a window of zero on every pair and field. The loader accepts it and it behaves as no lag allowed (SYNTHETIC test value).
func TestA708AVersion2RuleSetWithEveryWindowZeroLoadsAndAllowsNoLag(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "fixtures", "valid", "recon-rule-set", "fin_version_2_window_zero.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"incompatible", "single_valued"} {
		for _, e := range doc[section].([]any) {
			e.(map[string]any)["convergence_window_seconds"] = int64(0)
		}
	}
	out, _ := json.Marshal(doc)
	rs, err := reconcile.ParseRuleSet(out)
	if err != nil {
		t.Fatalf("a version 2 rule set with every window 0 must load: %v", err)
	}
	res, err := reconcile.Reconcile(rs, pair(), creditAt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Classification != reconcile.ContradictoryState || res.Wait != nil {
		t.Errorf("window 0 allows no lag: want CONTRADICTORY_STATE at once, got %s wait=%+v", res.Classification, res.Wait)
	}
}
