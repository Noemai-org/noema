package mapping

import (
	"encoding/json"
	"errors"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

// ScopeFromDescriptors types the fields of the pack's evidence entities from its evidence_descriptors, which the evidence mapping needs for the list entities it reads (the case system's
// `case` and `complaint_ref`, A438: without a scope a mapping with a list row is refused; pass the result to WithScope). Every descriptor must be sound or the whole scope is refused: an entity with no
// name, an entity named twice, no field, a field type outside the contract's seven, an Enum with no value, a value on any other type. An error names no pack content (AGENT-13).
func ScopeFromDescriptors(pack []byte) (predicate.Scope, error) {
	var f struct {
		Descriptors []struct {
			Entity string `json:"entity"`
			Fields map[string]struct {
				Type   string   `json:"type"`
				Values []string `json:"values"`
			} `json:"fields"`
		} `json:"evidence_descriptors"`
	}
	if err := json.Unmarshal(pack, &f); err != nil || len(f.Descriptors) == 0 {
		return predicate.Scope{}, errors.New("the pack gives no evidence descriptor")
	}
	scalars := map[string]predicate.Type{"String": predicate.String, "Integer": predicate.Integer, "Boolean": predicate.Boolean, "Timestamp": predicate.Timestamp, "Duration": predicate.Duration, "Reference": predicate.Reference}
	sc := predicate.Scope{Entities: make(map[string]map[string]predicate.Type, len(f.Descriptors)), Variables: map[string]predicate.Type{}}
	for _, d := range f.Descriptors {
		if d.Entity == "" || len(d.Fields) == 0 {
			return predicate.Scope{}, errors.New("an evidence descriptor has no entity or no field")
		}
		if _, twice := sc.Entities[d.Entity]; twice {
			return predicate.Scope{}, errors.New("an evidence entity is described twice")
		}
		fields := make(map[string]predicate.Type, len(d.Fields))
		for name, ft := range d.Fields {
			switch {
			case ft.Type == "Enum":
				if len(ft.Values) == 0 {
					return predicate.Scope{}, errors.New("an enum field of an evidence descriptor has no value")
				}
				fields[name] = predicate.EnumOf(ft.Values...)
			case len(ft.Values) != 0:
				return predicate.Scope{}, errors.New("a field of an evidence descriptor that is not an enum lists values")
			default:
				t, ok := scalars[ft.Type]
				if !ok {
					return predicate.Scope{}, errors.New("an evidence descriptor has a field of an unknown type")
				}
				fields[name] = t
			}
		}
		sc.Entities[d.Entity] = fields
	}
	return sc, nil
}
