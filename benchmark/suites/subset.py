"""A scored subset of a generated suite root (BEN-29): the cases, oracles and a valid suite manifest for just the chosen case ids, so the scorer sees exactly the
cases a named suite ran and nothing else. Generated layer only (INV-22, REQ-AGT-004). SYNTHETIC."""
import json
import shutil
from collections.abc import Sequence
from pathlib import Path
from typing import Any

from benchmark.contracts import Contracts, pretty_bytes

Json = dict[str, Any]
LAYERS = ("GENERATED", "HELD_OUT", "PARTNER")


def _pidgin_or_code_switched(case: Json) -> bool:
    mix = case["locale_mix"]
    return "pcm" in mix or len(mix) > 1


def subset_suite(src: Path, case_ids: Sequence[str], dst: Path, suite_name: str) -> None:
    manifest = json.loads((src / "manifest.json").read_text(encoding="utf-8"))
    by_id = {e["case_id"]: e for e in manifest["cases"]}
    missing = sorted(set(case_ids) - set(by_id))
    if missing:
        raise ValueError(f"the suite has no case {', '.join(missing[:3])}")
    chosen = [by_id[i] for i in sorted(case_ids)]
    if dst.exists():
        shutil.rmtree(dst)
    (dst / "cases").mkdir(parents=True)
    cases = []
    for entry in chosen:
        case_file = src / "cases" / f"{entry['case_id']}.json"
        case = json.loads(case_file.read_text(encoding="utf-8"))
        cases.append(case)
        shutil.copyfile(case_file, dst / "cases" / case_file.name)
        oracle = src / case["oracle_ref"]
        target = dst / case["oracle_ref"]
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(oracle, target)
    # The scorer reads oracles from one directory; the generator writes them under oracle/. A flat `oracles/` view keeps the scorer's own CLI unchanged.
    (dst / "oracles").mkdir()
    for oracle_file in sorted((dst / "oracle").rglob("*.json")):
        shutil.copyfile(oracle_file, dst / "oracles" / oracle_file.name)
    layer_counts = {layer: sum(1 for e in chosen if e["suite_layer"] == layer) for layer in LAYERS}
    category_counts = {category: 0 for category in manifest["category_counts"]}
    for entry in chosen:
        category_counts[entry["category"]] += 1
    document: Json = {
        "suite_id": f"{manifest['suite_id']}-{suite_name}"[:64], "suite_version": manifest["suite_version"], "generator": manifest["generator"],
        "layer_counts": layer_counts, "category_counts": category_counts,
        "pidgin_or_code_switched": {"count": sum(1 for c in cases if _pidgin_or_code_switched(c)), "of": len(cases)}, "cases": chosen,
    }
    problems = Contracts().errors("benchmark-suite-manifest", document)
    if problems:
        raise ValueError(f"the subset manifest is invalid: {problems[0]}")
    (dst / "manifest.json").write_bytes(pretty_bytes(document))
