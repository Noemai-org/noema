"""Plan and produce the generated suite (layer A of PERD §48.1): cases, oracle documents, fixtures, fault plans and the manifest.

The plan is a pure function of (generator version, seed): per category, every archetype gets a base case, the remaining quota
is filled with further variants of the archetypes, and a share of the quota is mutated cases (PERD §48.4). Base cases are
numbered from 0001, mutated cases from 1001. Nothing here reads `benchmark-heldout` (INV-22, REQ-AGT-004).
"""
import json
from dataclasses import dataclass, replace
from pathlib import Path
from typing import Any

from benchmark.contracts import pretty_bytes, sha256_of

from .archetype import Arc, Instance, instantiate
from .catalogue import CATEGORY_CODES, CATEGORY_QUOTAS
from .library import BY_CATEGORY
from .mutations import MUTATIONS, Mutation

GENERATOR_NAME = "noema-casegen"
GENERATOR_VERSION = "1.0.0"
SUITE_ID = "generated-v1"
SUITE_VERSION = "1.0.0"
DEFAULT_SEED = 20261020
MUTATED_SHARE_PERCENT = 30
FIRST_MUTATED = 1001
ROOT = Path(__file__).resolve().parent.parent.parent
PACK = ROOT / "packs" / "fintech_v1.json"
Json = dict[str, Any]


@dataclass(frozen=True)
class Planned:
    category: str
    number: int
    arc: Arc
    variant: int
    mutation: Mutation | None
    base_key: str

    @property
    def case_id(self) -> str:
        return f"BEN-{CATEGORY_CODES[self.category]}-{self.number:04d}"


@dataclass
class Suite:
    seed: int
    cases: dict[str, Json]
    oracles: dict[str, Json]
    fixtures: dict[str, Json]
    manifest: Json
    files: dict[str, bytes]


def _candidates(category: str) -> list[tuple[Mutation, Arc]]:
    """Mutations whose mutated case belongs to `category`, interleaved so that every mutation is used before any is repeated."""
    pool = [a for arcs in BY_CATEGORY.values() for a in arcs]
    per_mutation = [sorted((a for a in pool if m.applies(a) and (m.category or a.cat) == category), key=lambda a: a.key) for m in MUTATIONS]
    depth = max((len(p) for p in per_mutation), default=0)
    return [(MUTATIONS[i], per_mutation[i][j]) for j in range(depth) for i in range(len(MUTATIONS)) if j < len(per_mutation[i])]


def plan() -> list[Planned]:
    planned: list[Planned] = []
    for category, quota in CATEGORY_QUOTAS:
        arcs = BY_CATEGORY[category]
        if len(arcs) > quota:
            raise ValueError(f"{category}: {len(arcs)} archetypes do not fit a quota of {quota}")
        mutated = _candidates(category)[: min(round(quota * MUTATED_SHARE_PERCENT / 100), quota - len(arcs))]
        for j in range(quota - len(mutated)):
            planned.append(Planned(category, j + 1, arcs[j % len(arcs)], j // len(arcs), None, arcs[j % len(arcs)].key))
        for i, (mutation, arc) in enumerate(mutated):
            planned.append(Planned(category, FIRST_MUTATED + i, mutation.apply(arc), 0, mutation, arc.key))
    return planned


def oracle_path(p: Planned) -> str:
    """oracle/<category code>/<case id>_v<version>.json, the form of the PERD §48.2 example (lower case, as the schema requires)."""
    return f"oracle/{CATEGORY_CODES[p.category].lower()}/{p.case_id.lower().replace('-', '_')}_v1.json"


def _unique(items: tuple[str, ...] | list[str]) -> list[str]:
    return list(dict.fromkeys(items))


def _pack_versions() -> tuple[str, str]:
    pack = json.loads(PACK.read_text(encoding="utf-8"))
    return f"{pack['pack_id']}@{pack['pack_version']}", f"{pack['policy']['policy_id']}@{pack['policy']['version']}"


def _fault_plan(case_id: str, inst: Instance) -> Json:
    return {
        "plan_id": f"fp_{case_id.lower().replace('-', '_')}", "synthetic": True, "case_id": case_id,
        "provisional": "PERD §37.1 shape; the fault-plan schema is owned by the environment track and replaces this file's format when it lands",
        "faults": inst.faults, "harness_effects": list(inst.arc.harness),
    }


def build_case(p: Planned, seed: int, pack_version: str, policy_version: str) -> tuple[Json, Json, Json, Json | None, list[str]]:
    inst = instantiate(p.arc, seed, p.variant)
    fixture = json.loads(json.dumps(inst.world.fixture))
    fixture["fixture_id"] = f"fx_{CATEGORY_CODES[p.category].lower()}_{p.number:04d}"
    fixture["principals"][0]["roles"] = list(p.arc.roles)
    plan_doc = _fault_plan(p.case_id, inst) if inst.faults or p.arc.harness else None
    case: Json = {
        "case_id": p.case_id, "version": 1, "suite_layer": "GENERATED",
        "generator": {"name": GENERATOR_NAME, "version": GENERATOR_VERSION, "seed": seed},
        "families": _unique(inst.families), "category": p.category, "locale_mix": inst.locale_mix, "channel": inst.channel,
        "principal": {"principal_id": inst.world.principal, "roles": list(p.arc.roles)},
        "goal": inst.goal, "utterances": [{"turn": i, "text": t} for i, t in enumerate(inst.utterances, start=1)],
        "known_facts_fixture": f"fixtures/{fixture['fixture_id']}.json",
        "events_timeline": [{"at": at, "event": event} for at, event in inst.timeline],
        "fault_plan": plan_doc["plan_id"] if plan_doc else "fp_none",
        "policy_version": policy_version, "pack_version": pack_version, "permitted_capabilities": inst.permitted,
        "expected": inst.expected, "oracle_ref": oracle_path(p),
        "safety_gate_relevance": _unique(p.arc.gates), "severity_if_failed": p.arc.sev, "provenance": "generated",
    }
    oracle: Json = {"case_id": p.case_id, "case_version": 1, **inst.oracle}
    return case, oracle, fixture, plan_doc, sorted(inst.covers)


def generate(seed: int = DEFAULT_SEED) -> Suite:
    pack_version, policy_version = _pack_versions()
    cases: dict[str, Json] = {}
    oracles: dict[str, Json] = {}
    fixtures: dict[str, Json] = {}
    files: dict[str, bytes] = {}
    entries: list[Json] = []
    base_ids: dict[str, str] = {}
    planned = plan()
    for p in planned:
        if p.mutation is None:
            base_ids.setdefault(p.arc.key, p.case_id)  # the first (variant 0) base case of an archetype
    for p in planned:
        case, oracle, fixture, fault_plan, covers = build_case(p, seed, pack_version, policy_version)
        cases[p.case_id], oracles[p.case_id] = case, oracle
        fixture_path = case["known_facts_fixture"]
        fixtures[fixture_path] = fixture
        files[f"cases/{p.case_id}.json"] = pretty_bytes(case)
        files[case["oracle_ref"]] = pretty_bytes(oracle)
        files[fixture_path] = pretty_bytes(fixture)
        if fault_plan:
            files[f"fault_plans/{fault_plan['plan_id']}.json"] = pretty_bytes(fault_plan)
        entries.append({
            "case_id": p.case_id, "version": 1, "suite_layer": "GENERATED", "category": p.category, "sha256": sha256_of(case), "covers": covers,
            "base_case_id": base_ids[p.base_key] if p.mutation else None,
            "mutation": {"kind": p.mutation.kind, "name": p.mutation.name} if p.mutation else None,
        })
    entries.sort(key=lambda e: e["case_id"])
    shared = sum(1 for c in cases.values() if "pcm" in c["locale_mix"] or len(c["locale_mix"]) > 1)
    manifest: Json = {
        "suite_id": SUITE_ID, "suite_version": SUITE_VERSION,
        "generator": {"name": GENERATOR_NAME, "version": GENERATOR_VERSION, "seed": seed},
        "layer_counts": {"GENERATED": len(cases), "HELD_OUT": 0, "PARTNER": 0},
        "category_counts": {category: sum(1 for c in cases.values() if c["category"] == category) for category, _ in CATEGORY_QUOTAS},
        "pidgin_or_code_switched": {"count": shared, "of": len(cases)}, "cases": entries,
    }
    files["manifest.json"] = pretty_bytes(manifest)
    return Suite(seed, cases, oracles, fixtures, manifest, dict(sorted(files.items())))


def write(suite: Suite, out: Path) -> None:
    """Write the suite under `out` (the suite root: cases/, oracle/, fixtures/, fault_plans/, manifest.json)."""
    for relative, data in suite.files.items():
        target = out / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)


__all__ = ["GENERATOR_NAME", "GENERATOR_VERSION", "DEFAULT_SEED", "Suite", "generate", "plan", "write", "replace"]
