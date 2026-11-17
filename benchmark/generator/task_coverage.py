"""Per-ID coverage of the case-family tasks BEN-06 to BEN-09 (REQ-BEN-001: "Cases validate; each is linked to its EC or US ID").

`python -m benchmark.generator.task_coverage --task BEN-06 [--seed N]` generates the suite, reads the coverage tags of its manifest (tags and case ids only: no oracle and no expectation is
read) and prints, for each identifier the task names, which generated cases carry it. An identifier no case carries is a GAP and the command exits 1. An identifier carried by exactly one case is
reported as THIN: covered, but with no second case to show it is not a one-off. Synthetic data only (AGENT-25)."""
import argparse
import sys
from typing import Any, Mapping

from benchmark.generator.catalogue import EDGE_CASES
from benchmark.generator.generate import DEFAULT_SEED, generate

Json = dict[str, Any]

# The identifiers each plan row names (docs/poc/execution/tasks.csv). An "EC-X*" entry is every catalogue edge case with that prefix.
TASKS: dict[str, tuple[str, ...]] = {
    "BEN-06": ("EC-A*", "US-02", "US-07", "PUS-01", "PUS-02", "PUS-08"),
    "BEN-07": ("US-03", "US-04", "US-13", "US-14", "EC-S*"),
    "BEN-08": ("US-08", "US-18", "US-20", "EC-T*"),
    "BEN-09": ("US-05", "US-21", "EC-S19", "EC-E*"),
}
TITLES = {"BEN-06": "ambiguity and confused-deputy cases", "BEN-07": "state-conflict cases", "BEN-08": "policy-mutation and clock cases", "BEN-09": "integration-hostility and duplicate cases"}


def selectors(task: str) -> list[str]:
    """The identifiers a task names, with every `EC-X*` expanded from the catalogue, sorted and without repeats."""
    if task not in TASKS:
        raise ValueError(f"unknown task {task!r}; expected one of {', '.join(sorted(TASKS))}")
    out: set[str] = set()
    for entry in TASKS[task]:
        if entry.endswith("*"):
            out |= {i for i in EDGE_CASES if i.startswith(entry[:-1])}
        else:
            out.add(entry)
    return sorted(out)


def coverage(manifest: Mapping[str, Any], task: str) -> Json:
    """For each identifier of the task: the case ids that carry it. `gaps` have none; `thin` have exactly one."""
    tags: dict[str, list[str]] = {}
    for entry in manifest["cases"]:
        for tag in entry["covers"]:
            tags.setdefault(tag, []).append(entry["case_id"])
    rows = {i: sorted(tags.get(i, [])) for i in selectors(task)}
    return {"rows": rows, "gaps": [i for i, c in rows.items() if not c], "thin": [i for i, c in rows.items() if len(c) == 1]}


def render(result: Mapping[str, Any], task: str) -> str:
    rows: Mapping[str, list[str]] = result["rows"]
    lines = [f"## {task}: {TITLES[task]}: coverage by identifier (SYNTHETIC generated suite; tags and case ids only)", "",
             "| ID | Cases | Depth | First case ids |", "|---|---|---|---|"]
    for ident, cases in rows.items():
        depth = "GAP" if not cases else ("thin" if len(cases) == 1 else "ok")
        lines.append(f"| {ident} | {len(cases)} | {depth} | {', '.join(cases[:3]) if cases else '-'} |")
    lines += ["", f"identifiers: {len(rows)}; gaps: {len(result['gaps'])}; thin (exactly one case): {len(result['thin'])}"]
    if result["gaps"]:
        lines.append("GAP: " + ", ".join(result["gaps"]))
    if result["thin"]:
        lines.append("thin: " + ", ".join(result["thin"]))
    return "\n".join(lines) + "\n"


def report(manifest: Mapping[str, Any], task: str) -> tuple[str, int]:
    """The printed table and the exit code (1 when any identifier has no case), from a manifest."""
    result = coverage(manifest, task)
    return render(result, task), 1 if result["gaps"] else 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python -m benchmark.generator.task_coverage", description=__doc__)
    parser.add_argument("--task", required=True, choices=sorted(TASKS))
    parser.add_argument("--seed", type=int, default=DEFAULT_SEED)
    args = parser.parse_args(argv)
    text, code = report(generate(args.seed).manifest, args.task)
    sys.stdout.write(text)
    return code


if __name__ == "__main__":
    sys.exit(main())
