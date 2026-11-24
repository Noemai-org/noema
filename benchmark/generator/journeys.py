"""A652: the seeded J1 to J8 manifest for the front end (PERD §11.1; REQ-UI-001: "the Playwright suite SHALL pass for J1-J8 flows").

`python -m benchmark.generator.journeys --out benchmark/journeys/j1-j8.v1.json [--seed N]` builds, from the generated suite of one seed, the cases each journey is run on: for each of the eight journeys the user stories it
exercises (the PERD's own table) and one to three generated cases tagged with one of them (a journey with one is marked `thin`), chosen by a fixed rule (below). A case is listed with what the customer says (the utterances, the locale and the channel), who says it (the
synthetic principal and its one session role), the world it is said in (the facts fixture, the fault plan, the timeline, the policy and pack versions) and nothing that scores it: no expectation, no oracle reference, no ground
truth (INV-22). Every value is SYNTHETIC (AGENT-25). The same seed gives the same file byte for byte (REQ-BEN-001); a test fails when the committed copy is stale.

The cases themselves are materialised by the generator: `python -m benchmark.generator generate --seed N --out DIR` (the manifest names the command). The rule: among the cases tagged with a story of the journey, not
already used by an earlier journey, take the first (by id) whose locale mix is Pidgin or code-switched, the first that is plain en-NG, then the next by id until there are three.
"""
import argparse
import json
import sys
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Any

from benchmark.generator.generate import DEFAULT_SEED, Suite, generate

Json = dict[str, Any]

# journey -> (title, user stories), from PERD §11.1 (v1.22)
JOURNEYS: dict[str, tuple[str, tuple[str, ...]]] = {
    "J1": ("Clean locate and explain", ("US-01",)),
    "J2": ("Ambiguous counterparty", ("US-02",)),
    "J3": ("Systems disagree", ("US-03", "US-14")),
    "J4": ("Silent success", ("US-06",)),
    "J5": ("Pending settlement", ("US-17",)),
    "J6": ("Confirmation that goes stale", ("US-18",)),
    "J7": ("Prohibited request", ("US-10", "US-22")),
    "J8": ("Abandon and resume", ("US-11",)),
}
PER_JOURNEY = 3
LABEL = "SYNTHETIC generated cases for the front end's J1 to J8 journeys: what the customer says and the world it is said in, no expectation and no oracle (INV-22, AGENT-25)"
CASE_MEMBERS = ("case_id", "principal_id", "roles", "locale_mix", "channel", "utterances", "known_facts_fixture", "fault_plan", "events_timeline", "policy_version", "pack_version")


def _entry(case: Json) -> Json:
    return {"case_id": case["case_id"], "principal_id": case["principal"]["principal_id"], "roles": list(case["principal"]["roles"]), "locale_mix": list(case["locale_mix"]), "channel": case["channel"],
            "utterances": [dict(u) for u in case["utterances"]], "known_facts_fixture": case["known_facts_fixture"], "fault_plan": case["fault_plan"],
            "events_timeline": [dict(e) for e in case["events_timeline"]], "policy_version": case["policy_version"], "pack_version": case["pack_version"]}


def _pidgin(case: Json) -> bool:
    return any(loc != "en-NG" for loc in case["locale_mix"])


def _choose(candidates: Sequence[Json]) -> list[Json]:
    chosen: list[Json] = []
    wants: tuple[Callable[[Json], bool], ...] = (_pidgin, lambda c: not _pidgin(c))
    for want in wants:
        pick = next((c for c in candidates if want(c) and c not in chosen), None)
        if pick is not None:
            chosen.append(pick)
    chosen += [c for c in candidates if c not in chosen][: PER_JOURNEY - len(chosen)]
    return sorted(chosen, key=lambda c: c["case_id"])


def build(suite: Suite) -> Json:
    tags = {e["case_id"]: set(e["covers"]) for e in suite.manifest["cases"]}
    used: set[str] = set()
    rows: list[Json] = []
    for journey, (title, stories) in JOURNEYS.items():
        candidates = [suite.cases[i] for i in sorted(suite.cases) if tags[i] & set(stories) and i not in used]
        chosen = _choose(candidates)
        used |= {c["case_id"] for c in chosen}
        rows.append({"journey": journey, "title": title, "stories": list(stories), "thin": len(chosen) == 1, "cases": [_entry(c) for c in chosen]})
    return {"label": LABEL, "seed": suite.seed, "generator": suite.manifest["generator"], "perd": "PERD §11.1 (v1.22)",
            "materialise": f"python -m benchmark.generator generate --seed {suite.seed} --out DIR", "journeys": rows}


def render(doc: Json) -> str:
    return json.dumps(doc, indent=2, sort_keys=True, ensure_ascii=False) + "\n"


def main(argv: Sequence[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m benchmark.generator.journeys", description=__doc__, allow_abbrev=False)
    p.add_argument("--out", required=True, type=Path)
    p.add_argument("--seed", type=int, default=DEFAULT_SEED)
    a = p.parse_args(argv)
    doc = build(generate(a.seed))
    short = [r["journey"] for r in doc["journeys"] if not r["cases"]]
    if short:
        sys.stderr.write(f"journeys: no case for {', '.join(short)}; nothing written\n")
        return 1
    a.out.parent.mkdir(parents=True, exist_ok=True)
    a.out.write_text(render(doc), encoding="utf-8")
    sys.stdout.write(f"{sum(len(r['cases']) for r in doc['journeys'])} cases in {len(doc['journeys'])} journeys, seed {doc['seed']}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
