package intakeadapters

import (
	"context"

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
