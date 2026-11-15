package recovery_test

// RCV-02: the recovery budget. REQ-RCV-003 (two dispatches per logical key, then escalate; no third dispatch ever), REQ-RCV-002,
// INV-07, AGENT-08, AGENT-16 (the number is an input, not a default). The key is tenant_id, correlation_id, capability, target_ref
// (Architect ruling A3), counted over DISPATCH_ATTEMPTED events. Everything is SYNTHETIC.

import (
	"fmt"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/execution/recovery"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const tenant = "ten_aa"

func key(corr, cap, target string) recovery.Key {
	return recovery.Key{TenantID: tenant, CorrelationID: corr, Capability: cap, TargetRef: target}
}

func dispatchRow(seq int64, corr, cap, target string) chain.Row {
	p := fmt.Sprintf(`{"attempt":1,"capability":%q,"target_ref":%q}`, cap, target)
	return chain.Row{Seq: seq, CorrelationID: corr, EventType: "DISPATCH_ATTEMPTED", Payload: &p}
}

func rawDispatch(seq int64, corr, payload string) chain.Row {
	return chain.Row{Seq: seq, CorrelationID: corr, EventType: "DISPATCH_ATTEMPTED", Payload: &payload}
}

func gate(t interface {
	Helper()
	Fatal(args ...any)
}, max int) *recovery.Gate {
	t.Helper()
	g, err := recovery.NewGate(max)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestTheBudgetIsAnInputAndHasNoDefault(t *testing.T) {
	for _, n := range []int{0, -1} {
		if _, err := recovery.NewGate(n); err == nil {
			t.Errorf("NewGate(%d) accepted", n)
		}
	}
}

func TestThirdDispatchOfOneKeyIsRefusedWithABudgetOfTwo(t *testing.T) {
	g := gate(t, 2)
	k := key("cor_1", "fin.cap.a", "res_1")
	var rows []chain.Row
	for i := 0; i < 2; i++ {
		if d := g.Decide(tenant, rows, k); d != recovery.DispatchAllowed {
			t.Fatalf("dispatch %d: %s", i+1, d)
		}
		rows = append(rows, dispatchRow(int64(i+1), "cor_1", "fin.cap.a", "res_1"))
	}
	if d := g.Decide(tenant, rows, k); d != recovery.BudgetSpent {
		t.Fatalf("third dispatch: %s, want %s", d, recovery.BudgetSpent)
	}
}

func TestOtherKeysDoNotShareABudget(t *testing.T) {
	g := gate(t, 2)
	rows := []chain.Row{dispatchRow(1, "cor_1", "fin.cap.a", "res_1"), dispatchRow(2, "cor_1", "fin.cap.a", "res_1")}
	for name, k := range map[string]recovery.Key{
		"other correlation": key("cor_2", "fin.cap.a", "res_1"),
		"other capability":  key("cor_1", "fin.cap.b", "res_1"),
		"other target":      key("cor_1", "fin.cap.a", "res_2"),
	} {
		if d := g.Decide(tenant, rows, k); d != recovery.DispatchAllowed {
			t.Errorf("%s: %s", name, d)
		}
	}
}

// A dispatch whose capability or target cannot be read might belong to this key; unknown is never "not counted".
func TestADispatchThatCannotBeAttributedCountsAgainstEveryKeyOfItsCorrelation(t *testing.T) {
	g := gate(t, 2)
	k := key("cor_1", "fin.cap.a", "res_1")
	for name, payload := range map[string]string{
		"no payload fields":   `{}`,
		"capability missing":  `{"target_ref":"res_1"}`,
		"target missing":      `{"capability":"fin.cap.a"}`,
		"capability a number": `{"capability":7,"target_ref":"res_1"}`,
		"not json":            `{`,
	} {
		rows := []chain.Row{dispatchRow(1, "cor_1", "fin.cap.a", "res_1"), rawDispatch(2, "cor_1", payload)}
		if d := g.Decide(tenant, rows, k); d != recovery.BudgetSpent {
			t.Errorf("%s: %s, want %s", name, d, recovery.BudgetSpent)
		}
		other := []chain.Row{rawDispatch(1, "cor_9", payload), rawDispatch(2, "cor_9", payload)}
		if d := g.Decide(tenant, other, k); d != recovery.DispatchAllowed {
			t.Errorf("%s: a dispatch of another correlation was counted: %s", name, d)
		}
	}
}

func TestOnlyDispatchesAreCounted(t *testing.T) {
	g := gate(t, 1)
	rows := []chain.Row{}
	for i, et := range []string{"GRANT_SPENT", "RECEIPT", "VERDICT", "DECISION_MADE", "DISPATCH_FAILED"} {
		p := `{"capability":"fin.cap.a","target_ref":"res_1"}`
		rows = append(rows, chain.Row{Seq: int64(i + 1), CorrelationID: "cor_1", EventType: et, Payload: &p})
	}
	if d := g.Decide(tenant, rows, key("cor_1", "fin.cap.a", "res_1")); d != recovery.DispatchAllowed {
		t.Fatalf("%s", d)
	}
}

func TestCheckBudgetFlagsEveryDispatchBeyondTheBudget(t *testing.T) {
	rows := []chain.Row{
		dispatchRow(1, "cor_1", "fin.cap.a", "res_1"),
		dispatchRow(2, "cor_1", "fin.cap.a", "res_1"),
		dispatchRow(3, "cor_1", "fin.cap.a", "res_1"),
		dispatchRow(4, "cor_1", "fin.cap.a", "res_1"),
		dispatchRow(5, "cor_2", "fin.cap.a", "res_1"),
		rawDispatch(6, "cor_3", `{}`),
	}
	got := recovery.CheckBudget(tenant, rows, 2)
	want := []recovery.BudgetViolation{
		{Key: key("cor_1", "fin.cap.a", "res_1"), Seq: 3, Count: 3, Reason: recovery.BudgetExceeded},
		{Key: key("cor_1", "fin.cap.a", "res_1"), Seq: 4, Count: 4, Reason: recovery.BudgetExceeded},
		{Key: recovery.Key{TenantID: tenant, CorrelationID: "cor_3"}, Seq: 6, Reason: recovery.KeyUnreadable},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("violations\n got %v\nwant %v", got, want)
	}
}

// Property: an agent that dispatches whenever the gate allows never dispatches a third time for one key, however the keys interleave.
func TestPropertyNoThirdDispatchEver(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		max := rapid.IntRange(1, 4).Draw(t, "budget")
		g := gate(t, max)
		corrs := []string{"cor_1", "cor_2"}
		caps := []string{"fin.cap.a", "fin.cap.b"}
		targets := []string{"res_1", "res_2"}
		var rows []chain.Row
		per := map[recovery.Key]int{}
		steps := rapid.IntRange(0, 60).Draw(t, "steps")
		for i := 0; i < steps; i++ {
			k := key(rapid.SampledFrom(corrs).Draw(t, "corr"), rapid.SampledFrom(caps).Draw(t, "cap"), rapid.SampledFrom(targets).Draw(t, "target"))
			if g.Decide(tenant, rows, k) != recovery.DispatchAllowed {
				continue
			}
			rows = append(rows, dispatchRow(int64(len(rows)+1), k.CorrelationID, k.Capability, k.TargetRef))
			per[k]++
			if per[k] > max {
				t.Fatalf("%v dispatched %d times with a budget of %d", k, per[k], max)
			}
		}
		if v := recovery.CheckBudget(tenant, rows, max); len(v) != 0 {
			t.Fatalf("the checker flags a sequence the gate allowed: %v", v)
		}
	})
}

// A key of another tenant than the chain being read is refused, never counted as fresh (INV-12 tenant boundary).
func TestAKeyOfAnotherTenantIsRefused(t *testing.T) {
	g := gate(t, 2)
	k := recovery.Key{TenantID: "ten_bb", CorrelationID: "cor_1", Capability: "fin.cap.a", TargetRef: "res_1"}
	if d := g.Decide(tenant, nil, k); d != recovery.BudgetSpent {
		t.Fatalf("%s", d)
	}
}

// A key with an empty part cannot match any recorded dispatch, so it would look unspent; it is refused (Codex review of noema#196).
func TestAnIncompleteKeyIsRefused(t *testing.T) {
	g := gate(t, 2)
	for name, k := range map[string]recovery.Key{
		"no correlation": key("", "fin.cap.a", "res_1"),
		"no capability":  key("cor_1", "", "res_1"),
		"no target":      key("cor_1", "fin.cap.a", ""),
		"nothing":        {TenantID: tenant},
	} {
		if d := g.Decide(tenant, nil, k); d != recovery.BudgetSpent {
			t.Errorf("%s: %s, want %s", name, d, recovery.BudgetSpent)
		}
	}
}
