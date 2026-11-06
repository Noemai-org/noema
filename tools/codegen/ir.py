"""The shape of a contract, extracted from its JSON Schema (CON-03, REQ-API-001).

The generator models structure: objects, arrays, maps, enums, constants, references, nullable values and unions. It does
not model validation: patterns, bounds, `if`/`then`/`else`, `not`, `contains`. The JSON Schema validator owns those, at
every boundary. It never guesses: a keyword it does not know, or a combination it does not model, is a GenerationError
that names the schema and the path. Standard library only.
"""
from __future__ import annotations

import re
from dataclasses import dataclass, field
from typing import Any

ID_PREFIX = "https://contracts.noema.invalid/"


class GenerationError(Exception):
    pass


# Keywords that change the shape and are modelled.
SHAPE = {"type", "properties", "required", "additionalProperties", "items", "prefixItems", "$ref", "enum", "const", "oneOf", "anyOf", "allOf"}
# Keywords that only constrain or describe. The validator enforces them; the generated types carry the ones a type system can
# hold (patterns, lengths and bounds in Python) and ignore the rest.
VALIDATION = {"pattern", "minLength", "maxLength", "minimum", "maximum", "exclusiveMinimum", "minItems", "maxItems", "uniqueItems", "minProperties",
              "maxProperties", "propertyNames", "contains", "not", "if", "then", "else", "format"}
META = {"$schema", "$id", "title", "description", "default", "$defs", "examples", "$comment"}


@dataclass
class Prim:
    kind: str  # string, integer, number, boolean, null, any
    nullable: bool = False
    constraints: dict[str, Any] = field(default_factory=dict)


@dataclass
class Const:
    value: Any
    nullable: bool = False


@dataclass
class Enum:
    values: list[Any]
    nullable: bool = False


@dataclass
class Ref:
    schema: str  # file name
    name: str  # name of the target Named type
    nullable: bool = False


@dataclass
class Arr:
    item: Any
    constraints: dict[str, Any] = field(default_factory=dict)
    nullable: bool = False


@dataclass
class MapOf:
    value: Any
    nullable: bool = False


@dataclass
class Field_:
    prop: str
    type: Any
    required: bool


@dataclass
class Obj:
    fields: list[Field_]
    extra: Any = None  # None: closed (additionalProperties: false). Prim("any"): open. Any other type: the type of the other members.
    nullable: bool = False

    @property
    def closed(self) -> bool:
        return self.extra is None


@dataclass
class Union:
    alts: list[Any]
    nullable: bool = False


@dataclass
class Named:
    name: str
    type: Any
    schema: str
    path: str  # "" for a root, "$defs/X" for a definition, "properties/a" for a hoisted type
    doc: str = ""


@dataclass
class Built:
    file: str
    id: str
    root: Named
    named: dict[str, Named]  # every Named of the file by name, root included, in definition order


def pascal(s: str) -> str:
    parts = [p for p in re.split(r"[^0-9A-Za-z]+", s) if p]
    return "".join(p[0].upper() + p[1:] for p in parts)


def root_name(file: str) -> str:
    m = re.match(r"^(?P<n>[a-z0-9-]+)\.v(?P<v>[0-9]+)\.json$", file)
    if not m:
        raise GenerationError(f"{file}: not named <name>.v<major>.json")
    return pascal(m["n"]) + "V" + m["v"]


def is_null(node: Any) -> bool:
    return isinstance(node, dict) and node.get("type") == "null" and set(node) <= {"type", "description"}


class Builder:
    def __init__(self, files: dict[str, dict]):
        self.files = files
        self.ids = {s.get("$id"): f for f, s in files.items()}
        self.named: dict[str, Named] = {}
        self.pending: list[tuple[str, str, str, Ref]] = []
        self.cur = ""
        self.base = ""

    def err(self, path: str, msg: str) -> GenerationError:
        return GenerationError(f"{self.cur} at {path or '/'}: {msg}")

    def resolve(self, ref: str, path: str) -> Ref:
        if ref == "#":
            return Ref(self.cur, self.base)
        if ref.startswith("#/$defs/"):
            f, name = self.cur, ref[len("#/$defs/"):]
        elif ref.startswith(ID_PREFIX):
            url, _, frag = ref.partition("#")
            f = self.ids.get(url)
            if f is None:
                raise self.err(path, f"$ref {ref} names no schema")
            name = frag[len("/$defs/"):] if frag.startswith("/$defs/") else None
            m = re.fullmatch(r"/properties/([A-Za-z0-9_]+)", frag)
            if m:
                # A property of another schema's root. A hoisted property type is named <Root><Property>; whether it exists is
                # checked once every file is built.
                target = Ref(f, root_name(f) + pascal(m[1]))
                self.pending.append((self.cur, path, ref, target))
                return target
            if frag and name is None:
                raise self.err(path, f"$ref {ref}: only #/$defs/<name> and #/properties/<name> fragments are modelled")
            if name is None:
                return Ref(f, root_name(f))
        else:
            raise self.err(path, f"$ref {ref} is not local and not a contract")
        if name not in self.files[f].get("$defs", {}):
            raise self.err(path, f"$ref {ref}: no definition {name}")
        return Ref(f, root_name(f) + name)

    # -- one schema node -> a type ---------------------------------------------------------------------------------
    def node(self, n: Any, path: str, hint: str, hoist: bool = True) -> Any:
        if n is True or n == {}:
            return Prim("any")
        if not isinstance(n, dict):
            raise self.err(path, "a schema must be an object")
        for k in n:
            if k not in SHAPE and k not in VALIDATION and k not in META:
                raise self.err(path, f"keyword {k!r} is not modelled by the generator")
        t = self.shape(n, path, hint)
        if hoist and isinstance(t, (Obj, Enum, Union)):
            name = hint
            if name in self.named:
                raise self.err(path, f"derived name {name} is already taken")
            nullable = t.nullable
            if not isinstance(t, Union):
                t.nullable = False
            self.named[name] = Named(name, t, self.cur, path, n.get("description", ""))
            return Ref(self.cur, name, nullable)
        return t

    def shape(self, n: dict, path: str, hint: str) -> Any:
        if "$ref" in n:
            extra = [k for k in n if k in SHAPE and k != "$ref"]
            if extra:
                raise self.err(path, f"$ref beside {extra} is not modelled")
            return self.resolve(n["$ref"], path)
        if "const" in n:
            return Const(n["const"])
        if "enum" in n:
            values = [v for v in n["enum"] if v is not None]
            return Enum(values, nullable=len(values) != len(n["enum"]))
        for kw in ("oneOf", "anyOf"):
            if kw in n:
                alts = n[kw]
                nullable = any(is_null(a) for a in alts)
                rest = [a for a in alts if not is_null(a)]
                shaped = [a for a in rest if isinstance(a, dict) and any(k in a for k in ("$ref", "type", "const", "enum"))]
                if not shaped:
                    break  # constraint-only alternatives; the node's own shape stands
                if len(shaped) != len(rest):
                    raise self.err(path, f"{kw} mixes shaped and constraint-only alternatives")
                if len(rest) == 1:
                    t = self.node(rest[0], path, hint, hoist=False)
                    t.nullable = nullable or t.nullable
                    return t
                alts = [self.node(a, f"{path}/{kw}/{i}", f"{hint}Option{i + 1}") for i, a in enumerate(rest)]
                if all(isinstance(a, Prim) and a.kind == "string" and not a.nullable for a in alts):
                    return Prim("string", nullable)  # alternatives that differ only in pattern: one string; the validator holds the patterns
                return Union(alts, nullable)
        if "allOf" in n:
            refs = [m for m in n["allOf"] if isinstance(m, dict) and "$ref" in m]
            own = any(k in n for k in ("type", "properties"))
            typed = [m for m in n["allOf"] if isinstance(m, dict) and "type" in m]
            if refs and (own or typed or len(refs) > 1):
                raise self.err(path, "allOf with a $ref beside another shape is not modelled")
            if refs:
                return self.resolve(refs[0]["$ref"], path)
            if typed:
                raise self.err(path, "allOf with a typed member is not modelled")
        types = n.get("type")
        nullable = False
        if isinstance(types, list):
            nullable = "null" in types
            types = [t for t in types if t != "null"]
            if len(types) == 1:
                types = types[0]
            elif not types:
                return Const(None)
        if types is None:
            if "properties" in n:
                types = "object"
            elif "items" in n or "prefixItems" in n:
                types = "array"
            else:
                return Prim("any", nullable)
        if isinstance(types, list):
            if not types:
                return Const(None)
            return Union([Prim(k) for k in types], nullable)  # a scalar union such as ["string", "integer"]
        constraints = {k: n[k] for k in ("pattern", "minLength", "maxLength", "minimum", "maximum", "exclusiveMinimum", "minItems", "maxItems") if k in n}
        if types == "object":
            return self.obj(n, path, hint, nullable)
        if types == "array":
            if "prefixItems" in n and n.get("items", False) is False:
                members = [self.node(m, f"{path}/prefixItems/{i}", f"{hint}Item", hoist=False) for i, m in enumerate(n["prefixItems"])]
                if any(m != members[0] for m in members):
                    raise self.err(path, "prefixItems with different member types is not modelled")
                item = members[0]
            elif "items" in n:
                item = self.node(n["items"], f"{path}/items", f"{hint}Item")
            else:
                item = Prim("any")
            return Arr(item, constraints, nullable)
        if types == "null":
            return Const(None)
        if types in ("string", "integer", "number", "boolean"):
            return Prim(types, nullable, constraints)
        raise self.err(path, f"type {types!r} is not modelled")

    def obj(self, n: dict, path: str, hint: str, nullable: bool) -> Any:
        props = n.get("properties", {})
        add = n.get("additionalProperties", True)
        for r in n.get("required", []):
            if r not in props:
                raise self.err(path, f"required {r!r} is not a declared property")
        if add is False:
            extra = None
        elif isinstance(add, dict):
            extra = self.node(add, f"{path}/additionalProperties", f"{hint}Value")
        else:
            extra = Prim("any")
        if not props:
            return Obj([], None, nullable) if extra is None else MapOf(extra, nullable)
        required = set(n.get("required", []))
        fields = [Field_(p, self.node(v, f"{path}/properties/{p}", hint + pascal(p)), p in required) for p, v in props.items()]
        return Obj(fields, extra, nullable)

    # -- one schema file ---------------------------------------------------------------------------------------------
    def build(self, file: str) -> Built:
        s = self.files[file]
        self.cur = file
        self.base = root_name(file)
        # Definitions first, then the root, so a hoisted name is reserved by its owner.
        order: list[tuple[str, str, dict]] = [(self.base + d, f"$defs/{d}", node) for d, node in s.get("$defs", {}).items()]
        order.append((self.base, "", {k: v for k, v in s.items() if k != "$defs"}))
        for name, path, node in order:
            if name in self.named:
                raise self.err(path, f"name {name} is already taken")
        for name, path, node in order:
            t = self.node(node, path, name, hoist=False)
            self.named[name] = Named(name, t, file, path, node.get("description", ""))
        mine = {k: v for k, v in self.named.items() if v.schema == file}
        # Hoisted types were added while building; keep a stable order: definitions, hoisted by path, root.
        ordered = dict(sorted(mine.items(), key=lambda kv: (kv[0] == self.base, kv[1].path, kv[0])))
        return Built(file, s.get("$id", ""), mine[self.base], ordered)


def build_schemas(files: dict[str, dict]) -> dict[str, Built]:
    b = Builder(files)
    out: dict[str, Built] = {}
    for f in sorted(files):
        out[f] = b.build(f)
    for cur, path, ref, target in b.pending:
        if target.name not in b.named:
            raise GenerationError(f"{cur} at {path or '/'}: $ref {ref} does not name a generated type (the property is not an object, enum or union)")
    return out


def walk_types(t: Any):
    """Every type node under t, t included."""
    yield t
    if isinstance(t, Obj):
        for f in t.fields:
            yield from walk_types(f.type)
        if t.extra is not None:
            yield from walk_types(t.extra)
    elif isinstance(t, Arr):
        yield from walk_types(t.item)
    elif isinstance(t, MapOf):
        yield from walk_types(t.value)
    elif isinstance(t, Union):
        for a in t.alts:
            yield from walk_types(a)


def references(b: Built) -> set[str]:
    """The schema files that b refers to, other than itself."""
    out = set()
    for nm in b.named.values():
        for t in walk_types(nm.type):
            if isinstance(t, Ref) and t.schema != b.file:
                out.add(t.schema)
    return out
