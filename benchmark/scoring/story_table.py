"""BEN-12 (A300): the G2 table, one row per user story and a by-family rollup, as a pure function over the case judgements and the manifest.

It adds up what the scorer already decided (`Judgement`) and decides nothing itself (AGENT-16): no threshold, no new pass rule beyond "every case of the story completed, handled, no gate".
A story with no case is NO_CASE, never a pass; an incomplete case never passes a story. Aggregates and case ids only. SYNTHETIC. Generated layer only (INV-22)."""
from collections.abc import Mapping, Sequence
from typing import Any

from collections import defaultdict

from benchmark.scoring.judge import Judgement, judge
from benchmark.scoring.registry import Registry

Json = dict[str, Any]
STORIES = tuple(f"US-{n:02d}" for n in range(1, 23))
# The stories gate G2 names (docs/poc/execution/gates/G2.md, plan 3.1). The gate's other conditions (write capabilities, kill switch, conformance packs) are not in this table.
G2_STORIES = ("US-02", "US-03", "US-05", "US-07", "US-08", "US-10", "US-11", "US-13", "US-14", "US-15", "US-18", "US-19", "US-20", "US-21", "US-22")


class StoryTableError(Exception):
    pass


def _row(group: Sequence[Judgement]) -> Json:
    failing = sorted(j.case_id for j in group if j.completed and (not j.handled or j.gates))
    incomplete = sorted(j.case_id for j in group if not j.completed)
    if not group:
        status = "NO_CASE"
    elif failing:
        status = "FAIL"
    elif incomplete:
        status = "INCOMPLETE"
    else:
        status = "PASS"
    return {"n": len(group), "handled": sum(1 for j in group if j.handled), "gate_trips": sum(1 for j in group if j.gates), "incomplete": len(incomplete),
            "status": status, "failing": failing, "incomplete_cases": incomplete}


def story_table(judgements: Sequence[Judgement], manifest: Mapping[str, Any], families: Mapping[str, Sequence[str]]) -> Json:
    ids = [j.case_id for j in judgements]
    if len(set(ids)) != len(ids):
        raise StoryTableError("a case is judged twice")
    covers = {e["case_id"]: list(e["covers"]) for e in manifest["cases"]}
    if set(ids) != set(covers):
        raise StoryTableError(f"judgements and manifest disagree on the cases: {sorted(set(ids) ^ set(covers))[:5]}")
    missing = sorted(c for c in ids if not families.get(c))
    if missing:
        raise StoryTableError(f"cases without families: {missing[:5]}")
    ordered = sorted(judgements, key=lambda j: j.case_id)
    stories = {s: _row([j for j in ordered if s in covers[j.case_id]]) for s in STORIES}
    names = sorted({f for c in ids for f in families[c]})
    rollup = {f: _row([j for j in ordered if f in families[j.case_id]]) for f in names}
    not_passing = [s for s in G2_STORIES if stories[s]["status"] != "PASS"]
    return {"stories": stories, "families": rollup, "g2": {"status": "NOT_PASSING" if not_passing else "ALL_PASS", "not_passing": not_passing}}


def render(table: Mapping[str, Any]) -> str:
    lines = ["## G2 table: one row per user story (SYNTHETIC generated suite; aggregates and case ids only)", "", "| Story | Cases | Handled | Status | Failing or incomplete |", "|---|---|---|---|---|"]
    for s, r in table["stories"].items():
        lines.append(f"| {s} | {r['n']} | {r['handled']} | {r['status']} | {', '.join(r['failing'] + r['incomplete_cases']) or '-'} |")
    lines += ["", "## By family", "", "| Family | Cases | Handled | Status |", "|---|---|---|---|"]
    lines += [f"| {f} | {r['n']} | {r['handled']} | {r['status']} |" for f, r in table["families"].items()]
    g2 = table["g2"]
    lines += ["", f"G2: {g2['status']}" + (f" (not passing: {', '.join(g2['not_passing'])})" if g2["not_passing"] else "")]
    return "\n".join(lines) + "\n"


def from_run(cases: Sequence[Json], oracles: Sequence[Json], results: Sequence[Json], registry: Registry, manifest: Mapping[str, Any]) -> Json:
    """Judge each case exactly as the scorecard does (a duplicate result is an unscored case), then build the table. Families come from the case files."""
    oracle_by_id = {o["case_id"]: o for o in oracles}
    found: dict[str, list[Json]] = defaultdict(list)
    for r in results:
        found[r["case_id"]].append(r)
    judgements = [judge(c, oracle_by_id[c["case_id"]], found[c["case_id"]][0] if len(found.get(c["case_id"], [])) == 1 else None, registry,
                        problem="DUPLICATE_RESULT" if len(found.get(c["case_id"], [])) > 1 else None) for c in cases]
    scored = {c["case_id"] for c in cases}
    narrowed = {**manifest, "cases": [e for e in manifest["cases"] if e["case_id"] in scored]}
    return story_table(judgements, narrowed, {c["case_id"]: c["families"] for c in cases})
