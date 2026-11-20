package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

// factsDomain separates the facts digest from every other hash in the system: it is written, length-prefixed, before anything else.
const factsDomain = "noema.evidence_item_facts.v1"

// ErrFactsInput is returned by FactsDigest for facts that cannot be digested: none, an empty entity, status, field name or value kind, a kind that is not one of the seven, a value
// that is not in the canonical text of its kind, or two facts of one entity. It is never a digest of what is left.
var ErrFactsInput = errors.New("snapshot: the mapped facts of an evidence item cannot be digested")

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

// FactsDigest is the digest of the mapped, normalized facts of one evidence item (A365, refined): "sha256:" and the SHA-256 of
//
//	LP(domain) LP(decimal number of facts)
//	for each fact in bytewise order of its entity:  LP(entity) LP(status) LP(decimal number of fields)
//	    for each field in bytewise order of its name:  LP(name) LP(kind) LP(text)
//
// where LP is a 4-byte big-endian length and then the bytes (the framing of SetHash). The facts are the rows the evidence mapping gives for the item, the one mapping the Reconciler
// uses; this function never sees the payload, so a field the mapping drops or a spelling it normalizes cannot reach the digest, and a change in a mapped fact always does. Order
// (of the facts and of a map's fields) does not matter. A value is typed: its kind is part of the digest, and its text is canonical for its kind (an integer, a timestamp or a duration
// is the base-10 text of an int64 with no sign but a minus, no leading zero and no "-0"; a boolean is "true" or "false"), so no value has two spellings and none is a float.
//
// No facts, an empty entity, status, field name or kind, a kind outside the seven above, a value that is not canonical, an empty enum or reference, or two facts of one entity is
// ErrFactsInput. A fact may have no field (an item whose status is not KNOWN has none). The input is not modified.
//
// Example: the one fact {debit, KNOWN, state=enum POSTED} digests to the value pinned in facts_digest_test.go.
func FactsDigest(facts []Fact) (string, error) {
	if len(facts) == 0 {
		return "", fmt.Errorf("%w: no fact", ErrFactsInput)
	}
	ordered := append([]Fact(nil), facts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Entity < ordered[j].Entity }) // Go compares strings bytewise, which is UTF-8 order
	for i, f := range ordered {
		if err := checkFact(f); err != nil {
			return "", err
		}
		if i > 0 && ordered[i-1].Entity == f.Entity {
			return "", fmt.Errorf("%w: two facts of one entity", ErrFactsInput)
		}
	}
	h := sha256.New()
	writeLP(h, factsDomain)
	writeLP(h, strconv.Itoa(len(ordered)))
	for _, f := range ordered {
		names := make([]string, 0, len(f.Fields))
		for name := range f.Fields {
			names = append(names, name)
		}
		sort.Strings(names)
		writeLP(h, f.Entity)
		writeLP(h, f.Status)
		writeLP(h, strconv.Itoa(len(names)))
		for _, name := range names {
			v := f.Fields[name]
			writeLP(h, name)
			writeLP(h, v.Kind)
			writeLP(h, v.Text)
		}
	}
	return hashPrefix + hex.EncodeToString(h.Sum(nil)), nil
}

func checkFact(f Fact) error {
	if f.Entity == "" || f.Status == "" {
		return fmt.Errorf("%w: a fact has no entity or no status", ErrFactsInput)
	}
	for name, v := range f.Fields {
		if name == "" || !canonical(v) {
			return fmt.Errorf("%w: a field has no name, or a kind or text that is not canonical", ErrFactsInput)
		}
	}
	return nil
}

// canonical says whether the value's text is the one spelling of a value of its kind.
func canonical(v FactValue) bool {
	switch v.Kind {
	case KindEnum, KindReference:
		return v.Text != ""
	case KindString:
		return true
	case KindBoolean:
		return v.Text == "true" || v.Text == "false"
	case KindInteger, KindTimestamp, KindDuration:
		n, err := strconv.ParseInt(v.Text, 10, 64)
		return err == nil && strconv.FormatInt(n, 10) == v.Text // "+1", "01" and "-0" parse, and do not format back
	}
	return false
}
