package predicate

import "strings"

// Check type-checks a parsed predicate against a scope and returns its type. Every error is a *Error with a
// parse-time Code. A predicate that passes cannot fail at evaluation except on a null operand or an overflow.
func Check(e *Expr, scope Scope) (Type, error) {
	t, err := typeOf(e.root, scope, "")
	if err != nil {
		return Type{}, err
	}
	return t, nil
}

func literalType(n *node, other Type) (Type, *Error) {
	switch n.op {
	case opStr:
		if other.Kind == KindEnum && !other.has(n.text) {
			return Type{}, fail(CodeUnknownEnumValue, strings.Join([]string{"'", n.text, "' is not one of [", strings.Join(other.Values, ", "), "]"}, ""))
		}
		return String, nil
	case opInt:
		return Integer, nil
	case opDur:
		return Duration, nil
	case opBool:
		return Boolean, nil
	}
	return Null, nil
}

func compatible(a, b Type) bool {
	switch {
	case a.Kind == KindNull || b.Kind == KindNull:
		return true
	case a.Kind == KindEnum && b.Kind == KindEnum:
		return sameType(a, b)
	case a.Kind == KindEnum:
		return b.Kind == KindString
	case b.Kind == KindEnum:
		return a.Kind == KindString
	case (a.Kind == KindReference && b.Kind == KindString) || (a.Kind == KindString && b.Kind == KindReference):
		return true
	}
	return a.Kind == b.Kind
}

func typeOf(n *node, sc Scope, bound string) (Type, *Error) {
	switch n.op {
	case opStr, opInt, opDur, opBool, opNull:
		return literalType(n, String)
	case opNow:
		return Timestamp, nil
	case opPath:
		return pathType(n.parts, sc, bound)
	case opAnd, opOr, opNot:
		for _, k := range n.kids {
			t, err := typeOf(k, sc, bound)
			if err != nil {
				return Type{}, err
			}
			if t.Kind != KindBoolean {
				return Type{}, fail(CodeTypeMismatch, "and, or and not need Boolean operands")
			}
		}
		return Boolean, nil
	case opExists, opCount:
		return aggregateType(n, sc)
	case opCmp:
		return compareType(n, sc, bound)
	case opIn:
		return inType(n, sc, bound)
	}
	return arithType(n, sc, bound)
}

func pathType(parts []string, sc Scope, bound string) (Type, *Error) {
	dotted := strings.Join(parts, ".")
	if bound != "" && parts[0] == bound {
		if len(parts) != 2 {
			return Type{}, fail(CodeUnknownField, dotted+": an entity field is one name deep")
		}
		t, ok := sc.Entities[bound][parts[1]]
		if !ok {
			return Type{}, fail(CodeUnknownField, bound+" has no field '"+parts[1]+"'")
		}
		return t, nil
	}
	for n := len(parts); n > 0; n-- {
		if t, ok := sc.Variables[strings.Join(parts[:n], ".")]; ok {
			if n != len(parts) {
				return Type{}, fail(CodeUnknownField, dotted+": "+strings.Join(parts[:n], ".")+" has no field '"+parts[n]+"'")
			}
			return t, nil
		}
	}
	if _, ok := sc.Entities[parts[0]]; ok {
		return Type{}, fail(CodeUnknownName, "'"+parts[0]+"' is an entity: use exists(...) or count(...)")
	}
	return Type{}, fail(CodeUnknownName, "'"+dotted+"' is not a declared variable")
}

func aggregateType(n *node, sc Scope) (Type, *Error) {
	if _, ok := sc.Entities[n.text]; !ok {
		return Type{}, fail(CodeUnknownEntity, "'"+n.text+"' has no evidence descriptor")
	}
	t, err := typeOf(n.kids[0], sc, n.text)
	if err != nil {
		return Type{}, err
	}
	if t.Kind != KindBoolean {
		return Type{}, fail(CodeNotBoolean, "the condition of exists(...) and count(...) must be Boolean")
	}
	if n.op == opExists {
		return Boolean, nil
	}
	return Integer, nil
}

func compareType(n *node, sc Scope, bound string) (Type, *Error) {
	l, r := n.kids[0], n.kids[1]
	lt, err := typeOf(l, sc, bound)
	if err != nil {
		return Type{}, err
	}
	rt, err := typeOf(r, sc, bound)
	if err != nil {
		return Type{}, err
	}
	if l.op == opStr && rt.Kind == KindEnum {
		if _, e := literalType(l, rt); e != nil {
			return Type{}, e
		}
	}
	if r.op == opStr && lt.Kind == KindEnum {
		if _, e := literalType(r, lt); e != nil {
			return Type{}, e
		}
	}
	if !compatible(lt, rt) {
		return Type{}, fail(CodeTypeMismatch, "cannot compare "+lt.Kind.String()+" with "+rt.Kind.String())
	}
	ordered := n.text == "<" || n.text == "<=" || n.text == ">" || n.text == ">="
	if ordered && lt.Kind != KindInteger && lt.Kind != KindTimestamp && lt.Kind != KindDuration {
		return Type{}, fail(CodeTypeMismatch, n.text+" needs Integer, Timestamp or Duration")
	}
	return Boolean, nil
}

func inType(n *node, sc Scope, bound string) (Type, *Error) {
	lt, err := typeOf(n.kids[0], sc, bound)
	if err != nil {
		return Type{}, err
	}
	for _, lit := range n.list {
		t, e := literalType(lit, lt)
		if e != nil {
			return Type{}, e
		}
		if !compatible(lt, t) {
			return Type{}, fail(CodeTypeMismatch, "cannot test "+lt.Kind.String()+" against a "+t.Kind.String()+" list")
		}
	}
	return Boolean, nil
}

func arithType(n *node, sc Scope, bound string) (Type, *Error) {
	lt, err := typeOf(n.kids[0], sc, bound)
	if err != nil {
		return Type{}, err
	}
	rt, err := typeOf(n.kids[1], sc, bound)
	if err != nil {
		return Type{}, err
	}
	switch {
	case lt.Kind == rt.Kind && (lt.Kind == KindInteger || lt.Kind == KindDuration):
		return lt, nil
	case lt.Kind == KindTimestamp && rt.Kind == KindDuration:
		return Timestamp, nil
	case lt.Kind == KindTimestamp && rt.Kind == KindTimestamp && n.text == "-":
		return Duration, nil
	}
	return Type{}, fail(CodeTypeMismatch, lt.Kind.String()+" "+n.text+" "+rt.Kind.String()+" is not defined")
}
