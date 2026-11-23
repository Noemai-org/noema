package intakeadapters

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/policy"
)

// fieldSpec is the type of a variable or an entity field as the pack writes it.
type fieldSpec struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

func typeOf(s fieldSpec) (predicate.Type, error) {
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
			return predicate.Type{}, fmt.Errorf("an Enum needs values")
		}
		return predicate.EnumOf(s.Values...), nil
	}
	return predicate.Type{}, fmt.Errorf("unknown type %q", s.Type)
}

// topLevel decodes the keys of a pack file the adapters read. A pack holds many other keys, so an unknown key is not refused; a key the adapters need is required to be present.
func topLevel(raw []byte, keys ...string) (map[string]json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fail(ErrPack, "the pack does not decode", err)
	}
	for _, k := range keys {
		if v, ok := top[k]; !ok || string(v) == "null" {
			return nil, fmt.Errorf("%w: the pack has no %q", ErrPack, k)
		}
	}
	return top, nil
}

// vocabulary is the scope a pack's policy is checked against and the capability IDs its rules may name (policy.Vocabulary): the typed variables, the evidence entities with their
// fields, and the capabilities. An intent node is not a target here: see the README, question 3.
func vocabulary(top map[string]json.RawMessage) (policy.Vocabulary, error) {
	var caps []struct {
		ID string `json:"capability_id"`
	}
	var descriptors []struct {
		Entity string               `json:"entity"`
		Fields map[string]fieldSpec `json:"fields"`
	}
	var variables map[string]fieldSpec
	for key, into := range map[string]any{"capabilities": &caps, "evidence_descriptors": &descriptors, "predicate_variables": &variables} {
		if err := json.Unmarshal(top[key], into); err != nil {
			return policy.Vocabulary{}, fail(ErrPack, "a section of the pack does not decode", fmt.Errorf("%s: %w", key, err))
		}
	}
	sc := predicate.Scope{Entities: map[string]map[string]predicate.Type{}, Variables: map[string]predicate.Type{}}
	for _, d := range descriptors {
		if _, dup := sc.Entities[d.Entity]; dup || d.Entity == "" {
			return policy.Vocabulary{}, fail(ErrPack, "an evidence entity is empty or described twice", fmt.Errorf("entity %q", d.Entity))
		}
		sc.Entities[d.Entity] = map[string]predicate.Type{}
		for name, spec := range d.Fields {
			t, err := typeOf(spec)
			if err != nil {
				return policy.Vocabulary{}, fail(ErrPack, "a field of an evidence entity has a type that is not usable", fmt.Errorf("%s.%s: %w", d.Entity, name, err))
			}
			sc.Entities[d.Entity][name] = t
		}
	}
	for name, spec := range variables {
		t, err := typeOf(spec)
		if err != nil {
			return policy.Vocabulary{}, fail(ErrPack, "a predicate variable has a type that is not usable", fmt.Errorf("variable %s: %w", name, err))
		}
		sc.Variables[name] = t
	}
	v := policy.Vocabulary{Scope: sc}
	for _, c := range caps {
		v.Targets = append(v.Targets, c.ID)
	}
	return v, nil
}

var (
	quotedString = regexp.MustCompile(`'[^']*'`)
	wordPath     = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.]*`)
)

// reads is the variables and the entities a condition names. The language has one kind of quotation (single quotes, no escape), so the text inside quotes is dropped, and every
// remaining word that is a declared variable or a declared entity is read. A word that is merely spelled like one is read too: the answer errs toward asking for more, and an
// over-asked fact is refused only when the source cannot give it.
func reads(condition string, sc predicate.Scope) (variables, entities []string) {
	for _, w := range wordPath.FindAllString(quotedString.ReplaceAllString(condition, ""), -1) {
		if _, ok := sc.Variables[w]; ok {
			variables = append(variables, w)
		}
		if _, ok := sc.Entities[w]; ok {
			entities = append(entities, w)
		}
	}
	return variables, entities
}

type packPolicy struct {
	PolicyID string `json:"policy_id"`
	Version  string `json:"version"`
	Rules    []struct {
		AppliesTo []string `json:"applies_to"`
		Condition string   `json:"condition"`
	} `json:"rules"`
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// LoadPolicyBook loads the policy of each pack file given: the current pack, and the older pack versions whose policy a tenant may still have been admitted under (an AT_ADMISSION
// rule is read from the version in force at admission, D8). Every pack must carry one policy with the same policy_id and the same vocabulary, and each version once; anything else is
// ErrPack and no book: the adapters never evaluate half a document. Each policy is checked when it loads (policy.Parse): a rule naming a target that is not a capability, or a condition that does not
// type-check, refuses the pack.
func LoadPolicyBook(packs ...[]byte) (*PolicyBook, error) {
	if len(packs) == 0 {
		return nil, fmt.Errorf("%w: no pack", ErrPack)
	}
	b := &PolicyBook{versions: map[string]policy.Version{}, hashes: map[string]string{}}
	readsOf := map[string]struct{ vars, ents []string }{}
	for i, raw := range packs {
		top, err := topLevel(raw, "policy", "capabilities", "predicate_variables", "evidence_descriptors")
		if err != nil {
			return nil, err
		}
		vocab, err := vocabulary(top)
		if err != nil {
			return nil, err
		}
		doc, err := policy.Parse(top["policy"], vocab)
		if err != nil {
			return nil, fail(ErrPack, "the policy document is not valid", err)
		}
		var rules packPolicy
		if err := json.Unmarshal(top["policy"], &rules); err != nil {
			return nil, fail(ErrPack, "the policy's rules do not decode", err)
		}
		if i == 0 {
			b.policyID, b.scope = doc.PolicyID(), vocab.Scope
		}
		if doc.PolicyID() != b.policyID || !reflect.DeepEqual(vocab.Scope, b.scope) {
			return nil, fmt.Errorf("%w: pack %d has another policy or another vocabulary than the first", ErrPack, i)
		}
		if _, dup := b.versions[doc.Version()]; dup {
			return nil, fail(ErrPack, "a policy version is given twice", fmt.Errorf("version %s", doc.Version()))
		}
		b.versions[doc.Version()] = doc
		if h, err := policy.DocumentHash(top["policy"]); err == nil {
			b.hashes[doc.Version()] = h // a document with no single reading has no hash: no row can rest on it, so a published row for that version is a drift (A413)
		}
		for _, r := range rules.Rules {
			vars, ents := reads(r.Condition, vocab.Scope)
			for _, target := range r.AppliesTo {
				e := readsOf[target]
				e.vars, e.ents = append(e.vars, vars...), append(e.ents, ents...)
				readsOf[target] = e
			}
		}
	}
	b.needs = make(map[string]FactNeed, len(readsOf))
	for target, e := range readsOf {
		b.needs[target] = FactNeed{Variables: sortedUnique(e.vars), Entities: sortedUnique(e.ents)}
	}
	return b, nil
}
