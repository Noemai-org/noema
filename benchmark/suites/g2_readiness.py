"""BEN-12 (A771): which of the G2 stories' cases this build of the runner can run now, and what stops the rest.

`python -m benchmark.suites.g2_readiness --suite DIR [--webhooks] [--require-all]` reads a generated suite and, for each of the fifteen user stories G2 names, counts its cases and the ones the runner can run now, using the runner's own
support check (`benchmark.runner.support.reasons_for`), so it cannot disagree with `--support` or with a run. A G2 story with cases but none runnable is named: the G2 table would have no row to pass for it, and the reasons say
what to build (a fault producer, an event driver). Exit 0 unless `--require-all` is given and a story has no runnable case (1); the held-out set is refused (2, INV-22). SYNTHETIC; it reads no oracle and no expectation."""
import argparse
import sys
from collections import Counter
from collections.abc import Sequence
from pathlib import Path
from typing import Any

from benchmark.scoring.story_table import G2_STORIES
from benchmark.runner.suite import Suite, SuiteError, load_suite
from benchmark.runner.support import reasons_for

Json = dict[str, Any]
FORBIDDEN = "benchmark-heldout"


def readiness(suite: Suite, *, webhooks: bool = False) -> Json:
    tags = {e["case_id"]: set(e["covers"]) for e in suite.manifest["cases"]}
    rows: list[Json] = []
    for story in G2_STORIES:
        ids: list[str] = []
        runnable: list[str] = []
        blocked: Counter[str] = Counter()
        for lc in suite.cases:
            case_id = lc.case["case_id"]
            if story not in tags[case_id]:
                continue
            ids.append(case_id)
            reasons = reasons_for(lc, webhooks=webhooks)
            if reasons:
                blocked.update(reasons)
            else:
                runnable.append(case_id)
        rows.append({"story": story, "cases": len(ids), "runnable": len(runnable), "runnable_cases": sorted(runnable),
                     "blocked_by": dict(sorted(blocked.items(), key=lambda kv: (-kv[1], kv[0])))})
    return {"suite": suite.manifest.get("suite_id"), "webhooks": webhooks, "stories": rows, "no_runnable_case": [r["story"] for r in rows if r["cases"] and not r["runnable"]],
            "no_case": [r["story"] for r in rows if not r["cases"]]}


def render(doc: Json) -> str:
    lines = ["## G2 readiness: per story, what this build of the runner can run now (SYNTHETIC generated suite)", "", "| Story | Cases | Runnable | Blocked by |", "|---|---|---|---|"]
    for r in doc["stories"]:
        lines.append(f"| {r['story']} | {r['cases']} | {r['runnable']} | {', '.join(f'{k} ({v})' for k, v in r['blocked_by'].items()) or '-'} |")
    stuck, empty = doc["no_runnable_case"], doc["no_case"]
    lines += ["", "Stories with cases but no runnable case: " + (", ".join(stuck) if stuck else "none"), "Stories with no case at all: " + (", ".join(empty) if empty else "none")]
    return "\n".join(lines) + "\n"


def main(argv: Sequence[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m benchmark.suites.g2_readiness", description=__doc__, allow_abbrev=False)
    p.add_argument("--suite", required=True, type=Path)
    p.add_argument("--webhooks", action="store_true", help="count the two webhook kinds as driven, as a runner with a sender (--webhook-payloads) does")
    p.add_argument("--require-all", action="store_true", help="exit 1 when a G2 story has no runnable case")
    a = p.parse_args(argv)
    if FORBIDDEN in str(a.suite).lower():
        sys.stdout.write("refused: the path names the held-out set (INV-22, REQ-AGT-004)\n")
        return 2
    try:
        doc = readiness(load_suite(a.suite), webhooks=a.webhooks)
    except SuiteError as error:
        sys.stdout.write(f"unusable suite: {error}\n")
        return 2
    sys.stdout.write(render(doc))
    return 1 if a.require_all and (doc["no_runnable_case"] or doc["no_case"]) else 0


if __name__ == "__main__":
    sys.exit(main())
