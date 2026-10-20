package reconcile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

type fieldSpec struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type factRef struct {
	Entity string `json:"entity"`
	Field  string `json:"field"`
	Value  string `json:"value"`
}

type incompatible struct {
	Label  string   `json:"label"`
	A      factRef  `json:"a"`
	B      factRef  `json:"b"`
	Unless *factRef `json:"unless"`
}

type singleValued struct {
	Entity string `json:"entity"`
	Field  string `json:"field"`
}

type derivedSpec struct {
	Name          string   `json:"name"`
	Entity        string   `json:"entity"`
	Require       factRef  `json:"require"`
	Same          []string `json:"same"`
	TimeField     string   `json:"time_field"`
	WithinSeconds int64    `json:"within_seconds"`
}

type ruleSpec struct {
	ID             string   `json:"id"`
	Classification string   `json:"classification"`
	Condition      string   `json:"condition"`
	Uses           []string `json:"uses"`
}

type pendingMarker struct {
	Entity string   `json:"entity"`
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

type rulesetFile struct {
	RulesetID      string                          `json:"ruleset_id"`
	Version        int                             `json:"version"`
	Label          string                          `json:"label"`
	Entities       map[string]map[string]fieldSpec `json:"entities"`
	Incompatible   []incompatible                  `json:"incompatible"`
	SingleValued   []singleValued                  `json:"single_valued"`
	Derived        []derivedSpec                   `json:"derived"`
	Rules          []ruleSpec                      `json:"rules"`
	Fallback       ruleSpec                        `json:"fallback"`
	Certainty      map[string]int                  `json:"certainty"`
	PendingMarkers []pendingMarker                 `json:"pending_markers"`
}

type rule struct {
	id             string
	classification Classification
	prog           *predicate.Program
	uses           []string
}

// RuleSet is a compiled, versioned rule set: pack data turned into programs once, at pack load.
type RuleSet struct {
	id           string
	version      int
	scope        predicate.Scope
	incompatible []incompatible
	single       []singleValued
	derived      []derivedSpec
	rules        []rule
	fallback     rule
	certainty    map[Classification]int
	pending      []pendingMarker
}

// Engine entities every rule set may use besides its own: the contradictions found, the derived clusters and the
// unavailable items. The standard fields below are added to every evidence entity.
const (
	entityContradiction = "contradiction"
	entityDuplicate     = "duplicate"
	entityUnavailable   = "unavailable"
)

var errRuleSet = errors.New("rule set")

func typeFromSpec(s fieldSpec) (predicate.Type, error) {
	switch s.Type {
	case "Boolean":
		return predicate.Boolean, nil
	case "String":
		return predicate.String, nil
	case "Integer":
		return predicate.Integer, nil
	case "Timestamp":
		return predicate.Timestamp, nil
	case "Duration":
		return predicate.Duration, nil
	case "Reference":
		return predicate.Reference, nil
	case "Enum":
		if len(s.Values) == 0 {
			return predicate.Type{}, fmt.Errorf("%w: an Enum field needs values", errRuleSet)
		}
		return predicate.EnumOf(s.Values...), nil
	}
	return predicate.Type{}, fmt.Errorf("%w: unknown field type %q", errRuleSet, s.Type)
}

func buildScope(f rulesetFile) (predicate.Scope, error) {
	sc := predicate.Scope{Entities: map[string]map[string]predicate.Type{}, Variables: map[string]predicate.Type{}}
	std := map[string]predicate.Type{"status": predicate.EnumOf(allStatuses...), "source": predicate.Reference,
		"evidence_id": predicate.Reference, "effective_at": predicate.Timestamp, "required": predicate.Boolean}
	for name, fields := range f.Entities {
		if name == entityContradiction || name == entityDuplicate || name == entityUnavailable {
			return sc, fmt.Errorf("%w: entity %q is reserved", errRuleSet, name)
		}
		sc.Entities[name] = map[string]predicate.Type{}
		for k, v := range std {
			sc.Entities[name][k] = v
		}
		for field, spec := range fields {
			t, err := typeFromSpec(spec)
			if err != nil {
				return sc, err
			}
			if _, clash := std[field]; clash {
				return sc, fmt.Errorf("%w: field %q of %s is a standard field", errRuleSet, field, name)
			}
			sc.Entities[name][field] = t
		}
	}
	sc.Entities[entityContradiction] = map[string]predicate.Type{"label": predicate.Reference, "a": predicate.Reference, "b": predicate.Reference}
	sc.Entities[entityDuplicate] = map[string]predicate.Type{"size": predicate.Integer, "key": predicate.Reference}
	sc.Entities[entityUnavailable] = map[string]predicate.Type{"entity": predicate.Reference, "required": predicate.Boolean, "evidence_id": predicate.Reference}
	return sc, nil
}

// ParseRuleSet decodes a rule set (strictly: an unknown field is an error), type-checks every rule and matrix entry
// against its own entities, and compiles the rule predicates. A rule set that fails here never loads (fail closed).
func ParseRuleSet(data []byte) (*RuleSet, error) {
	var f rulesetFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: %v", errRuleSet, err)
	}
	if f.RulesetID == "" || f.Version < 1 {
		return nil, fmt.Errorf("%w: ruleset_id and a positive version are required", errRuleSet)
	}
	sc, err := buildScope(f)
	if err != nil {
		return nil, err
	}
	rs := &RuleSet{id: f.RulesetID, version: f.Version, scope: sc, incompatible: f.Incompatible, single: f.SingleValued, derived: f.Derived, pending: f.PendingMarkers,
		certainty: map[Classification]int{}}
	if err := rs.checkMatrix(f); err != nil {
		return nil, err
	}
	if err := rs.compileRules(f); err != nil {
		return nil, err
	}
	return rs, rs.checkCertainty(f)
}

func (rs *RuleSet) checkRef(r factRef) error {
	fields, ok := rs.scope.Entities[r.Entity]
	if !ok {
		return fmt.Errorf("%w: unknown entity %q", errRuleSet, r.Entity)
	}
	t, ok := fields[r.Field]
	if !ok {
		return fmt.Errorf("%w: entity %s has no field %q", errRuleSet, r.Entity, r.Field)
	}
	if t.Kind == predicate.KindEnum && r.Value != "" {
		for _, v := range t.Values {
			if v == r.Value {
				return nil
			}
		}
		return fmt.Errorf("%w: %q is not a value of %s.%s", errRuleSet, r.Value, r.Entity, r.Field)
	}
	return nil
}

func (rs *RuleSet) checkMatrix(f rulesetFile) error {
	seen := map[string]bool{}
	for _, m := range f.Incompatible {
		if m.Label == "" || seen[m.Label] {
			return fmt.Errorf("%w: matrix labels must be present and unique (%q)", errRuleSet, m.Label)
		}
		seen[m.Label] = true
		refs := []factRef{m.A, m.B}
		if m.Unless != nil {
			refs = append(refs, *m.Unless)
		}
		for _, r := range refs {
			if err := rs.checkRef(r); err != nil {
				return err
			}
		}
	}
	for _, sv := range f.SingleValued {
		if err := rs.checkRef(factRef{Entity: sv.Entity, Field: sv.Field}); err != nil {
			return err
		}
	}
	for _, d := range f.Derived {
		if _, ok := rs.scope.Entities[d.Entity]; !ok || d.Name != entityDuplicate || len(d.Same) == 0 || d.WithinSeconds < 0 {
			return fmt.Errorf("%w: derived %q is malformed (only %q is supported)", errRuleSet, d.Name, entityDuplicate)
		}
		if err := rs.checkRef(d.Require); err != nil {
			return err
		}
		for _, field := range append([]string{d.TimeField}, d.Same...) {
			if _, ok := rs.scope.Entities[d.Entity][field]; !ok {
				return fmt.Errorf("%w: derived %s names an unknown field %q", errRuleSet, d.Name, field)
			}
		}
	}
	return nil
}

func (rs *RuleSet) compileRule(spec ruleSpec) (rule, error) {
	known := false
	for _, c := range allClassifications {
		known = known || string(c) == spec.Classification
	}
	if !known {
		return rule{}, fmt.Errorf("%w: rule %s names classification %q, which is not one of the twelve", errRuleSet, spec.ID, spec.Classification)
	}
	r := rule{id: spec.ID + "@" + strconv.Itoa(rs.version), classification: Classification(spec.Classification), uses: spec.Uses}
	for _, u := range spec.Uses {
		if _, ok := rs.scope.Entities[u]; !ok {
			return rule{}, fmt.Errorf("%w: rule %s uses unknown entity %q", errRuleSet, spec.ID, u)
		}
	}
	if spec.Condition == "" {
		return r, nil
	}
	p, err := predicate.Compile(spec.Condition, rs.scope)
	if err != nil {
		return rule{}, fmt.Errorf("%w: rule %s: %v", errRuleSet, spec.ID, err)
	}
	if p.Type().Kind != predicate.KindBoolean {
		return rule{}, fmt.Errorf("%w: rule %s is not Boolean", errRuleSet, spec.ID)
	}
	r.prog = p
	return r, nil
}

func (rs *RuleSet) compileRules(f rulesetFile) error {
	ids := map[string]bool{}
	for _, spec := range f.Rules {
		if spec.Condition == "" || ids[spec.ID] {
			return fmt.Errorf("%w: rule %q needs a condition and a unique id", errRuleSet, spec.ID)
		}
		ids[spec.ID] = true
		r, err := rs.compileRule(spec)
		if err != nil {
			return err
		}
		rs.rules = append(rs.rules, r)
	}
	if f.Fallback.Condition != "" || f.Fallback.Classification != string(InsufficientEvidence) {
		return fmt.Errorf("%w: the fallback is INSUFFICIENT_EVIDENCE with no condition (REQ-RES-001)", errRuleSet)
	}
	fb, err := rs.compileRule(f.Fallback)
	rs.fallback = fb
	return err
}

func (rs *RuleSet) checkCertainty(f rulesetFile) error {
	for _, c := range allClassifications {
		tier, ok := f.Certainty[string(c)]
		if !ok {
			return fmt.Errorf("%w: certainty has no tier for %s", errRuleSet, c)
		}
		rs.certainty[c] = tier
	}
	if len(f.Certainty) != len(allClassifications) {
		return fmt.Errorf("%w: certainty names a classification that does not exist", errRuleSet)
	}
	return nil
}

// ID is "ruleset_id@version", the value recorded with every result.
func (rs *RuleSet) ID() string { return rs.id + "@" + strconv.Itoa(rs.version) }

// Certainty is the tier of a classification in the rule set: 0 for no finding, higher for a more definite one.
// Removing evidence must never move a result to a higher tier (REQ-RES-001, metamorphic property).
func (rs *RuleSet) Certainty(c Classification) int { return rs.certainty[c] }

// Scope exposes the type scope for tests and for pack-load checks.
func (rs *RuleSet) Scope() predicate.Scope { return rs.scope }

// MatrixLabels lists the compatibility matrix labels in file order.
func (rs *RuleSet) MatrixLabels() []string {
	out := make([]string, 0, len(rs.incompatible))
	for _, m := range rs.incompatible {
		out = append(out, m.Label)
	}
	return out
}

// PendingMarker is one (entity, field, value) that marks a fact as pending.
type PendingMarker struct{ Entity, Field, Value string }

// PendingMarkers returns, for tests, the values that mark a pending fact, in a stable order.
func (rs *RuleSet) PendingMarkers() []PendingMarker {
	var out []PendingMarker
	for _, p := range rs.pending {
		for _, v := range p.Values {
			out = append(out, PendingMarker{p.Entity, p.Field, v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]) < fmt.Sprint(out[j]) })
	return out
}

// Fact names an (entity, field, value) triple of the compatibility matrix.
type Fact struct{ Entity, Field, Value string }

// MatrixEntry is one pair of values the rule set declares incompatible. Unless, when set, cancels the pair.
type MatrixEntry struct {
	Label  string
	A, B   Fact
	Unless *Fact
}

// Matrix returns the compatibility matrix in file order, so that tests can assert every entry.
func (rs *RuleSet) Matrix() []MatrixEntry {
	out := make([]MatrixEntry, 0, len(rs.incompatible))
	for _, m := range rs.incompatible {
		e := MatrixEntry{Label: m.Label, A: Fact(m.A), B: Fact(m.B)}
		if m.Unless != nil {
			u := Fact(*m.Unless)
			e.Unless = &u
		}
		out = append(out, e)
	}
	return out
}

// Produces lists the classifications some rule of the set can return, plus the fallback's. A classification that is not
// listed can never be returned.
func (rs *RuleSet) Produces() []Classification {
	seen := map[Classification]bool{rs.fallback.classification: true}
	for _, r := range rs.rules {
		seen[r.classification] = true
	}
	out := make([]Classification, 0, len(seen))
	for _, c := range allClassifications {
		if seen[c] {
			out = append(out, c)
		}
	}
	return out
}
