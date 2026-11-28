"""BEN-12 (A771, A637): the G2 family case fixtures.

`python -m benchmark.generator.g2_families --out benchmark/g2/g2-families.v1.json [--seed N]` builds, from the generated suite of one seed, one entry for each of the fifteen user stories G2 names
(`benchmark/scoring/story_table.py`, plan 3.1): every generated case tagged with the story, and what a run loads for it (its facts fixture, its fault plan, the fault effects it injects, the kinds of its timeline
events, its locale mix, its category and families). Nothing that scores a case is here: no expectation, no oracle, no ground truth (INV-22). Every value is SYNTHETIC (AGENT-25). The same seed gives the same file byte for
byte (REQ-BEN-001); a test fails when the committed copy is stale. The G2 table itself comes from a run (`--story-table`); this file names the cases that table will be built over, and `benchmark.suites.g2_readiness`
says which of them this build of the runner can run now.
"""
import argparse
import json
import sys
from collections.abc import Sequence
from pathlib import Path
from typing import Any

from benchmark.generator.generate import DEFAULT_SEED, Suite, generate

Json = dict[str, Any]
G2_STORIES = ("US-02", "US-03", "US-05", "US-07", "US-08", "US-10", "US-11", "US-13", "US-14", "US-15", "US-18", "US-19", "US-20", "US-21", "US-22")
LABEL = "SYNTHETIC generated cases for the fifteen G2 user stories: what a run loads for each, no expectation and no oracle (INV-22, AGENT-25)"
CASE_MEMBERS = ("case_id", "category", "families", "locale_mix", "known_facts_fixture", "fault_plan", "fault_effects", "event_kinds")


def _entry(case: Json, covers: Sequence[str]) -> Json:
    return {"case_id": case["case_id"], "category": case["category"], "families": list(case["families"]), "locale_mix": list(case["locale_mix"]),
            "known_facts_fixture": case["known_facts_fixture"], "fault_plan": case["fault_plan"],
            "fault_effects": sorted(c.removeprefix("FAULT:") for c in covers if c.startswith("FAULT:") and c != "FAULT:NONE"),
            "event_kinds": [e["event"].split(":")[0] for e in case["events_timeline"]]}


def build(suite: Suite) -> Json:
    covers = {e["case_id"]: e["covers"] for e in suite.manifest["cases"]}
    rows = [{"story": story, "cases": [_entry(suite.cases[i], covers[i]) for i in sorted(suite.cases) if story in covers[i]]} for story in G2_STORIES]
    return {"label": LABEL, "seed": suite.seed, "generator": suite.manifest["generator"], "plan": "plan 3.1 (G2 Trust depth)",
            "materialise": f"python -m benchmark.generator generate --seed {suite.seed} --out DIR", "stories": rows}


def render(doc: Json) -> str:
    return json.dumps(doc, indent=2, sort_keys=True, ensure_ascii=False) + "\n"


def main(argv: Sequence[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m benchmark.generator.g2_families", description=__doc__, allow_abbrev=False)
    p.add_argument("--out", required=True, type=Path)
    p.add_argument("--seed", type=int, default=DEFAULT_SEED)
    a = p.parse_args(argv)
    doc = build(generate(a.seed))
    short = [r["story"] for r in doc["stories"] if not r["cases"]]
    if short:
        sys.stderr.write(f"g2_families: no case for {', '.join(short)}; nothing written\n")
        return 1
    a.out.parent.mkdir(parents=True, exist_ok=True)
    a.out.write_text(render(doc), encoding="utf-8")
    sys.stdout.write(f"{sum(len(r['cases']) for r in doc['stories'])} case entries over {len(doc['stories'])} G2 stories, seed {doc['seed']}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
