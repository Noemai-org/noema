package predicate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	ip "github.com/Noemai-org/noema/services/core/internal/predicate"
)

var errPack = errors.New("outcome predicate")

type fieldType struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type descriptor struct {
	Entity string               `json:"entity"`
	Fields map[string]fieldType `json:"fields"`
}

type definition struct {
	OutcomeID         string   `json:"outcome_id"`
	SuccessPredicate  string   `json:"success_predicate"`
	SettleWindowSecs  int64    `json:"settle_window_seconds"`
	IntermediateState []string `json:"intermediate_states"`
}

type packFile struct {
	Descriptors []descriptor         `json:"evidence_descriptors"`
	Variables   map[string]fieldType `json:"predicate_variables"`
	Outcomes    []definition         `json:"outcome_definitions"`
}

// Definition is an outcome definition as the verifier needs it (SoT 15.3): the success predicate and the settle window.
type Definition struct {
	OutcomeID        string
	SuccessPredicate string
	SettleWindowSecs int64
}

// Pack is what Outcome takes from a loaded pack: the predicate scope built from its evidence descriptors and predicate
// variables, and its outcome definitions by ID. Packs are data (SoT 51): this reads JSON and imports no pack code.
type Pack struct {
	Scope       ip.Scope
	Definitions map[string]Definition
}

func typeOf(f fieldType) (ip.Type, error) {
	switch f.Type {
	case "Boolean":
		return ip.Boolean, nil
	case "String":
		return ip.String, nil
	case "Integer":
		return ip.Integer, nil
	case "Timestamp":
		return ip.Timestamp, nil
	case "Duration":
		return ip.Duration, nil
	case "Reference":
		return ip.Reference, nil
	case "Enum":
		if len(f.Values) == 0 {
			return ip.Type{}, fmt.Errorf("%w: an Enum needs values", errPack)
		}
		return ip.EnumOf(f.Values...), nil
	}
	return ip.Type{}, fmt.Errorf("%w: unknown field type %q", errPack, f.Type)
}

// LoadPack reads a pack's descriptors, variables and outcome definitions and type-checks every success predicate
// against them. A pack with a predicate that names an unknown entity, field or variable, or mixes types, does not
// load (REQ-VER-002, fail closed); the error says which outcome and why.
func LoadPack(raw []byte) (Pack, error) {
	var f packFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&f); err != nil {
		return Pack{}, fmt.Errorf("%w: %v", errPack, err)
	}
	sc := ip.Scope{Entities: map[string]map[string]ip.Type{}, Variables: map[string]ip.Type{}}
	for _, d := range f.Descriptors {
		fields := map[string]ip.Type{}
		for name, ft := range d.Fields {
			t, err := typeOf(ft)
			if err != nil {
				return Pack{}, fmt.Errorf("%s.%s: %w", d.Entity, name, err)
			}
			fields[name] = t
		}
		sc.Entities[d.Entity] = fields
	}
	for name, ft := range f.Variables {
		t, err := typeOf(ft)
		if err != nil {
			return Pack{}, fmt.Errorf("variable %s: %w", name, err)
		}
		sc.Variables[name] = t
	}
	out := Pack{Scope: sc, Definitions: map[string]Definition{}}
	for _, o := range f.Outcomes {
		if _, err := Compile(o.SuccessPredicate, sc); err != nil {
			return Pack{}, fmt.Errorf("outcome %s: %w", o.OutcomeID, err)
		}
		out.Definitions[o.OutcomeID] = Definition{OutcomeID: o.OutcomeID, SuccessPredicate: o.SuccessPredicate, SettleWindowSecs: o.SettleWindowSecs}
	}
	return out, nil
}

// Compile type-checks src against scope and returns a program that is pure: no clock, no I/O, no model.
func Compile(src string, scope ip.Scope) (*ip.Program, error) {
	p, err := ip.Compile(src, scope)
	if err != nil {
		return nil, err
	}
	if p.Type().Kind != ip.KindBoolean {
		return nil, fmt.Errorf("%w: a success predicate must be Boolean, not %s", errPack, p.Type().Kind)
	}
	return p, nil
}
