package intakeadapters

import (
	"context"
	"fmt"
	"slices"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// FactNeed is exactly what the rules that apply to one target read: the typed variables and the evidence entities their conditions name. The adapter derives it from the pack's
// policy, so a source reads no more than it must and cannot leave out what a rule needs.
type FactNeed struct {
	Variables []string // sorted
	Entities  []string // sorted
}

// FactSet is what a FactSource answers. A variable that is read and genuinely null is present with a null Value; a variable or an entity that was not read is absent, and the adapter
// refuses an absence (ErrFacts): an entity the source could not read is an error of the source, never an empty list (INV-19).
type FactSet struct {
	Variables map[string]predicate.Value
	Entities  map[string][]predicate.Row
}

// FactSource supplies the facts the conditions of the policy read, for one query: the sealed snapshot (q.SnapshotID) and the intent it was sealed for. There is no production
// implementation yet: nothing in the repository reads the resolution status of the target slot, the logical resolution epoch, the case the plan routes to, or the case rows of the
// case system for the policy (see status/TRU-adapters-policies-clocks.md). The adapter asks for the minimum (FactNeed) and checks the answer against the pack's declared types.
type FactSource interface {
	PolicyFacts(ctx context.Context, q intake.PolicyQuery, need FactNeed) (FactSet, error)
}

// checkFacts builds the environment the conditions are evaluated in, or refuses the facts (ErrFacts). Everything needed must be there; nothing may be undeclared; each value must
// be of its declared type, an enum value one of the declared values. A null is allowed where a fact was read and is null. Nothing is added, defaulted or repaired.
func checkFacts(given FactSet, need FactNeed, sc predicate.Scope) (predicate.Env, error) {
	env := predicate.Env{Variables: make(map[string]predicate.Value, len(given.Variables)), Entities: make(map[string][]predicate.Row, len(given.Entities))}
	for _, name := range need.Variables {
		if _, ok := given.Variables[name]; !ok {
			return predicate.Env{}, fail(ErrFacts, "a variable the rules read was not given", fmt.Errorf("variable %s", name))
		}
	}
	for _, name := range need.Entities {
		if _, ok := given.Entities[name]; !ok {
			return predicate.Env{}, fail(ErrFacts, "an entity the rules read was not read", fmt.Errorf("entity %s", name))
		}
	}
	for name, v := range given.Variables {
		t, ok := sc.Variables[name]
		if !ok {
			return predicate.Env{}, fail(ErrFacts, "a variable is not declared by the pack", fmt.Errorf("variable %s", name))
		}
		if err := checkValue(t, v); err != nil {
			return predicate.Env{}, fail(ErrFacts, "a variable is not of its declared type", fmt.Errorf("variable %s: %w", name, err))
		}
		env.Variables[name] = v
	}
	for name, rows := range given.Entities {
		fields, ok := sc.Entities[name]
		if !ok {
			return predicate.Env{}, fail(ErrFacts, "an entity is not declared by the pack", fmt.Errorf("entity %s", name))
		}
		for _, row := range rows {
			for field, v := range row {
				t, ok := fields[field]
				if !ok {
					return predicate.Env{}, fail(ErrFacts, "an entity has a field the pack does not declare", fmt.Errorf("entity %s, field %s", name, field))
				}
				if err := checkValue(t, v); err != nil {
					return predicate.Env{}, fail(ErrFacts, "a field of an entity is not of its declared type", fmt.Errorf("%s.%s: %w", name, field, err))
				}
			}
		}
		env.Entities[name] = rows
	}
	return env, nil
}

func checkValue(t predicate.Type, v predicate.Value) error {
	if v.IsNull() {
		return nil
	}
	if v.Kind != t.Kind {
		return fmt.Errorf("the value is a %s and the pack declares a %s", v.Kind, t.Kind)
	}
	if t.Kind == predicate.KindEnum && !slices.Contains(t.Values, v.S) {
		return fmt.Errorf("%q is not a declared value", v.S)
	}
	return nil
}
