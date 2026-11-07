package predicate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	ip "github.com/Noemai-org/noema/services/core/internal/predicate"
)

var errPack = errors.New("outcome predicate")

// MaxSettleSeconds is the largest settle window that converts to a time.Duration. The contract allows more (2^53-1 seconds);
// beyond this the conversion wraps negative and a verdict would not wait.
const MaxSettleSeconds = math.MaxInt64 / int64(time.Second)

type fieldType struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type descriptor struct {
	Entity string               `json:"entity"`
	Source json.RawMessage      `json:"source"`
	Fields map[string]fieldType `json:"fields"`
}

// definition lists every field of outcome-definition.v1 so that a misspelled one is an unknown field and not a silent zero.
type definition struct {
	OutcomeID            string          `json:"outcome_id"`
	IntentNodes          json.RawMessage `json:"intent_nodes"`
	SuccessPredicate     string          `json:"success_predicate"`
	IntermediateStates   json.RawMessage `json:"intermediate_states"`
	TerminalStates       json.RawMessage `json:"terminal_states"`
	SettleWindowSeconds  *int64          `json:"settle_window_seconds"`
	VerificationSources  json.RawMessage `json:"verification_sources"`
	Compensation         json.RawMessage `json:"compensation"`
	ExplanationTemplates json.RawMessage `json:"explanation_templates"`
	SLA                  json.RawMessage `json:"sla"`
}

type packFile struct {
	Descriptors []json.RawMessage          `json:"evidence_descriptors"`
	Variables   map[string]json.RawMessage `json:"predicate_variables"`
	Outcomes    []json.RawMessage          `json:"outcome_definitions"`
}

// strict decodes one value and refuses a field it does not know.
func strict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
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

// LoadPack reads a pack's descriptors, variables and outcome definitions and type-checks every success predicate against
// them. It is strict where a mistake would otherwise be silent: a field it does not know, a duplicate entity or outcome ID, a
// missing or negative settle window, and a predicate that names an unknown entity, field or variable or is not Boolean all
// stop the load (REQ-VER-002, fail closed). The error says which outcome and why.
//
// The pack schema has no pending condition yet (only intermediate_states), so no pack can produce PENDING_EXTERNAL; the caller
// supplies a pending condition to the verifier until OUT-02 defines one in the schema.
func LoadPack(raw []byte) (Pack, error) {
	var f packFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return Pack{}, fmt.Errorf("%w: %v", errPack, err)
	}
	// A pack holds many other top-level keys that Outcome does not read, so unknown keys cannot be refused here. The three it does
	// read must be present: a misspelled one would otherwise decode to nothing and load as an empty, unusable pack.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return Pack{}, fmt.Errorf("%w: %v", errPack, err)
	}
	for _, key := range []string{"evidence_descriptors", "predicate_variables", "outcome_definitions"} {
		if v, ok := top[key]; !ok || string(v) == "null" {
			return Pack{}, fmt.Errorf("%w: the pack has no %q", errPack, key)
		}
	}
	sc := ip.Scope{Entities: map[string]map[string]ip.Type{}, Variables: map[string]ip.Type{}}
	for i, rawDesc := range f.Descriptors {
		var d descriptor
		if err := strict(rawDesc, &d); err != nil {
			return Pack{}, fmt.Errorf("%w: evidence descriptor %d: %v", errPack, i, err)
		}
		if _, dup := sc.Entities[d.Entity]; dup {
			return Pack{}, fmt.Errorf("%w: two descriptors for entity %q", errPack, d.Entity)
		}
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
	for name, rawVar := range f.Variables {
		var ft fieldType
		if err := strict(rawVar, &ft); err != nil {
			return Pack{}, fmt.Errorf("%w: variable %s: %v", errPack, name, err)
		}
		t, err := typeOf(ft)
		if err != nil {
			return Pack{}, fmt.Errorf("variable %s: %w", name, err)
		}
		sc.Variables[name] = t
	}
	out := Pack{Scope: sc, Definitions: map[string]Definition{}}
	for i, rawDef := range f.Outcomes {
		var o definition
		if err := strict(rawDef, &o); err != nil {
			return Pack{}, fmt.Errorf("%w: outcome definition %d: %v", errPack, i, err)
		}
		if o.OutcomeID == "" {
			return Pack{}, fmt.Errorf("%w: outcome definition %d has no outcome_id", errPack, i)
		}
		if _, dup := out.Definitions[o.OutcomeID]; dup {
			return Pack{}, fmt.Errorf("%w: two definitions for outcome %q", errPack, o.OutcomeID)
		}
		if o.SettleWindowSeconds == nil || *o.SettleWindowSeconds < 0 || *o.SettleWindowSeconds > MaxSettleSeconds {
			return Pack{}, fmt.Errorf("%w: outcome %s needs a settle window from 0 to %d seconds", errPack, o.OutcomeID, MaxSettleSeconds)
		}
		if _, err := Compile(o.SuccessPredicate, sc); err != nil {
			return Pack{}, fmt.Errorf("outcome %s: %w", o.OutcomeID, err)
		}
		out.Definitions[o.OutcomeID] = Definition{OutcomeID: o.OutcomeID, SuccessPredicate: o.SuccessPredicate, SettleWindowSecs: *o.SettleWindowSeconds}
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
