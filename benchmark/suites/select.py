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
SELECTOR_KEYS = frozenset({"all", "covers_any", "covers_prefix", "except_covers", "families_any", "categories_any", "each_must_match"})
MATCHERS = frozenset({"all", "covers_any", "covers_prefix", "families_any", "categories_any"})


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


def select(manifest: Json, definition: Json, name: str, families: Mapping[str, Sequence[str]] | None = None) -> list[str]:
    selector = definition.get("select")
    if not isinstance(selector, dict):
        raise SelectionError(f"suite {name}: no selector")
    unknown = sorted(set(selector) - SELECTOR_KEYS)
    if unknown:
        raise SelectionError(f"suite {name}: unknown selector key {', '.join(unknown)}")
    if not set(selector) & MATCHERS:
        raise SelectionError(f"suite {name}: the selector names no way to match a case ({sorted(MATCHERS)})")
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
