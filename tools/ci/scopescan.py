#!/usr/bin/env python3
"""Cross-scope disclosure scanner (SEC-07; REQ-SEC-003, REQ-SEC-004, PERD section 44.1: `noema_cross_scope_disclosure_total`, a must-be-zero counter fed by the leak scanner).

A cross-scope disclosure is a value that belongs to one scope (a tenant, or one principal of a tenant) appearing on a surface that belongs to another: tenant A's marker in tenant B's API answer,
one customer's reference in another customer's explanation, tenant B's id in tenant A's Record export. The scanner finds them by TOKEN: the run plants a distinct, high-entropy marker for each scope
(and knows each scope's ids), and a surface of scope S may hold the tokens of S and of the scopes S is nested with (its tenant, its principals), and of nothing else.

  registry.json  {"scopes": [{"id": "ten_alpha", "tokens": ["ten_alpha", "MARK-..."]},
                             {"id": "usr_a1", "parent": "ten_alpha", "tokens": ["usr_a1", ...]}, ...]}
  surfaces.json  {"surfaces": [{"name": "api-alpha", "kind": "api", "scope": "ten_alpha", "paths": ["out/api/alpha"]},
                               {"name": "core-log", "kind": "logs", "scope": "*", "scope_field": "tenant_id", "paths": ["out/logs/core.log"]}]}

`kind` is one of SURFACES (the metric's label: an enum, never an id). `scope` is the scope id of the surface, or `*` for a stream of several scopes whose lines name their own scope in the JSON
member `scope_field`: a line is judged by that member; a line with no usable scope member holds at most one scope's tokens (two scopes in one unscoped line is a disclosure). A token must belong to exactly one scope and be at
least MIN_TOKEN characters (a short token matches by accident), at least two scopes are needed (one scope cannot leak), and every surface scanned must name a scope the registry knows.

It NEVER prints a token: only the surface kind and name, the file, the line and a fingerprint (an HMAC under a key made for the run). A scan that looked at nothing (no surface, no file, no line, no
token) is exit 2, never a pass. `render_metric` writes the counter in the Prometheus text format with every surface kind present (zero is a line, not an absence).

Library use (sec07_nightly.py): `load_registry`, `load_surfaces`, `scan_surfaces`, `render_metric`. Command: scopescan.py --registry FILE --surfaces FILE [--metric FILE]. Exit 0 clean, 1 disclosure, 2 bad input or nothing scanned.
"""
import argparse
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent))
import leakscan  # noqa: E402

SURFACES = ("api", "explanations", "logs", "other", "record", "traces")
METRIC = "noema_cross_scope_disclosure_total"
MIN_TOKEN = 8
SCOPE_ID = re.compile(r"[A-Za-z0-9_.:@-]{1,64}")


class InputError(Exception):
    """A registry or a surfaces file that cannot be used. The message never holds a token."""


@dataclass(frozen=True)
class Surface:
    name: str
    kind: str
    scope: str
    scope_field: str | None
    paths: tuple[str, ...]


@dataclass(frozen=True)
class Disclosure:
    kind: str  # the surface kind
    surface: str
    path: str
    line: int
    owner: str  # the scope whose token appeared
    fingerprint: str

    def __str__(self) -> str:
        return f"{self.surface} ({self.kind}) {self.path}:{self.line}: a token of scope {self.owner} on another scope's surface (fingerprint {self.fingerprint})"


@dataclass(frozen=True)
class Registry:
    owner_of: dict[str, str]
    parent_of: dict[str, str | None]
    pattern: re.Pattern[str]

    def related(self, a: str, b: str) -> bool:
        """Two scopes may share a surface when they are the same or one is nested in the other (a tenant and its principals)."""
        return a == b or b in self.ancestors(a) or a in self.ancestors(b)

    def ancestors(self, scope: str) -> set[str]:
        out: set[str] = set()
        cur = self.parent_of.get(scope)
        while cur is not None and cur not in out:
            out.add(cur)
            cur = self.parent_of.get(cur)
        return out


def _document(path: Path, what: str) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError, UnicodeDecodeError):
        raise InputError(f"the {what} file cannot be read as JSON") from None


def load_registry(path: Path) -> Registry:
    doc = _document(path, "registry")
    scopes = doc.get("scopes") if isinstance(doc, dict) else None
    if not isinstance(scopes, list) or len(scopes) < 2:
        raise InputError("the registry needs at least two scopes: one scope cannot leak")
    owner_of: dict[str, str] = {}
    parent_of: dict[str, str | None] = {}
    for s in scopes:
        sid = s.get("id") if isinstance(s, dict) else None
        if not isinstance(sid, str) or not SCOPE_ID.fullmatch(sid) or sid in parent_of:
            raise InputError("a scope has no id, an id off its shape, or an id used twice")
        parent = s.get("parent")
        if parent is not None and not isinstance(parent, str):
            raise InputError("a scope's parent is not a scope id")
        parent_of[sid] = parent
        tokens = s.get("tokens")
        if not isinstance(tokens, list) or not tokens:
            raise InputError("a scope has no token: it cannot be looked for")
        for t in tokens:
            if not isinstance(t, str) or len(t) < MIN_TOKEN or t != t.strip() or "\n" in t:
                raise InputError(f"a token of scope {sid} is not a string of at least {MIN_TOKEN} characters with no padding or newline")
            if owner_of.setdefault(t, sid) != sid:
                raise InputError("a token belongs to two scopes: it proves nothing")
    for sid, parent in parent_of.items():
        if parent is not None and parent not in parent_of:
            raise InputError("a scope names a parent the registry does not hold")
    ordered = sorted(owner_of, key=len, reverse=True)
    pattern = re.compile(r"(?<![A-Za-z0-9_])(?:" + "|".join(re.escape(t) for t in ordered) + r")(?![A-Za-z0-9_])")
    return Registry(owner_of, parent_of, pattern)


def load_surfaces(path: Path, registry: Registry) -> list[Surface]:
    doc = _document(path, "surfaces")
    items = doc.get("surfaces") if isinstance(doc, dict) else None
    if not isinstance(items, list) or not items:
        raise InputError("there is no surface to scan")
    out: list[Surface] = []
    for s in items:
        if not isinstance(s, dict):
            raise InputError("a surface is not an object")
        name, kind, scope, paths = s.get("name"), s.get("kind"), s.get("scope"), s.get("paths")
        field = s.get("scope_field")
        if not isinstance(name, str) or not name or kind not in SURFACES:
            raise InputError(f"a surface has no name or a kind outside {SURFACES}")
        if scope != "*" and scope not in registry.parent_of:
            raise InputError(f"surface {name}: its scope is neither `*` nor a scope of the registry")
        if scope == "*" and not isinstance(field, str):
            raise InputError(f"surface {name}: a `*` surface needs the `scope_field` that names the scope of each line")
        if scope != "*" and field is not None:
            raise InputError(f"surface {name}: `scope_field` is for `*` surfaces only")
        if not isinstance(paths, list) or not paths or not all(isinstance(p, str) and p for p in paths):
            raise InputError(f"surface {name}: no path")
        out.append(Surface(name, kind, scope, field if isinstance(field, str) else None, tuple(paths)))
    return out


def _line_scope(line: str, surface: Surface, registry: Registry) -> str | None:
    if surface.scope != "*":
        return surface.scope
    try:
        item = json.loads(line)
    except ValueError:
        return None
    value = item.get(surface.scope_field) if isinstance(item, dict) else None
    return value if isinstance(value, str) and value in registry.parent_of else None


def scan_text(text: str, surface: Surface, registry: Registry, path: str) -> list[Disclosure]:
    found: list[Disclosure] = []
    for number, line in enumerate(text.splitlines(), 1):
        owners = {registry.owner_of[m.group(0)]: m.group(0) for m in registry.pattern.finditer(line)}
        if not owners:
            continue
        here = _line_scope(line, surface, registry)
        if here is not None:
            wrong = {o: t for o, t in owners.items() if not registry.related(o, here)}
        else:  # an unscoped line may hold one scope's tokens, not two
            ids = sorted(owners)
            wrong = {a: owners[a] for a in ids if any(not registry.related(a, b) for b in ids if b != a)}
        for owner, token in sorted(wrong.items()):
            found.append(Disclosure(surface.kind, surface.name, path, number, owner, leakscan._fp(token)))
    return found


def scan_surfaces(surfaces: list[Surface], registry: Registry) -> tuple[list[Disclosure], int, int]:
    """Returns (disclosures, files scanned, lines scanned). An unreadable or undecodable file is an InputError: it could not be searched, so it cannot be called clean."""
    found: list[Disclosure] = []
    n_files = n_lines = 0
    for s in surfaces:
        for root in s.paths:
            problems: list[leakscan.Finding] = []
            files = leakscan._files_of(Path(root), problems)
            if problems:
                raise InputError(f"surface {s.name}: a path is missing, a link, or cannot be listed")
            for f in files:
                try:
                    data = f.read_bytes()
                except OSError:
                    raise InputError(f"surface {s.name}: a file cannot be read") from None
                text = leakscan._decode(data)
                if text is None:
                    raise InputError(f"surface {s.name}: a file is binary or not UTF-8 text, so it cannot be searched")
                n_files += 1
                n_lines += len(text.splitlines())
                found.extend(scan_text(text, s, registry, str(f)))
    return found, n_files, n_lines


def render_metric(found: list[Disclosure]) -> str:
    counts = {k: 0 for k in SURFACES}
    for d in found:
        counts[d.kind] += 1
    lines = [f"# HELP {METRIC} Values of one scope found on another scope's surface by the SEC-07 scan (must be zero).", f"# TYPE {METRIC} counter"]
    lines += [f'{METRIC}{{surface="{k}"}} {counts[k]}' for k in SURFACES]
    return "\n".join(lines) + "\n"


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--registry", required=True, type=Path)
    ap.add_argument("--surfaces", required=True, type=Path)
    ap.add_argument("--metric", type=Path, help="write the counter in the Prometheus text format")
    args = ap.parse_args(argv)
    try:
        registry = load_registry(args.registry)
        surfaces = load_surfaces(args.surfaces, registry)
        found, n_files, n_lines = scan_surfaces(surfaces, registry)
    except InputError as exc:
        print(f"scopescan FAIL: {exc}")
        return 2
    if n_files == 0 or n_lines == 0:
        print(f"scopescan FAIL: scanned {n_files} files, {n_lines} lines: a scan that looked at nothing is not a pass")
        return 2
    if args.metric:
        args.metric.write_text(render_metric(found), encoding="utf-8")
    for d in found:
        print("scopescan FAIL: " + str(d))
    if found:
        return 1
    print(f"scopescan OK: {len(surfaces)} surfaces, {n_files} files, {n_lines} lines, 0 cross-scope disclosures")
    return 0


if __name__ == "__main__":
    sys.exit(main())
