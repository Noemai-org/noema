"""Load a generated suite for a run (REQ-BEN-002). Strict, verified against its manifest, generated layer only, and the oracle documents are never opened:
the runner drives a case from the case, its fixture and its fault plan; ground truth is for the scorer and for the oracle client's reading of the environment (INV-22)."""
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from benchmark.contracts import Contracts, StrictJsonError, load_strict, sha256_of

Json = dict[str, Any]
FORBIDDEN = "heldout"


class SuiteError(Exception):
    pass


@dataclass(frozen=True)
class LoadedCase:
    case: Json
    fixture: Json
    plan: Json | None
    entry: Json


@dataclass
class Suite:
    root: Path
    manifest: Json
    cases: list[LoadedCase]
    invalid: list[tuple[str, list[str]]]  # (case id, schema errors): excluded, not run; the run is INCOMPLETE (REQ-BEN-001)


def _load(path: Path) -> Json:
    try:
        doc = load_strict(path)
    except (OSError, ValueError, StrictJsonError) as error:
        raise SuiteError(f"{path.name}: {error}") from None
    if not isinstance(doc, dict):
        raise SuiteError(f"{path.name}: not a JSON object")
    return doc


def _inside(root: Path, relative: str) -> Path:
    path = (root / relative).resolve()
    if root.resolve() not in path.parents:
        raise SuiteError(f"{relative}: outside the suite root")
    return path


def load_suite(root: Path, contracts: Contracts | None = None) -> Suite:
    if FORBIDDEN in str(root).lower():
        raise SuiteError("refused: the path names the held-out set (INV-22, REQ-AGT-004)")
    contracts = contracts or Contracts()
    manifest_path = Path(root) / "manifest.json"
    if not manifest_path.is_file():
        raise SuiteError(f"{root}: no manifest.json (generate a suite first: python -m benchmark.generator generate)")
    manifest = _load(manifest_path)
    problems = contracts.errors("benchmark-suite-manifest", manifest)
    if problems:
        raise SuiteError(f"manifest.json: {problems[0]}")
    cases: list[LoadedCase] = []
    invalid: list[tuple[str, list[str]]] = []
    for entry in manifest["cases"]:
        case_id = entry["case_id"]
        if entry["suite_layer"] != "GENERATED":
            raise SuiteError(f"{case_id}: layer {entry['suite_layer']}; this runner runs the generated layer only")
        case = _load(Path(root) / "cases" / f"{case_id}.json")
        if sha256_of(case) != entry["sha256"]:
            raise SuiteError(f"{case_id}: the case file changed since the manifest was written")
        errors = contracts.errors("benchmark-case", case)
        if errors:
            invalid.append((case_id, errors))
            continue
        fixture = _load(_inside(Path(root), case["known_facts_fixture"]))
        errors = contracts.errors("benchmark-fixture", fixture)
        if errors:
            invalid.append((case_id, [f"fixture {e}" for e in errors]))
            continue
        plan = None
        if case["fault_plan"] != "fp_none":
            plan_path = Path(root) / "fault_plans" / f"{case['fault_plan']}.json"
            if not plan_path.is_file():
                raise SuiteError(f"{case_id}: the fault plan {case['fault_plan']} has no file")
            plan = _load(plan_path)
            errors = contracts.errors("fault-plan", plan)
            if errors:
                invalid.append((case_id, [f"fault plan {e}" for e in errors]))
                continue
        cases.append(LoadedCase(case, fixture, plan, entry))
    return Suite(Path(root), manifest, cases, invalid)
