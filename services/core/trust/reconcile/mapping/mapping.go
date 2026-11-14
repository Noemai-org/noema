// Package mapping turns a source's stored payload into rows of the reconciliation rule set's vocabulary, by a versioned mapping that is pack data (ENV-04; ruling A208, relayed;
// REQ-RES-001, REQ-CTX-001; INV-19, INV-21, AGENT-16).
//
// The rule set speaks of entities such as `debit` and `switch` with enum fields; the Evidence Layer stores the ports' own words. This package is the one place the two meet. It has
// two closed forms and no expression language: a lookup (`path` and `map`: a payload string to an enum value) and one quantifier (`any_of`, `where`, `value`: if some element of
// a payload list has every listed field equal, the value). A field the payload does not give, or gives as a value the mapping does not list, is left UNSET: absence is never a
// negative fact (INV-19) and nothing is defaulted. A mapping that is not whole fails at load.
//
// May import: the standard library, internal/predicate and the parent package (for the rule set's types).
//
// Must never: read the clock, a file or a store; set a row's status (the evidence item's status is the caller's); default a field; name an industry term in code (the pack carries them).
package mapping

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

var errMapping = errors.New("evidence mapping")

type fieldSpec struct {
	Path  string            `json:"path"`
	Map   map[string]string `json:"map"`
	AnyOf string            `json:"any_of"`
	Where map[string]string `json:"where"`
	Value string            `json:"value"`
}

// rowKey is what makes a row unique: one entity from one source.
type rowKey struct{ entity, source string }

type rowSpec struct {
	Entity string               `json:"entity"`
	Source string               `json:"source"`
	Fields map[string]fieldSpec `json:"fields"`
}

type file struct {
	MappingID string    `json:"mapping_id"`
	Version   int       `json:"version"`
	Label     string    `json:"label"`
	Ruleset   string    `json:"ruleset"`
	Note      string    `json:"note"`
	Rows      []rowSpec `json:"rows"`
}

// Row is one row of an entity of the rule set: the fields the payload gave. A field that is not in Fields is unset.
type Row struct {
	Entity string
	Fields map[string]predicate.Value
}

// Mapping is a loaded, validated mapping.
type Mapping struct {
	id      string
	ruleset string
	rows    []rowSpec
}

// ID is the mapping's identity and version, `<mapping_id>@<version>`: a reconciliation records it next to the rule set's (INV-21).
func (m *Mapping) ID() string { return m.id }

// RuleSet is the rule set (`<id>@<version>`) the mapping was written for.
func (m *Mapping) RuleSet() string { return m.ruleset }

// Parse loads a mapping. rs is the rule set it must have been written for; sources are the ids of the pack's evidence sources. Anything missing, repeated, unknown or out of
// the rule set's vocabulary is an error: the pack is refused, never half-loaded.
func Parse(data []byte, rs *reconcile.RuleSet, sources []string) (*Mapping, error) {
	if err := noRepeatedMember(data); err != nil {
		return nil, err
	}
	var f file
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: not a mapping document", errMapping)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: more than one document", errMapping)
	}
	if f.MappingID == "" || f.Version < 1 || len(f.Rows) == 0 {
		return nil, fmt.Errorf("%w: needs a mapping_id, a positive version and rows", errMapping)
	}
	if f.Ruleset != rs.ID() {
		return nil, fmt.Errorf("%w: written for rule set %q, not %q", errMapping, f.Ruleset, rs.ID())
	}
	known := map[string]bool{}
	for _, s := range sources {
		known[s] = true
	}
	seen := map[rowKey]bool{}
	for i, r := range f.Rows {
		if err := checkRow(r, rs.Scope(), known); err != nil {
			return nil, fmt.Errorf("%w: rows[%d]: %v", errMapping, i, err)
		}
		key := rowKey{r.Entity, r.Source}
		if seen[key] {
			return nil, fmt.Errorf("%w: rows[%d]: entity %s is mapped twice from source %s", errMapping, i, r.Entity, r.Source)
		}
		seen[key] = true
	}
	return &Mapping{id: f.MappingID + "@" + strconv.Itoa(f.Version), ruleset: f.Ruleset, rows: f.Rows}, nil
}

func checkRow(r rowSpec, scope predicate.Scope, known map[string]bool) error {
	fields, ok := scope.Entities[r.Entity]
	if !ok {
		return fmt.Errorf("unknown entity %q", r.Entity)
	}
	if !known[r.Source] {
		return fmt.Errorf("source %q is not an evidence source of the pack", r.Source)
	}
	if len(r.Fields) == 0 {
		return errors.New("a row maps at least one field")
	}
	for name, spec := range r.Fields {
		typ, ok := fields[name]
		if !ok {
			return fmt.Errorf("entity %s has no field %q", r.Entity, name)
		}
		if typ.Kind != predicate.KindEnum {
			return fmt.Errorf("%s.%s is not an enum: this mapping maps enum fields only", r.Entity, name)
		}
		if err := checkSpec(spec, typ); err != nil {
			return fmt.Errorf("%s.%s: %v", r.Entity, name, err)
		}
	}
	return nil
}

func inEnum(t predicate.Type, v string) bool {
	for _, x := range t.Values {
		if x == v {
			return true
		}
	}
	return false
}

func checkSpec(s fieldSpec, typ predicate.Type) error {
	lookup := s.Path != "" || s.Map != nil
	quant := s.AnyOf != "" || s.Where != nil || s.Value != ""
	switch {
	case lookup == quant:
		return errors.New("a field is exactly one of a lookup (path and map) or a quantifier (any_of, where and value)")
	case lookup:
		if s.Path == "" || len(s.Map) == 0 || s.AnyOf != "" || s.Where != nil || s.Value != "" {
			return errors.New("a lookup has a path and a map, and nothing else")
		}
		for k, v := range s.Map {
			if k == "" || !inEnum(typ, v) {
				return fmt.Errorf("map entry %q to %q: the key is empty or the value is not a value of the field", k, v)
			}
		}
	default:
		if s.AnyOf == "" || len(s.Where) == 0 || !inEnum(typ, s.Value) || s.Path != "" || s.Map != nil {
			return errors.New("a quantifier has any_of, a where naming at least one field, and a value of the field, and nothing else")
		}
		for k, v := range s.Where {
			if k == "" || v == "" {
				return errors.New("where names a field and the value it must equal")
			}
		}
	}
	return nil
}

// Rows gives the rows of the rule set's vocabulary for one stored payload of a source, in the mapping's own order. A source the mapping does not know gives none. A field the
// payload does not give (or gives in a shape or as a value the mapping does not list) is absent from its row's Fields. It never panics on a payload of any shape.
func (m *Mapping) Rows(source string, payload map[string]any) ([]Row, error) {
	var out []Row
	for _, spec := range m.rows {
		if spec.Source != source {
			continue
		}
		row := Row{Entity: spec.Entity, Fields: map[string]predicate.Value{}}
		names := make([]string, 0, len(spec.Fields))
		for name := range spec.Fields {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if v, ok := evaluate(spec.Fields[name], payload); ok {
				row.Fields[name] = predicate.Enum(v)
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func evaluate(s fieldSpec, payload map[string]any) (string, bool) {
	if s.Path != "" {
		got, ok := payload[s.Path].(string)
		if !ok {
			return "", false
		}
		v, ok := s.Map[got]
		return v, ok
	}
	list, ok := payload[s.AnyOf].([]any)
	if !ok {
		return "", false
	}
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		all := true
		for k, want := range s.Where {
			if got, ok := obj[k].(string); !ok || got != want {
				all = false
				break
			}
		}
		if all {
			return s.Value, true
		}
	}
	return "", false
}

// noRepeatedMember refuses a JSON object that names a member twice at any depth: the standard decoder keeps the last one silently, and a pack file must mean one thing.
func noRepeatedMember(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch d := tok.(type) {
		case json.Delim:
			if d == '{' {
				seen := map[string]bool{}
				for dec.More() {
					k, err := dec.Token()
					if err != nil {
						return err
					}
					key, _ := k.(string)
					if seen[key] {
						return fmt.Errorf("%w: the member %q is repeated", errMapping, key)
					}
					seen[key] = true
					if err := walk(); err != nil {
						return err
					}
				}
			} else {
				for dec.More() {
					if err := walk(); err != nil {
						return err
					}
				}
			}
			_, err := dec.Token()
			return err
		}
		return nil
	}
	if err := walk(); err != nil {
		if errors.Is(err, errMapping) {
			return err
		}
		return fmt.Errorf("%w: not a mapping document", errMapping)
	}
	return nil
}
