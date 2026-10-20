package corpus

import (
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

const now int64 = 1792500000

func loadRuleSet(t *testing.T) *reconcile.RuleSet {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../../../packs/fintech/recon_rules/rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	rs, err := reconcile.ParseRuleSet(raw)
	if err != nil {
		t.Fatalf("rule set does not load: %v", err)
	}
	return rs
}

// item builds a KNOWN item of an entity with its fields; id is unique per call site.
func item(id, entity string, fields map[string]predicate.Value) reconcile.Evidence {
	return reconcile.Evidence{ID: id, Source: "src." + entity, Entity: entity, Status: reconcile.Known, EffectiveAt: now - 30, ExpiresAt: now + 300, Fields: fields}
}

func state(s string) map[string]predicate.Value {
	return map[string]predicate.Value{"state": predicate.Enum(s)}
}

func debit(id, s string) reconcile.Evidence {
	return item(id, "debit", map[string]predicate.Value{"state": predicate.Enum(s), "counterparty": predicate.Ref("p1"), "amount": predicate.Int(100),
		"posted_at": predicate.Time(now - 60), "intent": predicate.Ref("i1")})
}

func credit(id, s string, terminal bool) reconcile.Evidence {
	return item(id, "credit", map[string]predicate.Value{"state": predicate.Enum(s), "terminal": predicate.Bool(terminal)})
}

func settlement(id, s string, expectedBy int64) reconcile.Evidence {
	return item(id, "settlement", map[string]predicate.Value{"state": predicate.Enum(s), "expected_by": predicate.Time(expectedBy)})
}

func reversal(id, s string, linked bool) reconcile.Evidence {
	return item(id, "reversal", map[string]predicate.Value{"state": predicate.Enum(s), "original_debit_linked": predicate.Bool(linked)})
}

func run(t *testing.T, rs *reconcile.RuleSet, ev ...reconcile.Evidence) reconcile.Result {
	t.Helper()
	res, err := reconcile.Reconcile(rs, ev, now)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return res
}

// PERD §22.3, one case per rule, with the rule that must win.
func TestRulesOneByOne(t *testing.T) {
	rs := loadRuleSet(t)
	cases := []struct {
		name string
		ev   []reconcile.Evidence
		want reconcile.Classification
		rule string
	}{
		{"R1 debit posted and credit confirmed", []reconcile.Evidence{debit("d", "POSTED"), credit("c", "CREDITED", true)}, reconcile.SuccessConfirmed, "R1@1"},
		{"R2 switch timed out and credit unknown", []reconcile.Evidence{debit("d", "POSTED"), item("s", "switch", state("TIMED_OUT"))}, reconcile.ExecutionOutcomeUnknown, "R2@1"},
		{"R2 switch unknown and credit pending", []reconcile.Evidence{debit("d", "POSTED"), item("s", "switch", state("EXECUTION_OUTCOME_UNKNOWN")), credit("c", "PENDING", false)}, reconcile.ExecutionOutcomeUnknown, "R2@1"},
		{"R2 not credited without a terminal flag", []reconcile.Evidence{debit("d", "POSTED"), item("s", "switch", state("TIMED_OUT")), credit("c", "NOT_CREDITED", false)}, reconcile.ExecutionOutcomeUnknown, "R2@1"},
		{"R3 reversal posted and linked", []reconcile.Evidence{debit("d", "POSTED"), reversal("r", "POSTED", true)}, reconcile.ReversedConfirmed, "R3@1"},
		{"R4 switch failed and credit credited", []reconcile.Evidence{debit("d", "POSTED"), item("s", "switch", state("FAILED")), credit("c", "CREDITED", true)}, reconcile.ContradictoryState, "R4@1"},
		{"R5 switch successful and settlement pending in window", []reconcile.Evidence{debit("d", "POSTED"), item("s", "switch", state("SUCCESSFUL")), settlement("t", "PENDING", now+100)}, reconcile.SettlementPending, "R5@1"},
		{"R6 two debits, one counterparty and amount, inside ten minutes", []reconcile.Evidence{debit("d1", "POSTED"), debit("d2", "POSTED")}, reconcile.PossibleDuplicate, "R6@1"},
		{"R7 a required source is unavailable", []reconcile.Evidence{{ID: "u", Source: "src.credit", Entity: "credit", Status: reconcile.Unavailable, Required: true, ExpiresAt: now + 300}}, reconcile.InsufficientEvidence, "R7@1"},
		{"no rule matches: never a default success", []reconcile.Evidence{debit("d", "POSTED")}, reconcile.InsufficientEvidence, "R0@1"},
		{"empty set", nil, reconcile.InsufficientEvidence, "R0@1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := run(t, rs, c.ev...)
			if got.Classification != c.want || got.RuleID != c.rule {
				t.Fatalf("want %s by %s, got %s by %s", c.want, c.rule, got.Classification, got.RuleID)
			}
		})
	}
}

func TestResultDetails(t *testing.T) {
	rs := loadRuleSet(t)
	d, c := debit("d", "POSTED"), credit("c", "CREDITED", true)
	d.ExpiresAt, c.ExpiresAt = now+500, now+90
	got := run(t, rs, d, c)
	if !reflect.DeepEqual(got.Contributing, []string{"c", "d"}) || got.ValidUntil != now+90 {
		t.Fatalf("contributing %v valid_until %d", got.Contributing, got.ValidUntil)
	}
	empty := run(t, rs)
	if empty.ValidUntil != now || len(empty.Contributing) != 0 {
		t.Fatalf("an answer with no contributing evidence must expire now: %+v", empty)
	}
	bad := run(t, rs, debit("d", "POSTED"), item("s", "switch", state("FAILED")), credit("c", "CREDITED", true))
	if len(bad.Contradictions) != 1 || bad.Contradictions[0].Label != "switch_failed_credit_credited" {
		t.Fatalf("contradictions %+v", bad.Contradictions)
	}
}

// A stale, ambiguous or unauthorized item never satisfies a rule: only KNOWN does (PERD §21.1, INV-19).
func TestOnlyKnownEvidenceCounts(t *testing.T) {
	rs := loadRuleSet(t)
	for _, status := range []reconcile.FactStatus{reconcile.Inferred, reconcile.Ambiguous, reconcile.Unavailable, reconcile.Stale, reconcile.Contradictory, reconcile.Unknown, reconcile.Unauthorized} {
		c := credit("c", "CREDITED", true)
		c.Status = status
		got := run(t, rs, debit("d", "POSTED"), c)
		if got.Classification == reconcile.SuccessConfirmed {
			t.Fatalf("a %s credit confirmed a success", status)
		}
	}
}

func TestRejectsMalformedEvidence(t *testing.T) {
	rs := loadRuleSet(t)
	bad := [][]reconcile.Evidence{
		{item("x", "no_such_entity", nil)},
		{debit("d", "POSTED"), debit("d", "POSTED")},
		{item("y", "debit", map[string]predicate.Value{"colour": predicate.Ref("red")})},
	}
	for i, ev := range bad {
		if _, err := reconcile.Reconcile(rs, ev, now); err == nil {
			t.Fatalf("case %d: accepted malformed evidence", i)
		}
	}
}

func TestEveryMatrixEntryIsAsserted(t *testing.T) {
	rs := loadRuleSet(t)
	build := func(i int, f reconcile.Fact) reconcile.Evidence {
		fields := map[string]predicate.Value{f.Field: predicate.Enum(f.Value)}
		switch f.Entity {
		case "debit":
			return debit("m"+strconv.Itoa(i), f.Value)
		case "credit":
			return credit("m"+strconv.Itoa(i), f.Value, true)
		case "settlement":
			return settlement("m"+strconv.Itoa(i), f.Value, now+100)
		case "reversal":
			return reversal("m"+strconv.Itoa(i), f.Value, true)
		}
		return item("m"+strconv.Itoa(i), f.Entity, fields)
	}
	for i, m := range rs.Matrix() {
		a, b := build(10*i, m.A), build(10*i+1, m.B)
		got := run(t, rs, a, b)
		found := false
		for _, c := range got.Contradictions {
			found = found || c.Label == m.Label
		}
		if !found {
			t.Fatalf("%s: the pair was not flagged: %+v", m.Label, got)
		}
		if m.Unless != nil {
			cancelled := run(t, rs, a, b, build(10*i+2, *m.Unless))
			for _, c := range cancelled.Contradictions {
				if c.Label == m.Label {
					t.Fatalf("%s: the unless clause did not cancel the pair", m.Label)
				}
			}
		}
	}
}

// The expected labels and classification restate PERD §22.3 by hand, independent of the rule data.
type combo struct{ debit, credit, sw, settle, rev int }

var (
	debitV  = []string{"", "POSTED", "NOT_POSTED"}
	creditV = []string{"", "CREDITED", "NOT_CREDITED_T", "NOT_CREDITED_N", "PENDING"}
	switchV = []string{"", "SUCCESSFUL", "FAILED", "TIMED_OUT", "EXECUTION_OUTCOME_UNKNOWN"}
	settleV = []string{"", "PENDING_IN", "PENDING_OUT", "SETTLED", "FAILED"}
	revV    = []string{"", "POSTED_LINKED", "POSTED_UNLINKED", "PENDING"}
)

func (c combo) evidence() []reconcile.Evidence {
	var ev []reconcile.Evidence
	if v := debitV[c.debit]; v != "" {
		ev = append(ev, debit("d", v))
	}
	switch v := creditV[c.credit]; v {
	case "":
	case "NOT_CREDITED_T":
		ev = append(ev, credit("c", "NOT_CREDITED", true))
	case "NOT_CREDITED_N":
		ev = append(ev, credit("c", "NOT_CREDITED", false))
	default:
		ev = append(ev, credit("c", v, true))
	}
	if v := switchV[c.sw]; v != "" {
		ev = append(ev, item("s", "switch", state(v)))
	}
	switch v := settleV[c.settle]; v {
	case "":
	case "PENDING_IN":
		ev = append(ev, settlement("t", "PENDING", now+100))
	case "PENDING_OUT":
		ev = append(ev, settlement("t", "PENDING", now-100))
	default:
		ev = append(ev, settlement("t", v, now+100))
	}
	switch v := revV[c.rev]; v {
	case "":
	case "POSTED_LINKED":
		ev = append(ev, reversal("r", "POSTED", true))
	case "POSTED_UNLINKED":
		ev = append(ev, reversal("r", "POSTED", false))
	default:
		ev = append(ev, reversal("r", v, true))
	}
	return ev
}

func (c combo) expected() (labels []string, class reconcile.Classification) {
	d, cr, sw, st, rv := debitV[c.debit], creditV[c.credit], switchV[c.sw], settleV[c.settle], revV[c.rev]
	revPosted := rv == "POSTED_LINKED" || rv == "POSTED_UNLINKED"
	if sw == "FAILED" && cr == "CREDITED" {
		labels = append(labels, "switch_failed_credit_credited")
	}
	if d == "NOT_POSTED" && cr == "CREDITED" && !revPosted {
		labels = append(labels, "credit_without_debit")
	}
	if revPosted && d == "NOT_POSTED" {
		labels = append(labels, "reversal_conflict")
	}
	if sw == "SUCCESSFUL" && (cr == "NOT_CREDITED_T" || cr == "NOT_CREDITED_N") {
		labels = append(labels, "switch_successful_credit_not_credited")
	}
	sort.Strings(labels)
	creditResolved := cr == "CREDITED" || cr == "NOT_CREDITED_T"
	switch {
	case rv == "POSTED_LINKED" && !contains(labels, "reversal_conflict"):
		class = reconcile.ReversedConfirmed
	case len(labels) > 0:
		class = reconcile.ContradictoryState
	case d == "POSTED" && (sw == "TIMED_OUT" || sw == "EXECUTION_OUTCOME_UNKNOWN") && !creditResolved:
		class = reconcile.ExecutionOutcomeUnknown
	case sw == "SUCCESSFUL" && st == "PENDING_IN":
		class = reconcile.SettlementPending
	case d == "POSTED" && cr == "CREDITED" && st != "PENDING_IN" && st != "PENDING_OUT" && rv != "PENDING":
		class = reconcile.SuccessConfirmed
	default:
		class = reconcile.InsufficientEvidence
	}
	return labels, class
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Every combination of the five fact kinds (1500) against a hand-written expectation of PERD §22.3.
func TestExhaustiveFactCombinations(t *testing.T) {
	rs := loadRuleSet(t)
	n := 0
	for d := range debitV {
		for c := range creditV {
			for s := range switchV {
				for st := range settleV {
					for r := range revV {
						cb := combo{d, c, s, st, r}
						wantLabels, wantClass := cb.expected()
						got := run(t, rs, cb.evidence()...)
						var gotLabels []string
						for _, x := range got.Contradictions {
							gotLabels = append(gotLabels, x.Label)
						}
						sort.Strings(gotLabels)
						if got.Classification != wantClass || !reflect.DeepEqual(gotLabels, wantLabels) && !(len(gotLabels) == 0 && len(wantLabels) == 0) {
							t.Fatalf("%v: want %s %v, got %s %v (%s)", cb, wantClass, wantLabels, got.Classification, gotLabels, got.RuleID)
						}
						n++
					}
				}
			}
		}
	}
	if n != 3*5*5*5*4 {
		t.Fatalf("covered %d combinations", n)
	}
}

func randomEvidence(r *rand.Rand) []reconcile.Evidence {
	var ev []reconcile.Evidence
	for i := r.Intn(9); i > 0; i-- {
		id := "e" + strconv.Itoa(len(ev))
		var e reconcile.Evidence
		switch r.Intn(5) {
		case 0:
			e = debit(id, debitV[1+r.Intn(2)])
			e.Fields["amount"] = predicate.Int(int64(100 + 100*r.Intn(2)))
			e.Fields["posted_at"] = predicate.Time(now - int64(r.Intn(1500)))
		case 1:
			e = credit(id, []string{"CREDITED", "NOT_CREDITED", "PENDING"}[r.Intn(3)], r.Intn(2) == 0)
		case 2:
			e = item(id, "switch", state(switchV[1+r.Intn(4)]))
		case 3:
			e = settlement(id, []string{"PENDING", "SETTLED", "FAILED"}[r.Intn(3)], now+int64(r.Intn(400))-200)
		default:
			e = reversal(id, []string{"POSTED", "PENDING"}[r.Intn(2)], r.Intn(2) == 0)
		}
		if r.Intn(4) == 0 {
			e.Status = []reconcile.FactStatus{reconcile.Unavailable, reconcile.Stale, reconcile.Unknown, reconcile.Ambiguous}[r.Intn(4)]
		}
		e.Required = r.Intn(3) == 0
		e.ExpiresAt = now + int64(1+r.Intn(500))
		ev = append(ev, e)
	}
	return ev
}

func TestMetamorphicProperties(t *testing.T) {
	rs := loadRuleSet(t)
	r := rand.New(rand.NewSource(20261020))
	contradictions, collapses := 0, 0
	for i := 0; i < 4000; i++ {
		ev := randomEvidence(r)
		base := run(t, rs, ev...)

		// permutation invariance
		shuffled := append([]reconcile.Evidence(nil), ev...)
		r.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		if got := run(t, rs, shuffled...); !reflect.DeepEqual(got, base) {
			t.Fatalf("set %d: order changed the result:\n%+v\n%+v", i, base, got)
		}

		// adding evidence never turns a contradiction into a clean success
		if base.Classification == reconcile.ContradictoryState {
			contradictions++
			extra := randomEvidence(r)
			for k := range extra {
				extra[k].ID = "x" + extra[k].ID
			}
			got := run(t, rs, append(append([]reconcile.Evidence(nil), ev...), extra...)...)
			if got.Classification != reconcile.ContradictoryState && got.Classification != reconcile.ReversedConfirmed {
				t.Fatalf("set %d: adding evidence moved CONTRADICTORY_STATE to %s", i, got.Classification)
			}
		}

		// losing an authoritative source (it becomes unavailable and required) never raises certainty
		for k, e := range ev {
			if e.Status != reconcile.Known {
				continue
			}
			lost := append([]reconcile.Evidence(nil), ev...)
			lost[k].Status, lost[k].Required = reconcile.Unavailable, true
			if got := run(t, rs, lost...); rs.Certainty(got.Classification) > rs.Certainty(base.Classification) {
				t.Fatalf("set %d: losing %s raised certainty from %s to %s", i, e.ID, base.Classification, got.Classification)
			}
		}

		// no pending state is collapsed into a terminal one
		if base.Classification.IsTerminal() {
			collapses++
			known := map[string]reconcile.Evidence{}
			for _, e := range ev {
				known[e.ID] = e
			}
			for _, id := range base.Contributing {
				e := known[id]
				for _, m := range rs.PendingMarkers() {
					if e.Entity == m.Entity && e.Status == reconcile.Known && e.Fields[m.Field].S == m.Value {
						t.Fatalf("set %d: %s is terminal but contributing %s is pending (%v)", i, base.Classification, id, m)
					}
				}
			}
		}
	}
	if contradictions < 100 || collapses < 100 {
		t.Fatalf("weak coverage: %d contradictions, %d terminal results", contradictions, collapses)
	}
}

// Five of the twelve classifications have no rule in rule set v1 (open decisions OD-P-52 to OD-P-56). Until the founders add
// them, each situation falls to INSUFFICIENT_EVIDENCE (REQ-RES-001: never a default success). These tests assert that fallback,
// so adding a rule for one of them later is a deliberate change that must update the test and the open decision.
func TestUncoveredClassificationsFallBackToInsufficientEvidence(t *testing.T) {
	rs := loadRuleSet(t)
	uncovered := []reconcile.Classification{reconcile.RejectedConfirmed, reconcile.DebitPostedCreditUnconfirmed, reconcile.ReversalPending,
		reconcile.Partial, reconcile.EventOrderUncertain}

	produced := map[reconcile.Classification]bool{}
	for _, c := range rs.Produces() {
		produced[c] = true
	}
	for _, c := range uncovered {
		if produced[c] {
			t.Errorf("a rule now produces %s: update this test and OD-P-52..56", c)
		}
	}

	unknownCredit := credit("c", "CREDITED", true)
	unknownCredit.Status = reconcile.Unknown
	staleDebit, staleCredit := debit("d", "POSTED"), credit("c", "CREDITED", true)
	staleDebit.Status, staleCredit.Status = reconcile.Stale, reconcile.Stale
	situations := map[reconcile.Classification][]reconcile.Evidence{
		// not debited, switch failed: the facts PERD §22.3 gives for REJECTED_CONFIRMED
		reconcile.RejectedConfirmed: {debit("d", "NOT_POSTED"), item("s", "switch", state("FAILED"))},
		// debit known, credit unknown, no terminal switch result
		reconcile.DebitPostedCreditUnconfirmed: {debit("d", "POSTED"), unknownCredit},
		// reversal initiated, not posted
		reconcile.ReversalPending: {debit("d", "POSTED"), reversal("r", "PENDING", true)},
		// no entity models a multi-leg transaction, so only a lone leg can be expressed
		reconcile.Partial: {debit("d1", "POSTED")},
		// no entity models ordering, so the closest expressible case is facts too old to use
		reconcile.EventOrderUncertain: {staleDebit, staleCredit},
	}
	for _, c := range uncovered {
		got := run(t, rs, situations[c]...)
		if got.Classification != reconcile.InsufficientEvidence || got.RuleID != "R0@1" {
			t.Errorf("%s: want the INSUFFICIENT_EVIDENCE fallback, got %s by %s", c, got.Classification, got.RuleID)
		}
	}

	// and over random evidence the engine never returns one of the five
	r := rand.New(rand.NewSource(99))
	for i := 0; i < 4000; i++ {
		got := run(t, rs, randomEvidence(r)...)
		for _, c := range uncovered {
			if got.Classification == c {
				t.Fatalf("set %d returned %s, which no rule produces", i, c)
			}
		}
	}
}
