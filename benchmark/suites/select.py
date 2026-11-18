"""Named benchmark suites as data, and the pure selection of their cases (BEN-29; REQ-BEN-004, REQ-TST-005).

A suite is a selector over the suite manifest (and, for families, the case files). Selection is a pure function: the same manifest gives the same ids in the same order.
A selector key nobody knows, an empty cell (`each_must_match`), an empty selection or a suite below `min_cases` is a SelectionError, never a quiet pass.
Generated layer only: nothing here reads `benchmark-heldout` (INV-22, REQ-AGT-004). SYNTHETIC."""
import hashlib
import json
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any

Json = dict[str, Any]
SELECTOR_KEYS = frozenset({"all", "covers_any", "covers_prefix", "except_covers", "families_any", "categories_any", "each_must_match", "greedy_cover"})
MATCHERS = frozenset({"all", "covers_any", "covers_prefix", "families_any", "categories_any", "greedy_cover"})


class SelectionError(Exception):
    pass


def load_suites(path: Path) -> dict[str, Json]:
    document = json.loads(path.read_text(encoding="utf-8"))
    suites = document.get("suites") if isinstance(document, dict) else None
    if not isinstance(suites, dict) or not suites:
        raise SelectionError(f"{path} defines no suites")
    return dict(suites)


def load_families(suite_root: Path) -> dict[str, list[str]]:
    """case id to its families, from the case files of a generated suite root."""
    found: dict[str, list[str]] = {}
    for case_file in sorted((suite_root / "cases").glob("*.json")):
        case = json.loads(case_file.read_text(encoding="utf-8"))
        found[case["case_id"]] = list(case["families"])
    return found


def _matches(entry: Json, selector: Json, families: Mapping[str, Sequence[str]] | None) -> bool:
    covers = set(entry["covers"])
    if selector.get("all"):
        return True
    if "covers_any" in selector and covers & set(selector["covers_any"]):
        return True
    if "covers_prefix" in selector:
        prefix, skip = selector["covers_prefix"], set(selector.get("except_covers", []))
        if any(c.startswith(prefix) and c not in skip for c in covers):
            return True
    if "categories_any" in selector and entry["category"] in selector["categories_any"]:
        return True
    if "families_any" in selector:
        if families is None:
            raise SelectionError("a families selector needs the case families (load_families), and none were given")
        if set(families.get(entry["case_id"], ())) & set(selector["families_any"]):
            return True
    return False


def _greedy(entries: Sequence[Json], rule: Any, name: str) -> list[str]:
    """TST-02: the fewest cases that touch the most identifiers, chosen by a rule, not typed by hand. `rule` is {"n": int, "stages": [[tag prefix, ...], ...]}. For each stage in turn the case
    that adds the most tags of that stage not yet covered is picked (`FAULT:NONE` never counts); ties go to the category with the fewest picks so far, then to the lowest case id. Each stage runs until it adds no
    tag of its prefixes, so a later stage never revisits an earlier one. It stops at `n` cases, or when nothing adds a tag. The result is sorted, so the manifest's order does not matter."""
    if not isinstance(rule, dict) or set(rule) != {"n", "stages"}:
        raise SelectionError(f"suite {name}: greedy_cover needs exactly n and stages")
    n, stages = rule["n"], rule["stages"]
    if type(n) is not int or n < 1:
        raise SelectionError(f"suite {name}: greedy_cover n must be a positive integer, not {n!r}")
    if not isinstance(stages, list) or not stages or not all(isinstance(s, list) and s and all(isinstance(p, str) and p for p in s) for s in stages):
        raise SelectionError(f"suite {name}: greedy_cover stages must be a non-empty list of non-empty lists of prefixes")
    every = [p for stage in stages for p in stage]  # the tags that count towards a pick's category bookkeeping and later stages' coverage

    def tags(entry: Json, prefixes: Sequence[str]) -> set[str]:
        return {c for c in entry["covers"] if c != "FAULT:NONE" and c.startswith(tuple(prefixes))}

    pool = sorted(entries, key=lambda e: e["case_id"])
    picked: list[Json] = []
    covered: set[str] = set()
    per_category: dict[str, int] = {}
    for prefixes in stages:
        while len(picked) < n:
            best = min(pool, key=lambda e: (-len(tags(e, prefixes) - covered), per_category.get(e["category"], 0), e["case_id"]), default=None)
            if best is None or not tags(best, prefixes) - covered:
                break
            picked.append(best)
            pool.remove(best)
            covered |= tags(best, every)
            per_category[best["category"]] = per_category.get(best["category"], 0) + 1
    return sorted(e["case_id"] for e in picked)


def select(manifest: Json, definition: Json, name: str, families: Mapping[str, Sequence[str]] | None = None) -> list[str]:
    selector = definition.get("select")
    if not isinstance(selector, dict):
        raise SelectionError(f"suite {name}: no selector")
    unknown = sorted(set(selector) - SELECTOR_KEYS)
    if unknown:
        raise SelectionError(f"suite {name}: unknown selector key {', '.join(unknown)}")
    if not set(selector) & MATCHERS:
        raise SelectionError(f"suite {name}: the selector names no way to match a case ({sorted(MATCHERS)})")
    if "greedy_cover" in selector:
        if set(selector) != {"greedy_cover"}:
            raise SelectionError(f"suite {name}: greedy_cover cannot be combined with another selector key")
        chosen = [e for e in manifest["cases"] if e["case_id"] in set(_greedy(manifest["cases"], selector["greedy_cover"], name))]
    else:
        chosen = [e for e in manifest["cases"] if _matches(e, selector, families)]
    ids = sorted(e["case_id"] for e in chosen)
    if not ids:
        raise SelectionError(f"suite {name} selects no case")
    for tag in selector.get("each_must_match", []):
        if not any(tag in e["covers"] for e in chosen):
            raise SelectionError(f"suite {name}: no selected case covers {tag} (an empty cell)")
    minimum = definition.get("min_cases", 1)
    if len(ids) < minimum:
        raise SelectionError(f"suite {name} selects {len(ids)} cases, fewer than its minimum of {minimum}")
    return ids


def selection_digest(case_ids: Sequence[str]) -> str:
    return "sha256:" + hashlib.sha256("\n".join(case_ids).encode("utf-8")).hexdigest()
