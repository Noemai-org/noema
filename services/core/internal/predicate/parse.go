package predicate

import (
	"strconv"
	"strings"
	"unicode"
)

type tokKind uint8

const (
	tEnd tokKind = iota
	tInt
	tDuration
	tStr
	tOp
	tIdent
	tKeyword
)

type token struct {
	kind tokKind
	text string
}

var keywords = map[string]bool{"and": true, "or": true, "not": true, "in": true, "true": true, "false": true, "null": true,
	"exists": true, "count": true, "where": true, "now": true}

func isIdentStart(c rune) bool { return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }
func isIdentChar(c rune) bool  { return isIdentStart(c) || (c >= '0' && c <= '9') }

func tokenize(src string) ([]token, *Error) {
	rs := []rune(strings.TrimRightFunc(src, unicode.IsSpace))
	var out []token
	for i := 0; i < len(rs); {
		if unicode.IsSpace(rs[i]) {
			i++
			continue
		}
		c := rs[i]
		switch {
		case c >= '0' && c <= '9':
			j := i
			for j < len(rs) && rs[j] >= '0' && rs[j] <= '9' {
				j++
			}
			if j < len(rs) && strings.ContainsRune("smhd", rs[j]) && (j+1 >= len(rs) || !isIdentChar(rs[j+1])) {
				out = append(out, token{tDuration, string(rs[i : j+1])})
				i = j + 1
			} else {
				out = append(out, token{tInt, string(rs[i:j])})
				i = j
			}
		case c == '\'':
			j := i + 1
			for j < len(rs) && rs[j] != '\'' && rs[j] != '\\' {
				j++
			}
			if j >= len(rs) || rs[j] != '\'' {
				return nil, fail(CodeSyntax, "unterminated or escaped string at "+strconv.Itoa(i))
			}
			out = append(out, token{tStr, string(rs[i+1 : j])})
			i = j + 1
		case isIdentStart(c):
			j := i
			for j < len(rs) && isIdentChar(rs[j]) {
				j++
			}
			word := string(rs[i:j])
			if keywords[word] {
				out = append(out, token{tKeyword, word})
			} else {
				out = append(out, token{tIdent, word})
			}
			i = j
		default:
			two := ""
			if i+1 < len(rs) {
				two = string(rs[i : i+2])
			}
			switch {
			case two == "==" || two == "!=" || two == "<=" || two == ">=":
				out = append(out, token{tOp, two})
				i += 2
			case strings.ContainsRune("<>+-()[],.", c):
				out = append(out, token{tOp, string(c)})
				i++
			default:
				return nil, fail(CodeSyntax, "unexpected character at "+strconv.Itoa(i))
			}
		}
	}
	return append(out, token{kind: tEnd}), nil
}

type opKind uint8

const (
	opStr opKind = iota + 1
	opInt
	opDur
	opBool
	opNull
	opNow
	opPath
	opAnd
	opOr
	opNot
	opCmp
	opIn
	opArith
	opExists
	opCount
)

// node is one node of the syntax tree. The tree is immutable after Parse.
type node struct {
	op    opKind
	text  string   // operator, entity name or string literal
	num   int64    // integer or duration seconds
	flag  bool     // boolean literal
	parts []string // path segments
	kids  []*node
	list  []*node // literals of an `in` list
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek(k int) token {
	if p.i+k < len(p.toks) {
		return p.toks[p.i+k]
	}
	return token{kind: tEnd}
}
func (p *parser) is(kind tokKind, text string) bool {
	t := p.peek(0)
	return t.kind == kind && (text == "" || t.text == text)
}
func (p *parser) take(kind tokKind, text string) (token, *Error) {
	t := p.peek(0)
	if t.kind != kind || (text != "" && t.text != text) {
		want := text
		if want == "" {
			want = "a token of another kind"
		}
		return t, fail(CodeSyntax, "expected "+want+", found "+strconv.Quote(t.text))
	}
	p.i++
	return t, nil
}

// Expr is a parsed predicate. It carries no type information until Check or Compile.
type Expr struct{ root *node }

// Parse parses one predicate. It returns *Error with CodeSyntax on any grammar error.
func Parse(src string) (*Expr, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, perr := p.or()
	if perr != nil {
		return nil, perr
	}
	if _, perr = p.take(tEnd, ""); perr != nil {
		return nil, perr
	}
	return &Expr{root: n}, nil
}

func (p *parser) or() (*node, *Error) {
	left, err := p.and()
	for err == nil && p.is(tKeyword, "or") {
		p.i++
		var right *node
		if right, err = p.and(); err == nil {
			left = &node{op: opOr, kids: []*node{left, right}}
		}
	}
	return left, err
}

func (p *parser) and() (*node, *Error) {
	left, err := p.not()
	for err == nil && p.is(tKeyword, "and") {
		p.i++
		var right *node
		if right, err = p.not(); err == nil {
			left = &node{op: opAnd, kids: []*node{left, right}}
		}
	}
	return left, err
}

func (p *parser) not() (*node, *Error) {
	if p.is(tKeyword, "not") && !(p.peek(1).kind == tKeyword && p.peek(1).text == "in") {
		p.i++
		inner, err := p.not()
		if err != nil {
			return nil, err
		}
		return &node{op: opNot, kids: []*node{inner}}, nil
	}
	return p.compare()
}

func (p *parser) compare() (*node, *Error) {
	left, err := p.sum()
	if err != nil {
		return nil, err
	}
	if t := p.peek(0); t.kind == tOp && strings.Contains(" == != < <= > >= ", " "+t.text+" ") {
		p.i++
		right, rerr := p.sum()
		if rerr != nil {
			return nil, rerr
		}
		return &node{op: opCmp, text: t.text, kids: []*node{left, right}}, nil
	}
	negate := false
	if p.is(tKeyword, "not") && p.peek(1).kind == tKeyword && p.peek(1).text == "in" {
		p.i++
		negate = true
	}
	if !p.is(tKeyword, "in") {
		return left, nil
	}
	p.i++
	items, ierr := p.literalList()
	if ierr != nil {
		return nil, ierr
	}
	in := &node{op: opIn, kids: []*node{left}, list: items}
	if negate {
		return &node{op: opNot, kids: []*node{in}}, nil
	}
	return in, nil
}

func (p *parser) literalList() ([]*node, *Error) {
	if _, err := p.take(tOp, "["); err != nil {
		return nil, err
	}
	var items []*node
	for {
		lit, err := p.literal()
		if err != nil {
			return nil, err
		}
		items = append(items, lit)
		if !p.is(tOp, ",") {
			break
		}
		p.i++
	}
	if _, err := p.take(tOp, "]"); err != nil {
		return nil, err
	}
	return items, nil
}

func (p *parser) sum() (*node, *Error) {
	left, err := p.atom()
	for err == nil && (p.is(tOp, "+") || p.is(tOp, "-")) {
		op := p.peek(0).text
		p.i++
		var right *node
		if right, err = p.atom(); err == nil {
			left = &node{op: opArith, text: op, kids: []*node{left, right}}
		}
	}
	return left, err
}

func (p *parser) literal() (*node, *Error) {
	t := p.peek(0)
	switch {
	case t.kind == tStr:
		p.i++
		return &node{op: opStr, text: t.text}, nil
	case t.kind == tInt || t.kind == tDuration:
		return p.number(t)
	case t.kind == tKeyword && (t.text == "true" || t.text == "false"):
		p.i++
		return &node{op: opBool, flag: t.text == "true"}, nil
	case t.kind == tKeyword && t.text == "null":
		p.i++
		return &node{op: opNull}, nil
	}
	return nil, fail(CodeSyntax, "expected a literal, found "+strconv.Quote(t.text))
}

func (p *parser) number(t token) (*node, *Error) {
	p.i++
	digits, mult, op := t.text, int64(1), opInt
	if t.kind == tDuration {
		digits = t.text[:len(t.text)-1]
		mult = map[byte]int64{'s': 1, 'm': 60, 'h': 3600, 'd': 86400}[t.text[len(t.text)-1]]
		op = opDur
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n > MaxInteger/mult {
		return nil, fail(CodeSyntax, "number "+t.text+" is outside 0 to 9007199254740991")
	}
	return &node{op: op, num: n * mult}, nil
}

func (p *parser) atom() (*node, *Error) {
	t := p.peek(0)
	switch {
	case t.kind == tStr, t.kind == tInt, t.kind == tDuration,
		t.kind == tKeyword && (t.text == "true" || t.text == "false" || t.text == "null"):
		return p.literal()
	case t.kind == tOp && t.text == "(":
		p.i++
		inner, err := p.or()
		if err != nil {
			return nil, err
		}
		_, err2 := p.take(tOp, ")")
		return inner, err2
	case t.kind == tKeyword && (t.text == "exists" || t.text == "count"):
		return p.aggregate(t.text)
	case t.kind == tKeyword && t.text == "now":
		p.i++
		if _, err := p.take(tOp, "("); err != nil {
			return nil, err
		}
		_, err := p.take(tOp, ")")
		return &node{op: opNow}, err
	case t.kind == tIdent:
		parts := []string{t.text}
		p.i++
		for p.is(tOp, ".") {
			p.i++
			id, err := p.take(tIdent, "")
			if err != nil {
				return nil, err
			}
			parts = append(parts, id.text)
		}
		return &node{op: opPath, parts: parts}, nil
	}
	return nil, fail(CodeSyntax, "unexpected "+strconv.Quote(t.text))
}

func (p *parser) aggregate(word string) (*node, *Error) {
	p.i++
	if _, err := p.take(tOp, "("); err != nil {
		return nil, err
	}
	entity, err := p.take(tIdent, "")
	if err != nil {
		return nil, err
	}
	if _, err = p.take(tKeyword, "where"); err != nil {
		return nil, err
	}
	cond, cerr := p.or()
	if cerr != nil {
		return nil, cerr
	}
	if _, err = p.take(tOp, ")"); err != nil {
		return nil, err
	}
	op := opExists
	if word == "count" {
		op = opCount
	}
	return &node{op: op, text: entity.text, kids: []*node{cond}}, nil
}
