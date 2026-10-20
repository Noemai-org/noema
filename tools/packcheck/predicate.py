"""The SoT 43.7 outcome predicate language: parser and type checker (reference implementation in Python).

Boolean logic, comparisons, exists, count, field access on evidence entities and time arithmetic. No loops, no calls
but exists, count and now, no model. The grammar is in contracts/predicates/README.md. The shared corpus
contracts/predicates/tests/corpus.json is run by this module's tests and by the Go evaluator (internal/predicate, T6.1),
so the two cannot disagree on what parses and what type-checks (REQ-VER-002).
"""
from __future__ import annotations

import re
from dataclasses import dataclass

KEYWORDS = {"and", "or", "not", "in", "true", "false", "null", "exists", "count", "where", "now"}
TOKEN = re.compile(r"\s*(?:(?P<duration>\d+[smhd])(?![A-Za-z0-9_])|(?P<int>\d+)|(?P<str>'[^'\\]*')|(?P<op>==|!=|<=|>=|<|>|\+|-|\(|\)|\[|\]|,|\.)|(?P<id>[A-Za-z_][A-Za-z0-9_]*))")

Type = tuple  # ("Boolean",), ("Enum", frozenset), ...
BOOL, STR, INT, TS, DUR, REF, NULL = ("Boolean",), ("String",), ("Integer",), ("Timestamp",), ("Duration",), ("Reference",), ("Null",)


class PredicateError(ValueError):
    def __init__(self, code: str, message: str):
        super().__init__(f"{code}: {message}")
        self.code = code


def field_type(spec: dict) -> Type:
    return ("Enum", frozenset(spec["values"])) if spec["type"] == "Enum" else (spec["type"],)


@dataclass
class Scope:
    entities: dict[str, dict[str, Type]]  # entity -> field -> type
    variables: dict[str, Type]            # name (may be dotted) -> type

    @staticmethod
    def from_pack(pack: dict) -> "Scope":
        ents = {d["entity"]: {f: field_type(s) for f, s in d["fields"].items()} for d in pack.get("evidence_descriptors", [])}
        variables = {n: field_type(s) for n, s in (pack.get("predicate_variables") or {}).items()}
        return Scope(ents, variables)


def tokenize(text: str) -> list[tuple[str, str]]:
    out, pos = [], 0
    text = text.rstrip()
    while pos < len(text):
        m = TOKEN.match(text, pos)
        if not m or m.end() == pos:
            raise PredicateError("SYNTAX", f"unexpected character at {pos}: {text[pos:pos + 10]!r}")
        kind = m.lastgroup
        value = m.group(kind)
        out.append(("kw" if kind == "id" and value in KEYWORDS else kind, value))
        pos = m.end()
    out.append(("end", ""))
    return out


class Parser:
    def __init__(self, text: str):
        self.toks, self.i = tokenize(text), 0

    def peek(self, k=0):
        return self.toks[self.i + k]

    def take(self, kind=None, value=None):
        t = self.toks[self.i]
        if (kind and t[0] != kind) or (value and t[1] != value):
            raise PredicateError("SYNTAX", f"expected {value or kind}, found {t[1] or 'end'!r}")
        self.i += 1
        return t

    def is_(self, kind, value=None):
        t = self.peek()
        return t[0] == kind and (value is None or t[1] == value)

    def parse(self):
        node = self.or_()
        self.take("end")
        return node

    def or_(self):
        node = self.and_()
        while self.is_("kw", "or"):
            self.i += 1
            node = ("or", node, self.and_())
        return node

    def and_(self):
        node = self.not_()
        while self.is_("kw", "and"):
            self.i += 1
            node = ("and", node, self.not_())
        return node

    def not_(self):
        if self.is_("kw", "not") and not (self.peek(1) == ("kw", "in")):
            self.i += 1
            return ("not", self.not_())
        return self.cmp()

    def cmp(self):
        left = self.sum()
        t = self.peek()
        if t[0] == "op" and t[1] in ("==", "!=", "<", "<=", ">", ">="):
            self.i += 1
            return ("cmp", t[1], left, self.sum())
        neg = False
        if self.is_("kw", "not") and self.peek(1) == ("kw", "in"):
            self.i += 1
            neg = True
        if self.is_("kw", "in"):
            self.i += 1
            self.take("op", "[")
            items = [self.literal()]
            while self.is_("op", ","):
                self.i += 1
                items.append(self.literal())
            self.take("op", "]")
            node = ("in", left, items)
            return ("not", node) if neg else node
        return left

    def sum(self):
        node = self.atom()
        while self.peek()[0] == "op" and self.peek()[1] in ("+", "-"):
            op = self.take()[1]
            node = ("arith", op, node, self.atom())
        return node

    def literal(self):
        t = self.peek()
        if t[0] == "str":
            self.i += 1
            return ("str", t[1][1:-1])
        if t[0] == "int":
            self.i += 1
            return ("int", int(t[1]))
        if t[0] == "duration":
            self.i += 1
            return ("dur", int(t[1][:-1]) * {"s": 1, "m": 60, "h": 3600, "d": 86400}[t[1][-1]])
        if t[0] == "kw" and t[1] in ("true", "false"):
            self.i += 1
            return ("bool", t[1] == "true")
        if t[0] == "kw" and t[1] == "null":
            self.i += 1
            return ("null",)
        raise PredicateError("SYNTAX", f"expected a literal, found {t[1] or 'end'!r}")

    def atom(self):
        t = self.peek()
        if t[0] in ("str", "int", "duration") or (t[0] == "kw" and t[1] in ("true", "false", "null")):
            return self.literal()
        if t == ("op", "("):
            self.i += 1
            node = self.or_()
            self.take("op", ")")
            return node
        if t[0] == "kw" and t[1] in ("exists", "count"):
            self.i += 1
            self.take("op", "(")
            entity = self.take("id")[1]
            self.take("kw", "where")
            cond = self.or_()
            self.take("op", ")")
            return (t[1], entity, cond)
        if t == ("kw", "now"):
            self.i += 1
            self.take("op", "(")
            self.take("op", ")")
            return ("now",)
        if t[0] == "id":
            parts = [self.take("id")[1]]
            while self.is_("op", "."):
                self.i += 1
                parts.append(self.take("id")[1])
            return ("path", parts)
        raise PredicateError("SYNTAX", f"unexpected {t[1] or 'end'!r}")


def parse(text: str):
    return Parser(text).parse()


def _compatible(a: Type, b: Type) -> bool:
    if a == NULL or b == NULL:
        return True
    if a[0] == "Enum" and b[0] == "Enum":
        return a[1] == b[1]
    if a[0] == "Enum":
        return b == STR
    if b[0] == "Enum":
        return a == STR
    if {a, b} == {REF, STR}:
        return True
    return a == b


def _literal_type(lit, other: Type) -> Type:
    kind = lit[0]
    if kind == "str":
        if other[0] == "Enum" and lit[1] not in other[1]:
            raise PredicateError("UNKNOWN_ENUM_VALUE", f"{lit[1]!r} is not one of {sorted(other[1])}")
        return STR
    return {"int": INT, "dur": DUR, "bool": BOOL, "null": NULL}[kind]


def typeof(node, scope: Scope, bound: str | None = None) -> Type:
    kind = node[0]
    if kind in ("str", "int", "dur", "bool", "null"):
        return _literal_type(node, STR)
    if kind == "now":
        return TS
    if kind == "path":
        parts = node[1]
        if bound and parts[0] == bound:
            if len(parts) != 2:
                raise PredicateError("UNKNOWN_FIELD", f"{'.'.join(parts)}: an entity field is one name deep")
            fields = scope.entities[bound]
            if parts[1] not in fields:
                raise PredicateError("UNKNOWN_FIELD", f"{bound} has no field {parts[1]!r}")
            return fields[parts[1]]
        for n in range(len(parts), 0, -1):
            name = ".".join(parts[:n])
            if name in scope.variables:
                if n != len(parts):
                    raise PredicateError("UNKNOWN_FIELD", f"{'.'.join(parts)}: {name} has no field {parts[n]!r}")
                return scope.variables[name]
        if parts[0] in scope.entities:
            raise PredicateError("UNKNOWN_NAME", f"{parts[0]!r} is an entity: use exists(...) or count(...)")
        raise PredicateError("UNKNOWN_NAME", f"{'.'.join(parts)!r} is not a declared variable")
    if kind in ("and", "or"):
        for side in node[1:]:
            if typeof(side, scope, bound) != BOOL:
                raise PredicateError("TYPE_MISMATCH", f"{kind} needs Boolean operands")
        return BOOL
    if kind == "not":
        if typeof(node[1], scope, bound) != BOOL:
            raise PredicateError("TYPE_MISMATCH", "not needs a Boolean operand")
        return BOOL
    if kind in ("exists", "count"):
        entity = node[1]
        if entity not in scope.entities:
            raise PredicateError("UNKNOWN_ENTITY", f"{entity!r} has no evidence descriptor")
        if typeof(node[2], scope, entity) != BOOL:
            raise PredicateError("NOT_BOOLEAN", f"the condition of {kind}(...) must be Boolean")
        return BOOL if kind == "exists" else INT
    if kind == "cmp":
        op, l, r = node[1], node[2], node[3]
        lt = typeof(l, scope, bound)
        rt = typeof(r, scope, bound)
        if l[0] == "str" and rt[0] == "Enum":
            _literal_type(l, rt)
        if r[0] == "str" and lt[0] == "Enum":
            _literal_type(r, lt)
        if not _compatible(lt, rt):
            raise PredicateError("TYPE_MISMATCH", f"cannot compare {lt[0]} with {rt[0]}")
        if op in ("<", "<=", ">", ">=") and lt not in (INT, TS, DUR):
            raise PredicateError("TYPE_MISMATCH", f"{op} needs Integer, Timestamp or Duration")
        return BOOL
    if kind == "in":
        lt = typeof(node[1], scope, bound)
        for lit in node[2]:
            t = _literal_type(lit, lt)
            if not _compatible(lt, t):
                raise PredicateError("TYPE_MISMATCH", f"cannot test {lt[0]} against a {t[0]} list")
        return BOOL
    if kind == "arith":
        op, lt, rt = node[1], typeof(node[2], scope, bound), typeof(node[3], scope, bound)
        if lt == rt and lt in (INT, DUR):
            return lt
        if lt == TS and rt == DUR:
            return TS
        if lt == TS and rt == TS and op == "-":
            return DUR
        raise PredicateError("TYPE_MISMATCH", f"{lt[0]} {op} {rt[0]} is not defined")
    raise PredicateError("SYNTAX", f"unknown node {kind}")


def check(text: str, scope: Scope) -> Type:
    """Parse and type-check one predicate. Returns its type, which must be Boolean for every use in a pack."""
    return typeof(parse(text), scope)
