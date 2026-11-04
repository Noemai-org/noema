package planner_test

// REQ-PLN-001 and REQ-PLN-002 acceptance: identical inputs give an identical plan_hash; changing any plan field changes
// it; plans contain only registered, non-prohibited IDs. INV-18.

import (
	"fmt"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/execution/planner"
)

func genParams() *rapid.Generator[map[string]any] {
	val := rapid.OneOf(
		rapid.Map(rapid.StringN(0, 12, 20), func(s string) any { return s }),
		rapid.Map(rapid.Int64Range(-9007199254740991, 9007199254740991), func(n int64) any { return n }),
		rapid.Map(rapid.Bool(), func(b bool) any { return b }),
		rapid.Just[any](nil),
	)
	return rapid.MapOfN(rapid.StringMatching(`[a-z][a-z_]{0,8}`), val, 0, 4)
}

func genSpecs(ids []string) *rapid.Generator[[]planner.StepSpec] {
	step := rapid.Custom(func(t *rapid.T) planner.StepSpec {
		return planner.StepSpec{
			NodeID:         rapid.StringMatching(`[a-z][a-z_]{1,10}`).Draw(t, "node"),
			CapabilityID:   rapid.SampledFrom(ids).Draw(t, "cap"),
			Params:         genParams().Draw(t, "params"),
			Preconditions:  rapid.SliceOfN(rapid.StringMatching(`[a-z]{1,8}`), 0, 2).Draw(t, "pre"),
			Postconditions: rapid.SliceOfN(rapid.StringMatching(`[a-z]{1,8}`), 0, 2).Draw(t, "post"),
		}
	})
	return rapid.SliceOfN(step, 1, 4)
}

func build(t *rapid.T, gv, node string, specs []planner.StepSpec) string {
	r := reg(t)
	p, err := planner.New(r, gv, node, specs)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h, err := p.Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	return h
}

// Identical inputs, identical hash; the order in which params were written into the map does not matter.
func TestIdenticalInputsGiveAnIdenticalHash(t *testing.T) {
	r := reg(t)
	ids := allowed(t, r)
	rapid.Check(t, func(rt *rapid.T) {
		specs := genSpecs(ids).Draw(rt, "specs")
		a := build(rt, "1.0.0", "fin.node", specs)
		copyOf := make([]planner.StepSpec, len(specs))
		for i, s := range specs {
			m := map[string]any{}
			for k, v := range s.Params {
				m[k] = v
			}
			s.Params = m
			copyOf[i] = s
		}
		if b := build(rt, "1.0.0", "fin.node", copyOf); a != b {
			rt.Fatalf("the same plan hashed twice to %s and %s", a, b)
		}
	})
}

// Changing any one field of any step, or the versions, changes the hash.
func TestChangingAnyPlanFieldChangesTheHash(t *testing.T) {
	r := reg(t)
	ids := allowed(t, r)
	rapid.Check(t, func(rt *rapid.T) {
		specs := genSpecs(ids).Draw(rt, "specs")
		base := build(rt, "1.0.0", "fin.node", specs)
		i := rapid.IntRange(0, len(specs)-1).Draw(rt, "which")
		mut := make([]planner.StepSpec, len(specs))
		for k, s := range specs {
			m := map[string]any{}
			for pk, pv := range s.Params {
				m[pk] = pv
			}
			s.Params = m
			s.Preconditions = append([]string(nil), s.Preconditions...)
			s.Postconditions = append([]string(nil), s.Postconditions...)
			mut[k] = s
		}
		gv, node := "1.0.0", "fin.node"
		switch rapid.IntRange(0, 6).Draw(rt, "field") {
		case 0:
			gv = "1.0.1"
		case 1:
			node = "fin.node2"
		case 2:
			mut[i].NodeID += "x"
		case 3:
			other := rapid.SampledFrom(ids).Filter(func(s string) bool { return s != mut[i].CapabilityID }).Draw(rt, "other")
			mut[i].CapabilityID = other
		case 4:
			mut[i].Params["added_param"] = "v"
		case 5:
			mut[i].Preconditions = append(mut[i].Preconditions, "extra")
		case 6:
			mut[i].Postconditions = append(mut[i].Postconditions, "extra")
		}
		if got := build(rt, gv, node, mut); got == base {
			rt.Fatalf("a changed plan kept the hash %s", base)
		}
	})
}

// Changing an existing param's value changes the hash.
func TestChangingAParamValueChangesTheHash(t *testing.T) {
	r := reg(t)
	ids := allowed(t, r)
	rapid.Check(t, func(rt *rapid.T) {
		specs := genSpecs(ids).Filter(func(s []planner.StepSpec) bool { return len(s[0].Params) > 0 }).Draw(rt, "specs")
		base := build(rt, "1.0.0", "fin.node", specs)
		var key string
		for k := range specs[0].Params {
			key = k
			break
		}
		old := specs[0].Params[key]
		repl := any("changed-value-é")
		if s, ok := old.(string); ok && s == repl {
			repl = int64(7)
		}
		m := map[string]any{}
		for k, v := range specs[0].Params {
			m[k] = v
		}
		m[key] = repl
		mut := append([]planner.StepSpec(nil), specs...)
		mut[0].Params = m
		if got := build(rt, "1.0.0", "fin.node", mut); got == base {
			rt.Fatalf("param %q changed from %v to %v and the hash stayed %s", key, old, repl, base)
		}
	})
}

// The order of steps is part of the plan.
func TestReorderingStepsChangesTheHash(t *testing.T) {
	r := reg(t)
	ids := allowed(t, r)
	rapid.Check(t, func(rt *rapid.T) {
		specs := genSpecs(ids).Filter(func(s []planner.StepSpec) bool { return len(s) >= 2 }).Draw(rt, "specs")
		swapped := append([]planner.StepSpec(nil), specs...)
		swapped[0], swapped[1] = specs[1], specs[0]
		a, b := build(rt, "1.0.0", "fin.node", specs), build(rt, "1.0.0", "fin.node", swapped)
		same := specs[0].NodeID == specs[1].NodeID && specs[0].CapabilityID == specs[1].CapabilityID && fmtParams(specs[0]) == fmtParams(specs[1])
		if !same && a == b {
			rt.Fatalf("swapping two different steps kept the hash %s", a)
		}
	})
}

func fmtParams(s planner.StepSpec) string {
	return fmt.Sprintf("%v|%v|%v", s.Params, s.Preconditions, s.Postconditions)
}
