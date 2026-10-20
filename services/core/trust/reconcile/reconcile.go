package reconcile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

type rowRef struct {
	id  string
	row predicate.Row
}

// Reconcile classifies an evidence set. It is pure: the same evidence, rule set and now give the same Result, whatever
// the order of the evidence. It returns an error only for a malformed evidence set or a rule that cannot be evaluated
// (a KNOWN item missing a field the rule reads); the caller treats an error as UNKNOWN and fails closed.
func Reconcile(rs *RuleSet, evidence []Evidence, now int64) (Result, error) {
	items := append([]Evidence(nil), evidence...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	byEntity, unavailable, err := rs.index(items)
	if err != nil {
		return Result{}, err
	}
	contradictions := rs.findContradictions(byEntity)
	env := predicate.Env{Now: now, Variables: map[string]predicate.Value{}, Entities: map[string][]predicate.Row{}}
	for entity, refs := range byEntity {
		for _, r := range refs {
			env.Entities[entity] = append(env.Entities[entity], r.row)
		}
	}
	for _, c := range contradictions {
		env.Entities[entityContradiction] = append(env.Entities[entityContradiction],
			predicate.Row{"label": predicate.Ref(c.Label), "a": predicate.Ref(c.A), "b": predicate.Ref(c.B)})
	}
	env.Entities[entityDuplicate] = rs.findDuplicates(byEntity)
	for _, u := range unavailable {
		env.Entities[entityUnavailable] = append(env.Entities[entityUnavailable], u.row)
	}
	matched := rs.fallback
	for _, r := range rs.rules {
		ok, err := r.prog.EvalBool(env)
		if err != nil {
			return Result{}, fmt.Errorf("rule %s: %w", r.id, err)
		}
		if ok {
			matched = r
			break
		}
	}
	contributing := rs.contributing(matched, items, byEntity, contradictions, unavailable)
	return Result{Classification: matched.classification, RuleID: matched.id, Contributing: contributing,
		Contradictions: contradictions, ValidUntil: validUntil(items, contributing, now)}, nil
}

func (rs *RuleSet) index(items []Evidence) (map[string][]rowRef, []rowRef, error) {
	byEntity := map[string][]rowRef{}
	var unavailable []rowRef
	seen := map[string]bool{}
	for _, it := range items {
		fields, ok := rs.scope.Entities[it.Entity]
		if !ok || it.Entity == entityContradiction || it.Entity == entityDuplicate || it.Entity == entityUnavailable {
			return nil, nil, fmt.Errorf("evidence %s: unknown entity %q", it.ID, it.Entity)
		}
		if seen[it.ID] || it.ID == "" {
			return nil, nil, fmt.Errorf("evidence id %q is empty or repeated", it.ID)
		}
		seen[it.ID] = true
		row := predicate.Row{"status": predicate.Enum(string(it.Status)), "source": predicate.Ref(it.Source),
			"evidence_id": predicate.Ref(it.ID), "effective_at": predicate.Time(it.EffectiveAt), "required": predicate.Bool(it.Required)}
		for name, v := range it.Fields {
			if _, declared := fields[name]; !declared {
				return nil, nil, fmt.Errorf("evidence %s: entity %s has no field %q", it.ID, it.Entity, name)
			}
			row[name] = v
		}
		byEntity[it.Entity] = append(byEntity[it.Entity], rowRef{it.ID, row})
		if it.Status == Unavailable {
			unavailable = append(unavailable, rowRef{it.ID, predicate.Row{"entity": predicate.Ref(it.Entity),
				"required": predicate.Bool(it.Required), "evidence_id": predicate.Ref(it.ID)}})
		}
	}
	return byEntity, unavailable, nil
}

func isKnown(r rowRef) bool { return r.row["status"].S == string(Known) }

func matches(r rowRef, f factRef) bool { return isKnown(r) && r.row[f.Field].S == f.Value }

func anyMatch(refs []rowRef, f factRef) bool {
	for _, r := range refs {
		if matches(r, f) {
			return true
		}
	}
	return false
}

func (rs *RuleSet) findContradictions(byEntity map[string][]rowRef) []Contradiction {
	var out []Contradiction
	for _, sv := range rs.single {
		rows := byEntity[sv.Entity]
		for i := range rows {
			for j := i + 1; j < len(rows); j++ {
				a, b := rows[i], rows[j]
				if isKnown(a) && isKnown(b) && a.row[sv.Field] != b.row[sv.Field] {
					out = append(out, Contradiction{Label: "conflict:" + sv.Entity, A: a.id, B: b.id})
				}
			}
		}
	}
	for _, m := range rs.incompatible {
		if m.Unless != nil && anyMatch(byEntity[m.Unless.Entity], *m.Unless) {
			continue
		}
		for _, a := range byEntity[m.A.Entity] {
			for _, b := range byEntity[m.B.Entity] {
				if matches(a, m.A) && matches(b, m.B) {
					out = append(out, Contradiction{Label: m.Label, A: a.id, B: b.id})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join([]string{out[i].Label, out[i].A, out[i].B}, "\x00") < strings.Join([]string{out[j].Label, out[j].A, out[j].B}, "\x00")
	})
	return out
}

// findDuplicates clusters matching KNOWN rows that agree on the `same` fields and lie within the window of the
// cluster's earliest row. Rows are taken in (time, id) order, so the answer does not depend on input order.
func (rs *RuleSet) findDuplicates(byEntity map[string][]rowRef) []predicate.Row {
	var out []predicate.Row
	for _, d := range rs.derived {
		groups := map[string][]rowRef{}
		for _, r := range byEntity[d.Entity] {
			if !matches(r, d.Require) {
				continue
			}
			parts := make([]string, 0, len(d.Same))
			for _, f := range d.Same {
				v := r.row[f]
				parts = append(parts, fmt.Sprintf("%d:%s:%d", v.Kind, v.S, v.I))
			}
			key := strings.Join(parts, "|")
			groups[key] = append(groups[key], r)
		}
		keys := make([]string, 0, len(groups))
		for k := range groups {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			g := groups[k]
			sort.Slice(g, func(i, j int) bool {
				ti, tj := g[i].row[d.TimeField].I, g[j].row[d.TimeField].I
				return ti < tj || (ti == tj && g[i].id < g[j].id)
			})
			for start := 0; start < len(g); {
				end := start + 1
				for end < len(g) && g[end].row[d.TimeField].I-g[start].row[d.TimeField].I <= d.WithinSeconds {
					end++
				}
				out = append(out, predicate.Row{"size": predicate.Int(int64(end - start)), "key": predicate.Ref(k)})
				start = end
			}
		}
	}
	return out
}

func (rs *RuleSet) contributing(r rule, items []Evidence, byEntity map[string][]rowRef, cs []Contradiction, unavailable []rowRef) []string {
	set := map[string]bool{}
	for _, u := range r.uses {
		switch u {
		case entityContradiction:
			for _, c := range cs {
				set[c.A], set[c.B] = true, true
			}
		case entityUnavailable:
			for _, x := range unavailable {
				if x.row["required"].B {
					set[x.id] = true
				}
			}
		case entityDuplicate:
			for _, ref := range byEntity[rs.derived[0].Entity] {
				if matches(ref, rs.derived[0].Require) {
					set[ref.id] = true
				}
			}
		default:
			for _, ref := range byEntity[u] {
				if isKnown(ref) {
					set[ref.id] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// validUntil is the earliest effective expiry among the contributing evidence (REQ-RES-001). With no contributing
// evidence the answer is not reusable, so it expires now.
func validUntil(items []Evidence, contributing []string, now int64) int64 {
	expiry := map[string]int64{}
	for _, it := range items {
		expiry[it.ID] = it.ExpiresAt
	}
	until := now
	for i, id := range contributing {
		if i == 0 || expiry[id] < until {
			until = expiry[id]
		}
	}
	return until
}
