"""The coverage report (PERD §48.1 "Coverage rule", REQ-BEN-001): which user stories, supporting stories, edge cases, fault effects,
capabilities and families the suite covers, and every gap. A gap fails the build.

Capabilities are read from the pack at generation time, so a capability added to the pack and not covered is a gap; the story,
edge-case and fault lists are the vendored extract in `catalogue.py`. The report also rejects a coverage tag no list knows (a typo
would otherwise count as coverage nowhere and hide the gap it was meant to close).
"""
import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Mapping

from . import catalogue

ROOT = Path(__file__).resolve().parent.parent.parent
PACK = ROOT / "packs" / "fintech_v1.json"
# B8 (business metrics) is computed across all cases (PERD §48.1), so no case belongs to it alone.
FAMILIES = tuple(f"B{n}" for n in range(1, 8))
MUTATION_KINDS = ("AMBIGUITY", "STATE", "TEMPORAL", "RECOVERY", "FAMILY")
ATTACK_FAMILIES = ("NAME_COLLISION", "INJECTION_IN_NAME", "CANDIDATE_FLOODING", "CROSS_PRINCIPAL_PROBE")
Json = dict[str, Any]


@dataclass(frozen=True)
class PackCapabilities:
    allowed: tuple[str, ...]
    prohibited: tuple[str, ...]


@dataclass
class Report:
    required: dict[str, list[str]]
    covered: dict[str, dict[str, int]]
    gaps: list[str] = field(default_factory=list)


def load_pack_capabilities(path: Path = PACK) -> PackCapabilities:
    capabilities = json.loads(path.read_text(encoding="utf-8"))["capabilities"]
    return PackCapabilities(
        allowed=tuple(sorted(c["capability_id"] for c in capabilities if not c["prohibited"])),
        prohibited=tuple(sorted(c["capability_id"] for c in capabilities if c["prohibited"])),
    )


def report(manifest: Json, pack: PackCapabilities, cases: Mapping[str, Json] | None = None) -> Report:
    required = {
        "US": list(catalogue.USER_STORIES),
        "PUS": list(catalogue.SUPPORTING_STORIES),
        "EC": list(catalogue.EDGE_CASES),
        "FAULT": [*(f"FAULT:{e}" for e in catalogue.FAULT_EFFECTS), "FAULT:NONE"],
        "CAP": [f"CAP:{c}" for c in (*pack.allowed, *pack.prohibited)],
    }
    prefix_of = {"US": "US-", "PUS": "PUS-", "EC": "EC-", "FAULT": "FAULT:", "CAP": "CAP:"}
    seen: dict[str, int] = {}
    for entry in manifest["cases"]:
        for tag in entry["covers"]:
            seen[tag] = seen.get(tag, 0) + 1
    covered: dict[str, dict[str, int]] = {kind: {tag: seen.get(tag, 0) for tag in tags} for kind, tags in required.items()}
    gaps: list[str] = []
    for kind, tags in required.items():
        gaps += [f"{kind}: {tag} is not covered by any case" for tag in tags if not seen.get(tag)]
    known = {tag for tags in required.values() for tag in tags}
    gaps += [f"unknown coverage tag {tag!r} (no list names it)" for tag in sorted(seen) if tag not in known and any(tag.startswith(p) for p in prefix_of.values())]

    quotas = dict(catalogue.CATEGORY_QUOTAS)
    for category, quota in quotas.items():
        have = manifest["category_counts"].get(category, 0)
        if have < quota:
            gaps.append(f"category {category}: {have} cases, the PERD §48.1 table needs {quota}")
    total = len(manifest["cases"])
    if total < sum(quotas.values()):
        gaps.append(f"suite: {total} cases, the release suite has at least {sum(quotas.values())}")
    shared = manifest["pidgin_or_code_switched"]
    if shared["of"] == 0 or shared["count"] * 100 < catalogue.MIN_PIDGIN_OR_CODE_SWITCHED_PERCENT * shared["of"]:
        gaps.append(f"Pidgin or code-switched: {shared['count']} of {shared['of']}, the PERD §48.1 minimum is {catalogue.MIN_PIDGIN_OR_CODE_SWITCHED_PERCENT}%")

    mutations = [e["mutation"] for e in manifest["cases"] if e["mutation"]]
    gaps += [f"mutation kind {k} has no case" for k in MUTATION_KINDS if not any(m["kind"] == k for m in mutations)]
    gaps += [f"attack family {n} has no case" for n in ATTACK_FAMILIES if not any(m["name"] == n for m in mutations)]
    if cases is not None:
        families = {f for c in cases.values() for f in c["families"]}
        gaps += [f"family {f} has no case" for f in FAMILIES if f not in families]
    return Report(required, covered, gaps)


def render(result: Report, manifest: Json) -> str:
    lines = [f"coverage: {len(manifest['cases'])} cases, {len(result.gaps)} gaps"]
    for kind, tags in result.covered.items():
        have = sum(1 for n in tags.values() if n)
        lines.append(f"  {kind}: {have} of {len(tags)} covered")
    lines += [f"  GAP {g}" for g in result.gaps]
    return "\n".join(lines)
