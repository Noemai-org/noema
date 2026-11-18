package reconcile_test

// EVD-06 (REQ-CTX-005; EC-S16 to EC-S18; INV-21): "Permuting the arrival order of the same evidence set SHALL yield the identical classification (property test)". Reconcile is given every
// permutation of seeded random evidence sets over the real rule set of the fintech pack, and every answer, field by field (the classification, the rule that fired, the contributing items, the
// contradictions and the time until which it holds), must be the one the first order gave, errors included. SYNTHETIC.

import (
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

const (
	sets    = 400 // evidence sets
	maxSize = 6   // 720 orders of the largest
	seed    = 20261117
)

func theRuleSet(t *testing.T) *reconcile.RuleSet {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packs", "fintech", "recon_rules", "rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	rs, err := reconcile.ParseRuleSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

var (
	allStatuses = []reconcile.FactStatus{reconcile.Known, reconcile.Known, reconcile.Known, reconcile.Inferred, reconcile.Ambiguous, reconcile.Unavailable, reconcile.Stale,
		reconcile.Contradictory, reconcile.Unknown, reconcile.Unauthorized}
	stateOf = map[string][]string{
		"debit": {"POSTED", "NOT_POSTED"}, "credit": {"CREDITED", "NOT_CREDITED", "PENDING"}, "switch": {"SUCCESSFUL", "FAILED", "TIMED_OUT", "EXECUTION_OUTCOME_UNKNOWN"},
		"settlement": {"PENDING", "SETTLED", "FAILED"}, "reversal": {"POSTED", "PENDING"},
	}
	entities = []string{"debit", "credit", "switch", "settlement", "reversal"}
)

// randomSet draws one evidence set: 2 to maxSize items of any entity and status, some with a field unset (absence), a few sharing a counterparty, an amount and an intent so that duplicates and
// contradictions arise.
func randomSet(r *rand.Rand) []reconcile.Evidence {
	n := 2 + r.Intn(maxSize-1)
	debits := r.Intn(3) == 0 // a third of the sets are debits that may repeat each other: the duplicate window and its time order are then reached
	out := make([]reconcile.Evidence, n)
	for i := range out {
		entity := entities[r.Intn(len(entities))]
		status := allStatuses[r.Intn(len(allStatuses))]
		if debits {
			entity = "debit"
		}
		fields := map[string]predicate.Value{}
		if r.Intn(6) != 0 {
			states := stateOf[entity]
			fields["state"] = predicate.Enum(states[r.Intn(len(states))])
		}
		switch entity {
		case "debit":
			fields["counterparty"] = predicate.Ref("cp_" + strconv.Itoa(r.Intn(2)))
			fields["amount"] = predicate.Int(int64(100 * (1 + r.Intn(2))))
			if debits {
				fields["counterparty"], fields["amount"] = predicate.Ref("cp_0"), predicate.Int(100)
			}
			fields["posted_at"] = predicate.Time(int64([]int{1000, 1200, 1500, 1700, 2300}[r.Intn(5)])) // inside and outside the 600 s duplicate window
			fields["intent"] = predicate.Ref("int_" + strconv.Itoa(r.Intn(2)))
			if debits {
				fields["intent"] = predicate.Ref("int_0")
				fields["state"] = predicate.Enum("POSTED")
				status = reconcile.Known
			}
		case "credit":
			fields["terminal"] = predicate.Bool(r.Intn(2) == 0)
		case "settlement":
			fields["expected_by"] = predicate.Time(int64(900 + r.Intn(300)))
		case "reversal":
			fields["original_debit_linked"] = predicate.Bool(r.Intn(2) == 0)
		}
		out[i] = reconcile.Evidence{ID: "evd_" + strconv.Itoa(i), Source: "src_" + entity, Entity: entity, Status: status, EffectiveAt: int64(900 + r.Intn(200)),
			ExpiresAt: int64(1000 + r.Intn(400)), Required: r.Intn(4) != 0, Fields: fields}
	}
	return out
}

// permutations calls f with every order of items (Heap's algorithm), on a copy the callee may keep.
func permutations(items []reconcile.Evidence, f func([]reconcile.Evidence)) {
	a := append([]reconcile.Evidence(nil), items...)
	var gen func(k int)
	gen = func(k int) {
		if k == 1 {
			f(append([]reconcile.Evidence(nil), a...))
			return
		}
		gen(k - 1)
		for i := 0; i < k-1; i++ {
			if k%2 == 0 {
				a[i], a[k-1] = a[k-1], a[i]
			} else {
				a[0], a[k-1] = a[k-1], a[0]
			}
			gen(k - 1)
		}
	}
	gen(len(a))
}

type answer struct {
	res reconcile.Result
	err string
}

func ask(rs *reconcile.RuleSet, e []reconcile.Evidence) answer {
	res, err := reconcile.Reconcile(rs, e, 1100)
	if err != nil {
		return answer{err: err.Error()}
	}
	return answer{res: res}
}

func TestEveryArrivalOrderOfTheSameEvidenceGivesTheSameAnswerField_ByField(t *testing.T) {
	rs := theRuleSet(t)
	r := rand.New(rand.NewSource(seed))
	classes := map[reconcile.Classification]int{}
	orders := 0
	for s := 0; s < sets; s++ {
		set := randomSet(r)
		want := ask(rs, set)
		classes[want.res.Classification]++
		permutations(set, func(p []reconcile.Evidence) {
			orders++
			if got := ask(rs, p); !reflect.DeepEqual(got, want) {
				ids := make([]string, len(p))
				for i := range p {
					ids[i] = p[i].ID
				}
				t.Fatalf("set %d: the order %v gave %+v, the first order gave %+v", s, ids, got, want)
			}
		})
	}
	// The property is not vacuous: the sets reach several classifications, and many orders were tried.
	if len(classes) < 4 || orders < sets*2 {
		t.Fatalf("the generator reached %d classifications over %d orders: %v", len(classes), orders, classes)
	}
}

// Reconcile does not change the evidence it is given (the caller's slice and its fields).
func TestReconcileLeavesTheCallersEvidenceAlone(t *testing.T) {
	rs := theRuleSet(t)
	r := rand.New(rand.NewSource(seed + 1))
	for s := 0; s < 100; s++ {
		set := randomSet(r)
		before := make([]reconcile.Evidence, len(set))
		copy(before, set)
		ask(rs, set)
		if !reflect.DeepEqual(before, set) {
			t.Fatalf("set %d was changed by Reconcile", s)
		}
	}
}
