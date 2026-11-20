package mapping

import (
	"errors"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

// This file is a stub (red: tests first). The list entities of fin.map.v1@2 (ruling A438) are built in the next commit.

// ErrIncomplete is returned for a list payload that is not marked complete.
var ErrIncomplete = errors.New("evidence mapping: the list is not marked complete")

// ErrNoSuchList is returned when the mapping has no list entity of that name for that source.
var ErrNoSuchList = errors.New("evidence mapping: no such list entity")

// ErrListShape is returned for a list payload that is not the shape the mapping names.
var ErrListShape = errors.New("evidence mapping: the list payload is not the shape the mapping names")

// Option changes how Parse reads a mapping.
type Option func(*options)

type options struct{}

// WithScope names entities beyond the rule set's that a list row may feed.
func WithScope(predicate.Scope) Option { return func(*options) {} }

// ListRows gives the rows of one list entity.
func (m *Mapping) ListRows(source, entity string, payload map[string]any) ([]predicate.Row, error) {
	return nil, errors.New("not built")
}
