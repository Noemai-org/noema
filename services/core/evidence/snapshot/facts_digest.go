package snapshot

import "errors"

// ErrFactsInput is returned by FactsDigest for facts that cannot be digested: none, an empty entity, status, field name or value kind, a kind that is not one of the seven, a value
// that is not in the canonical text of its kind, or two facts of one entity. It is never a digest of what is left.
var ErrFactsInput = errors.New("snapshot: the mapped facts of an evidence item cannot be digested")

// errFactsNotBuilt is what the stub answers until the function is built.
var errFactsNotBuilt = errors.New("snapshot: FactsDigest is not built")

// The kinds of a fact's value, as the evidence mapping's typed values name them.
const (
	KindEnum      = "enum"
	KindString    = "string"
	KindReference = "reference"
	KindBoolean   = "boolean"
	KindInteger   = "integer"
	KindTimestamp = "timestamp"
	KindDuration  = "duration"
)

// FactValue is one typed value of a fact: its kind and its canonical text.
type FactValue struct {
	Kind string
	Text string
}

// Fact is one row of the evidence mapping for an item: the entity, the item's fact status as the Reconciler gives it to the row, and the fields the payload gave.
type Fact struct {
	Entity string
	Status string
	Fields map[string]FactValue
}

// FactsDigest is not built.
func FactsDigest(facts []Fact) (string, error) { return "", errFactsNotBuilt }
