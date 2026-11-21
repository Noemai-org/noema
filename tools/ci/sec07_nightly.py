#!/usr/bin/env python3
"""SEC-07 nightly driver: the scanners over every surface, with leaks planted on every surface first (REQ-SEC-003, REQ-SEC-007; PERD 44.1; task SEC-07, done when "scanners run nightly; planted leaks all caught").

  sec07_nightly.py --registry FILE --surfaces FILE --workdir DIR --metric FILE [--allow FILE]     the run: probe, then scan the real surfaces
  sec07_nightly.py --registry FILE --surfaces FILE --workdir DIR --selftest                         the probe alone (no target needed): do the scanners see?

The surfaces are the ones SEC-07 names: logs, traces, API output, explanations and the Record (scopescan.SURFACES, each with the scope it belongs to). For EACH surface the driver first writes, in a work
directory outside every scanned path, one file per planted leak with fresh random values: a canary, a 10-digit account number, a Luhn-valid card number, a value assigned to `api_key`, a JWT-shaped value, a bearer
token, and a token that belongs to a scope the surface may not hold. It runs the very scanners the real scan uses (leakscan for PII, secrets and canaries; scopescan for cross-scope) over them. If any planted leak on
any surface is not reported, the result is NOT RUN (exit 3): a scanner that finds nothing, or whose rules are broken, is never read as clean. Only then are the real surfaces scanned.

Exit: 0 clean and the scanners saw everything planted; 1 a leak or a cross-scope disclosure on a real surface; 2 bad input; 3 NOT RUN (a planted leak was not found, or the scan looked at nothing).
Output is counts and kinds only, never a value; `--metric` writes `noema_cross_scope_disclosure_total{surface}` and `noema_leak_scan_findings_total{surface}` in the Prometheus text format, every surface present, zero included.
All planted values are SYNTHETIC and fresh for each run.
"""
import argparse
import json
import re
import secrets
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import g1_leakscan  # noqa: E402
import leakscan  # noqa: E402
import scopescan  # noqa: E402

LEAK_METRIC = "noema_leak_scan_findings_total"
PROBE_KINDS = ("canary", "account_number", "card_number", "named_secret_value", "jwt", "bearer_token")
PROBE_SURFACE_KINDS = ("api", "explanations", "logs", "record", "traces")  # SEC-07: the five surfaces; a kind that is not configured cannot report zero (Codex P2 on noema#813)


class NotRun(Exception):
    pass


def _line(surface: scopescan.Surface, text: str, scope_for_field: str) -> str:
    item: dict[str, str] = {"msg": "planted", "text": text}
    if surface.scope_field:
        item[surface.scope_field] = scope_for_field
    return json.dumps(item) + "\n"


def _own_scope(surface: scopescan.Surface, registry: scopescan.Registry) -> str:
    if surface.scope != "*":
        return surface.scope
    return next(iter(registry.parent_of))


def _foreign(registry: scopescan.Registry, own: str) -> tuple[str, str]:
    """(scope id, one of its tokens) of a scope that may not share a surface with `own`."""
    for sid in registry.parent_of:
        if not registry.related(sid, own):
            token = next(t for t, owner in registry.owner_of.items() if owner == sid)
            return sid, token
    raise scopescan.InputError("no scope is outside the family of a surface's own scope, so nothing foreign can be planted there")


def probe(surfaces: list[scopescan.Surface], registry: scopescan.Registry, workdir: Path, canary: str) -> list[str]:
    """Plant every leak on every surface and run the real scanners over them. Returns what was NOT found, as `surface: kind` (empty: the scanners see)."""
    missing: list[str] = []
    root = workdir / "probe"
    for index, surface in enumerate(surfaces):
        folder = root / f"{index:02d}-{surface.kind}"
        folder.mkdir(parents=True, exist_ok=True)
        own = _own_scope(surface, registry)
        plants = g1_leakscan.planted_values(canary)
        for kind, text in plants.items():
            (folder / f"planted-{kind}.jsonl").write_text(_line(surface, text, own), encoding="utf-8")
        _foreign_scope, token = _foreign(registry, own)
        (folder / "planted-cross_scope.jsonl").write_text(_line(surface, f"planted for scope {_foreign_scope}: {token}", own), encoding="utf-8")
        found, _files, _lines = leakscan.scan_paths([folder], canaries=(canary,), allow=())
        seen = {(Path(f.path).name, f.kind) for f in found}
        for kind in plants:
            if (f"planted-{kind}.jsonl", kind) not in seen:
                missing.append(f"{surface.name} ({surface.kind}): {kind}")
        probe_surface = scopescan.Surface(surface.name, surface.kind, surface.scope, surface.scope_field, (str(folder / "planted-cross_scope.jsonl"),))
        disclosures, _f, _l = scopescan.scan_surfaces([probe_surface], registry)
        if not disclosures:
            missing.append(f"{surface.name} ({surface.kind}): cross_scope")
    return missing


def _inside(a: Path, b: Path) -> bool:
    a, b = a.resolve(), b.resolve()
    return a == b or a in b.parents or b in a.parents


def render_metrics(per_surface_leaks: dict[str, int], disclosures: list[scopescan.Disclosure]) -> str:
    leak_lines = [f"# HELP {LEAK_METRIC} PII, secret, account-number and canary findings of the SEC-07 scan (must be zero).", f"# TYPE {LEAK_METRIC} counter"]
    leak_lines += [f'{LEAK_METRIC}{{surface="{k}"}} {per_surface_leaks.get(k, 0)}' for k in scopescan.SURFACES]
    return scopescan.render_metric(disclosures) + "\n".join(leak_lines) + "\n"


def run(args: argparse.Namespace) -> int:
    try:
        registry = scopescan.load_registry(args.registry)
        surfaces = scopescan.load_surfaces(args.surfaces, registry)
    except scopescan.InputError as exc:
        print(f"sec07 FAIL: {exc}")
        return 2
    absent = [k for k in PROBE_SURFACE_KINDS if not any(s.kind == k for s in surfaces)]
    if absent:
        print("sec07 FAIL: the surfaces file configures no surface of kind " + ", ".join(absent) + ": a kind that is not scanned cannot report zero")
        return 2
    for s in surfaces:
        for p in s.paths:
            if _inside(args.workdir, Path(p)):
                print("sec07 refused: the work directory must not be, contain or lie inside a scanned path")
                return 2
    allow: tuple = ()
    if args.allow:
        try:
            allow = tuple(re.compile(ln) for ln in leakscan._lines_of(args.allow) if ln.strip())
        except (OSError, UnicodeDecodeError, re.error):
            print("sec07 FAIL: the allow file cannot be read or holds an invalid pattern")
            return 2
        if any(a.search("") for a in allow):
            print("sec07 FAIL: an allow pattern matches the empty string")
            return 2
    canary = "CANARY-" + secrets.token_hex(12)
    args.workdir.mkdir(parents=True, exist_ok=True)
    try:
        missing = probe(surfaces, registry, args.workdir, canary)
    except scopescan.InputError as exc:
        print(f"sec07 FAIL: {exc}")
        return 2
    if missing:
        print("NOT RUN: a planted leak was not reported, so a clean scan would mean nothing: " + "; ".join(missing))
        return 3
    print(f"probe: every planted leak ({', '.join(PROBE_KINDS)}, cross_scope) was found on each of {len(surfaces)} surfaces: the scanners can see")
    if args.selftest:
        return 0
    per_surface: dict[str, int] = {}
    leaks = 0
    files = lines = 0
    unread: list[str] = []
    for s in surfaces:
        found, f, n = leakscan.scan_paths(s.paths, canaries=(), allow=allow)
        files, lines = files + f, lines + n
        if f == 0 or n == 0:
            unread.append(f"{s.name} ({s.kind})")
        per_surface[s.kind] = per_surface.get(s.kind, 0) + len(found)
        leaks += len(found)
        for item in found:
            print(f"sec07 FAIL: {s.name} ({s.kind}): {item}")
    try:
        disclosures, f2, n2 = scopescan.scan_surfaces(surfaces, registry)
    except scopescan.InputError as exc:
        print(f"sec07 FAIL: {exc}")
        return 2
    for d in disclosures:
        print("sec07 FAIL: " + str(d))
    if files == 0 or lines == 0 or f2 == 0:
        print("NOT RUN: the scan looked at nothing; that is never a pass")
        return 3
    if unread:
        print("NOT RUN: a configured surface was absent or empty, so its counter would be a zero that proves nothing: " + "; ".join(unread))
        return 3
    args.metric.write_text(render_metrics(per_surface, disclosures), encoding="utf-8")
    if leaks or disclosures:
        print(f"sec07: {leaks} leak findings, {len(disclosures)} cross-scope disclosures on {len(surfaces)} surfaces")
        return 1
    print(f"sec07 OK: {len(surfaces)} surfaces, {files} files, {lines} lines, 0 leaks, 0 cross-scope disclosures")
    return 0


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--registry", required=True, type=Path)
    ap.add_argument("--surfaces", required=True, type=Path)
    ap.add_argument("--workdir", required=True, type=Path)
    ap.add_argument("--metric", type=Path)
    ap.add_argument("--allow", type=Path)
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args(argv)
    if not args.selftest and args.metric is None:
        print("sec07 FAIL: --metric is required for a real run")
        return 2
    return run(args)


if __name__ == "__main__":
    sys.exit(main())
