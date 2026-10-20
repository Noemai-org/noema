package predicate

// Program is a predicate that has been parsed and type-checked. It is immutable and safe for concurrent use.
type Program struct {
	root *node
	typ  Type
}

// Compile parses and type-checks a predicate against a scope. This is the pack-load step: a predicate that names an
// unknown entity, field or variable, or that mixes types, never becomes a Program.
func Compile(src string, scope Scope) (*Program, error) {
	e, err := Parse(src)
	if err != nil {
		return nil, err
	}
	t, err := Check(e, scope)
	if err != nil {
		return nil, err
	}
	return &Program{root: e.root, typ: t}, nil
}

// Type is the static type of the program's result.
func (p *Program) Type() Type { return p.typ }

// Eval evaluates the program. It is a pure function of the program and the environment: no clock, no I/O, no
// randomness, and the order of the rows in an entity never changes the result. A null operand to and, or, not, an
// ordering or arithmetic, and an arithmetic overflow, return an *Error; the caller treats it as UNKNOWN.
func (p *Program) Eval(env Env) (Value, error) {
	v, err := eval(p.root, env, "", nil)
	if err != nil {
		return Value{}, err
	}
	return v, nil
}

// EvalBool evaluates a program whose type is Boolean.
func (p *Program) EvalBool(env Env) (bool, error) {
	v, err := p.Eval(env)
	if err != nil {
		return false, err
	}
	return v.B, nil
}

func eval(n *node, env Env, bound string, row Row) (Value, *Error) {
	switch n.op {
	case opStr:
		return Str(n.text), nil
	case opInt:
		return Int(n.num), nil
	case opDur:
		return Dur(n.num), nil
	case opBool:
		return Bool(n.flag), nil
	case opNull:
		return Value{}, nil
	case opNow:
		return Time(env.Now), nil
	case opPath:
		return lookup(n.parts, env, bound, row), nil
	case opAnd, opOr:
		return evalLogic(n, env, bound, row)
	case opNot:
		v, err := eval(n.kids[0], env, bound, row)
		if err != nil {
			return Value{}, err
		}
		if v.IsNull() {
			return Value{}, fail(CodeNullOperand, "not of a null value")
		}
		return Bool(!v.B), nil
	case opExists, opCount:
		return evalAggregate(n, env)
	case opCmp:
		return evalCompare(n, env, bound, row)
	case opIn:
		return evalIn(n, env, bound, row)
	}
	return evalArith(n, env, bound, row)
}

func lookup(parts []string, env Env, bound string, row Row) Value {
	if bound != "" && parts[0] == bound {
		return row[parts[1]]
	}
	name := parts[0]
	for _, p := range parts[1:] {
		name += "." + p
	}
	return env.Variables[name]
}

func evalLogic(n *node, env Env, bound string, row Row) (Value, *Error) {
	left, err := eval(n.kids[0], env, bound, row)
	if err != nil {
		return Value{}, err
	}
	if left.IsNull() {
		return Value{}, fail(CodeNullOperand, "a null operand to and or or")
	}
	if (n.op == opAnd && !left.B) || (n.op == opOr && left.B) {
		return left, nil
	}
	right, err := eval(n.kids[1], env, bound, row)
	if err != nil {
		return Value{}, err
	}
	if right.IsNull() {
		return Value{}, fail(CodeNullOperand, "a null operand to and or or")
	}
	return right, nil
}

func evalAggregate(n *node, env Env) (Value, *Error) {
	var count int64
	for _, row := range env.Entities[n.text] {
		v, err := eval(n.kids[0], env, n.text, row)
		if err != nil {
			return Value{}, err
		}
		if v.IsNull() {
			return Value{}, fail(CodeNullOperand, "a null where-condition")
		}
		if v.B {
			count++
			if n.op == opExists {
				return Bool(true), nil
			}
		}
	}
	if n.op == opExists {
		return Bool(false), nil
	}
	return Int(count), nil
}

func equal(a, b Value) bool {
	if a.IsNull() || b.IsNull() {
		return a.IsNull() && b.IsNull()
	}
	switch a.Kind {
	case KindBoolean:
		return b.Kind == KindBoolean && a.B == b.B
	case KindInteger, KindTimestamp, KindDuration:
		return a.Kind == b.Kind && a.I == b.I
	}
	return a.S == b.S // String, Reference and Enum compare by text
}

func evalCompare(n *node, env Env, bound string, row Row) (Value, *Error) {
	l, err := eval(n.kids[0], env, bound, row)
	if err != nil {
		return Value{}, err
	}
	r, err := eval(n.kids[1], env, bound, row)
	if err != nil {
		return Value{}, err
	}
	switch n.text {
	case "==":
		return Bool(equal(l, r)), nil
	case "!=":
		return Bool(!equal(l, r)), nil
	}
	if l.IsNull() || r.IsNull() {
		return Value{}, fail(CodeNullOperand, "a null operand to an ordering")
	}
	switch n.text {
	case "<":
		return Bool(l.I < r.I), nil
	case "<=":
		return Bool(l.I <= r.I), nil
	case ">":
		return Bool(l.I > r.I), nil
	}
	return Bool(l.I >= r.I), nil
}

func evalIn(n *node, env Env, bound string, row Row) (Value, *Error) {
	v, err := eval(n.kids[0], env, bound, row)
	if err != nil {
		return Value{}, err
	}
	for _, lit := range n.list {
		item, err := eval(lit, env, bound, row)
		if err != nil {
			return Value{}, err
		}
		if equal(v, item) {
			return Bool(true), nil
		}
	}
	return Bool(false), nil
}

func evalArith(n *node, env Env, bound string, row Row) (Value, *Error) {
	l, err := eval(n.kids[0], env, bound, row)
	if err != nil {
		return Value{}, err
	}
	r, err := eval(n.kids[1], env, bound, row)
	if err != nil {
		return Value{}, err
	}
	if l.IsNull() || r.IsNull() {
		return Value{}, fail(CodeNullOperand, "a null operand to arithmetic")
	}
	var out int64
	if n.text == "+" {
		out = l.I + r.I
		if (r.I > 0 && out < l.I) || (r.I < 0 && out > l.I) {
			return Value{}, fail(CodeOverflow, "integer overflow")
		}
	} else {
		out = l.I - r.I
		if (r.I > 0 && out > l.I) || (r.I < 0 && out < l.I) {
			return Value{}, fail(CodeOverflow, "integer overflow")
		}
	}
	if out > MaxInteger || out < -MaxInteger {
		return Value{}, fail(CodeOverflow, "result outside the safe integer range")
	}
	kind := l.Kind
	if l.Kind == KindTimestamp && r.Kind == KindTimestamp {
		kind = KindDuration
	}
	return Value{Kind: kind, I: out}, nil
}
