package mapping

// List entities of the evidence mapping, version 2 (ruling A438, interim; A407, A411; REQ-RES-001, REQ-CTX-001; INV-19, INV-21).
//
// The reconciliation rows of the mapping describe one thing a source says (a debit is posted). A case system says a list: its cases, its complaints. A list entity has one row per element of a
// payload list (`rows_from`), and a marker member (`complete`) that says whether the list is the whole answer. "No such case" is concluded only from a list marked complete: an empty list that is
// not marked complete, or a marker that is missing or not the boolean true, is not "none" but UNKNOWN (ErrIncomplete). Even a list with rows is UNKNOWN when it is not marked complete, because the
// next page may hold the row that changes the answer. A field an element does not give, or gives as a value the mapping cannot read, is left UNSET (absence is never a negative fact, INV-19).
//
// The entity's own types come from a scope the caller supplies (WithScope: the pack's evidence descriptors), not from the reconciliation rule set; an entity may be in one scope only.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

// ErrIncomplete is returned for a list payload that is not marked complete: the answer is UNKNOWN, never "none".
var ErrIncomplete = errors.New("evidence mapping: the list is not marked complete")

// ErrNoSuchList is returned when the mapping has no list entity of that name for that source.
var ErrNoSuchList = errors.New("evidence mapping: no such list entity")

// ErrListShape is returned for a list payload that is not the shape the mapping names.
var ErrListShape = errors.New("evidence mapping: the list payload is not the shape the mapping names")

// Option changes how Parse reads a mapping.
type Option func(*options)

type options struct{ scope predicate.Scope }

// WithScope names the entities beyond the rule set's that a list row may feed, with their typed fields. Without it a mapping that has a list row is refused.
func WithScope(sc predicate.Scope) Option { return func(o *options) { o.scope = sc } }

// listField is one field of a list entity, resolved against its declared type at load.
type listField struct {
	path string
	typ  predicate.Type
	enum map[string]string
}

type listSpec struct {
	entity, source, rowsFrom, complete string
	fields                             map[string]listField
	names                              []string // sorted
}

func checkListRow(r rowSpec, rsScope, extra predicate.Scope, known map[string]bool) (listSpec, error) {
	if r.RowsFrom == "" || r.Complete == "" {
		return listSpec{}, errors.New("a list entity names both rows_from and complete")
	}
	if r.RowsFrom == r.Complete {
		return listSpec{}, errors.New("complete must name a member other than the list itself")
	}
	if _, inRuleSet := rsScope.Entities[r.Entity]; inRuleSet {
		return listSpec{}, fmt.Errorf("entity %q is the rule set's: a list entity is in one scope only", r.Entity)
	}
	fields, ok := extra.Entities[r.Entity]
	if !ok {
		return listSpec{}, fmt.Errorf("unknown entity %q: it is in neither the rule set nor the scope given", r.Entity)
	}
	if !known[r.Source] {
		return listSpec{}, fmt.Errorf("source %q is not an evidence source of the pack", r.Source)
	}
	if len(r.Fields) == 0 {
		return listSpec{}, errors.New("a row maps at least one field")
	}
	l := listSpec{entity: r.Entity, source: r.Source, rowsFrom: r.RowsFrom, complete: r.Complete, fields: map[string]listField{}}
	for name, spec := range r.Fields {
		typ, ok := fields[name]
		if !ok {
			return listSpec{}, fmt.Errorf("entity %s has no field %q", r.Entity, name)
		}
		if spec.Path == "" || spec.AnyOf != "" || spec.Where != nil || spec.Value != "" {
			return listSpec{}, fmt.Errorf("%s.%s: a list field has a path (and a map for an enum), and nothing else", r.Entity, name)
		}
		f := listField{path: spec.Path, typ: typ}
		switch typ.Kind {
		case predicate.KindEnum:
			if len(spec.Map) == 0 {
				return listSpec{}, fmt.Errorf("%s.%s: an enum field needs a map", r.Entity, name)
			}
			for k, v := range spec.Map {
				if k == "" || !inEnum(typ, v) {
					return listSpec{}, fmt.Errorf("%s.%s: map entry %q to %q: the key is empty or the value is not a value of the field", r.Entity, name, k, v)
				}
			}
			f.enum = spec.Map
		case predicate.KindReference, predicate.KindString, predicate.KindInteger, predicate.KindTimestamp, predicate.KindBoolean:
			if spec.Map != nil {
				return listSpec{}, fmt.Errorf("%s.%s: only an enum field has a map", r.Entity, name)
			}
		default:
			return listSpec{}, fmt.Errorf("%s.%s: a %s field cannot be read from a list", r.Entity, name, typ.Kind)
		}
		l.fields[name] = f
		l.names = append(l.names, name)
	}
	sort.Strings(l.names)
	return l, nil
}

// ListRows gives the rows of one list entity of a source, from one stored payload, in the payload's own order. It answers ErrNoSuchList for an entity the mapping does not list for the source, ErrIncomplete
// unless the payload's marker is the boolean true (so an empty result means "no such row" only for a list marked complete), and ErrListShape for a payload that is not the named list of objects. On an
// error it returns no rows. A field an element does not give, or gives in another shape or as an unlisted value, is absent from its row.
func (m *Mapping) ListRows(source, entity string, payload map[string]any) ([]predicate.Row, error) {
	var l *listSpec
	for i := range m.lists {
		if m.lists[i].source == source && m.lists[i].entity == entity {
			l = &m.lists[i]
		}
	}
	if l == nil {
		return nil, ErrNoSuchList
	}
	if done, ok := payload[l.complete].(bool); !ok || !done {
		return nil, ErrIncomplete
	}
	list, ok := payload[l.rowsFrom].([]any)
	if !ok {
		return nil, ErrListShape
	}
	out := make([]predicate.Row, 0, len(list))
	for _, el := range list {
		obj, ok := el.(map[string]any)
		if !ok {
			return nil, ErrListShape
		}
		row := predicate.Row{}
		for _, name := range l.names {
			if v, ok := readField(l.fields[name], obj[l.fields[name].path]); ok {
				row[name] = v
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func readField(f listField, raw any) (predicate.Value, bool) {
	switch f.typ.Kind {
	case predicate.KindEnum:
		s, ok := raw.(string)
		if !ok {
			return predicate.Value{}, false
		}
		v, ok := f.enum[s]
		return predicate.Enum(v), ok
	case predicate.KindReference, predicate.KindString:
		s, ok := raw.(string)
		if !ok || s == "" || strings.ContainsFunc(s, unicode.IsControl) {
			return predicate.Value{}, false
		}
		if f.typ.Kind == predicate.KindReference {
			return predicate.Ref(s), true
		}
		return predicate.Str(s), true
	case predicate.KindInteger:
		n, ok := wholeNumber(raw)
		return predicate.Int(n), ok
	case predicate.KindTimestamp:
		s, ok := raw.(string)
		if !ok {
			return predicate.Value{}, false
		}
		t, err := time.Parse(time.RFC3339, s) // a zone is required: an instant, not a wall clock
		if err != nil {
			return predicate.Value{}, false
		}
		return predicate.Time(t.Unix()), true
	case predicate.KindBoolean:
		b, ok := raw.(bool)
		return predicate.Bool(b), ok
	}
	return predicate.Value{}, false
}

// wholeNumber reads a JSON number as the payload reader keeps it (json.Number, so none is rounded; int and int64 for a caller that built the payload itself): a whole number within the
// language's integer bound, and nothing else. A float is not a type of this area (money and counts are integers), so none is accepted.
func wholeNumber(raw any) (int64, bool) {
	var n int64
	switch v := raw.(type) {
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		n = i
	case int:
		n = int64(v)
	case int64:
		n = v
	default:
		return 0, false
	}
	if n > predicate.MaxInteger || n < -predicate.MaxInteger {
		return 0, false
	}
	return n, true
}
