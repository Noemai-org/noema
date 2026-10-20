package predicate

import "sort"

// Kind is the type of a value in the language.
type Kind uint8

// The kinds. Enum is a closed list of upper-case values; Reference is an opaque identifier compared only for equality.
const (
	KindBoolean Kind = iota + 1
	KindString
	KindInteger
	KindTimestamp
	KindDuration
	KindReference
	KindNull
	KindEnum
)

var kindNames = map[Kind]string{KindBoolean: "Boolean", KindString: "String", KindInteger: "Integer", KindTimestamp: "Timestamp",
	KindDuration: "Duration", KindReference: "Reference", KindNull: "Null", KindEnum: "Enum"}

// String returns the name used in the corpus and in error messages.
func (k Kind) String() string { return kindNames[k] }

// MaxInteger is the largest integer the language accepts (2^53-1, the same bound as money and counts in /contracts).
const MaxInteger int64 = 9007199254740991

// Type is a Kind plus, for an Enum, its sorted list of allowed values.
type Type struct {
	Kind   Kind
	Values []string
}

// Primitive types.
var (
	Boolean   = Type{Kind: KindBoolean}
	String    = Type{Kind: KindString}
	Integer   = Type{Kind: KindInteger}
	Timestamp = Type{Kind: KindTimestamp}
	Duration  = Type{Kind: KindDuration}
	Reference = Type{Kind: KindReference}
	Null      = Type{Kind: KindNull}
)

// EnumOf returns an Enum type with its values sorted and de-duplicated.
func EnumOf(values ...string) Type {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return Type{Kind: KindEnum, Values: out}
}

func (t Type) has(value string) bool {
	i := sort.SearchStrings(t.Values, value)
	return i < len(t.Values) && t.Values[i] == value
}

func sameType(a, b Type) bool {
	if a.Kind != b.Kind || len(a.Values) != len(b.Values) {
		return false
	}
	for i := range a.Values {
		if a.Values[i] != b.Values[i] {
			return false
		}
	}
	return true
}

// Value is a runtime value. Timestamps are seconds since the Unix epoch and durations are seconds; both live in I.
// A Reference, a String and an Enum value live in S. The zero Value is null.
type Value struct {
	Kind Kind
	B    bool
	S    string
	I    int64
}

// Constructors for values.
func Bool(b bool) Value            { return Value{Kind: KindBoolean, B: b} }
func Str(s string) Value           { return Value{Kind: KindString, S: s} }
func Int(i int64) Value            { return Value{Kind: KindInteger, I: i} }
func Time(unixSeconds int64) Value { return Value{Kind: KindTimestamp, I: unixSeconds} }
func Dur(seconds int64) Value      { return Value{Kind: KindDuration, I: seconds} }
func Ref(s string) Value           { return Value{Kind: KindReference, S: s} }
func Enum(s string) Value          { return Value{Kind: KindEnum, S: s} }

// IsNull reports whether the value is null (also the zero Value).
func (v Value) IsNull() bool { return v.Kind == 0 || v.Kind == KindNull }

// Row is one evidence record of an entity: field name to value. A missing field is null.
type Row map[string]Value

// Scope is what a predicate is type-checked against: the typed fields of each evidence entity and the typed variables
// (a dotted name such as subject_resource.status is one variable). It is built from a pack's evidence_descriptors and
// predicate_variables.
type Scope struct {
	Entities  map[string]map[string]Type
	Variables map[string]Type
}

// Env is what a predicate is evaluated against. Now is the current time in Unix seconds, read by the caller from its
// clock; this package never reads one. A missing variable is null.
type Env struct {
	Entities  map[string][]Row
	Variables map[string]Value
	Now       int64
}
