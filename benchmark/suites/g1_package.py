"""BEN-05: the G1 evidence package skeleton (noema#566, section 4), rendered in minutes after the run from the run's own files.

`python -m benchmark.suites.g1_package --inputs FILE --a-score FILE --b-score FILE --replay FILE --out DIR`. It decides nothing (AGENT-16) and never says MET for what it cannot tie to evidence:
a row is MET only with an https link the Benchmark Owner opened and says it read; rows 5 (one final head, every check run green at it), 10 (both legs complete, Tier 0 at zero) and 11 (the replay
IDENTICAL, the unknown-outcome case not excluded) are computed from the run and no hand-written verdict can override them. The headline keeps the stand-in label (Founder, 4 Nov 2026).
Exit 0 only when every row is MET and "what was not tested" is not empty. SYNTHETIC inputs; it reads no held-out or expectation data (INV-22)."""
import argparse
import json
import re
import sys
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from benchmark.suites.g1 import named_exclusion_only

Json = dict[str, Any]
LABELS = ("stand-in", "tushe")
SHA = re.compile(r"[0-9a-f]{40}")
ROWS = (
    "US-01 and US-06 end to end on text through the public API", "The unknown outcome is verified by key and never retried", "The Record reconstructs both cases",
    "Property tests green at the final head", "One final head", "The fault is a hostility-proxy FaultPlan, not a mock", "INV-22 holds and is shown", "Leak scan over every artifact, with a canary",
    "Identity and tenant are exercised (two separate refusals, A220)", "The result is scored", "Two clean legs and a replay diff", "No secret or key in any artifact or summary",
)


class PackageError(Exception):
    pass


def _url(value: Any) -> bool:
    return isinstance(value, str) and value.startswith("https://") and " " not in value


def _need(inputs: Mapping[str, Any]) -> None:
    for key in ("dispatch_sha", "run_id", "fault_plan_id"):
        if not isinstance(inputs.get(key), str) or not inputs[key]:
            raise PackageError(f"inputs.{key} is required")
    if inputs.get("data_label") not in LABELS:
        raise PackageError(f"inputs.data_label must be one of {LABELS}")
    us06 = inputs.get("us06_cases")
    if not isinstance(us06, list) or not us06 or not all(isinstance(c, str) for c in us06):
        raise PackageError("inputs.us06_cases must name the unknown-outcome case or cases")
    if not isinstance(inputs.get("rows"), dict) or not isinstance(inputs.get("check_runs"), dict) or not isinstance(inputs.get("not_tested"), list):
        raise PackageError("inputs.rows, inputs.check_runs and inputs.not_tested are required")


def _linked(entry: Any) -> tuple[bool, str]:
    if not isinstance(entry, dict) or not _url(entry.get("link")):
        return False, "no https link"
    if entry.get("read_by_bo") is not True:
        return False, "the Benchmark Owner has not said they read it"
    return True, str(entry["link"])


def _row(n: int, inputs: Mapping[str, Any], a: Mapping[str, Any], b: Mapping[str, Any], replay: Mapping[str, Any]) -> tuple[bool, str]:
    ok, link = _linked(inputs["rows"].get(str(n)))
    sha, checks = inputs["dispatch_sha"], inputs["check_runs"]
    if n == 5:
        if not ok:
            return False, f"row 5 needs the owner's attestation like every row: {link}"
        if not SHA.fullmatch(sha):
            return False, "the dispatch SHA is not a full lowercase 40-hex SHA"
        if checks.get("head_sha") != sha or replay.get("commit_sha") != sha:
            return False, "the check runs or the run were not made at the dispatch SHA"
        if checks.get("all_success") is not True or not _url(checks.get("url")):
            return False, "not every check run at that head is green, or no link"
        return True, str(checks["url"])
    if not ok:
        return False, link
    if n == 10:
        named = {e["case_id"] for e in replay.get("open_exclusions", []) if isinstance(e, dict) and isinstance(e.get("case_id"), str)} - set(inputs["us06_cases"])
        for name, card in (("a", a), ("b", b)):
            tier0 = card.get("tier0") if isinstance(card, Mapping) else None
            clean = isinstance(card, Mapping) and card.get("complete") is True and isinstance(tier0, Mapping) and tier0.get("all_zero") is True
            if not clean and not named_exclusion_only(card, named):
                return False, f"leg {name} is incomplete for a reason that is not a named exclusion, or Tier 0 is not at zero"
    if n == 11:
        if replay.get("verdict") != "IDENTICAL":
            return False, f"replay verdict {replay.get('verdict')}"
        gone = sorted(set(inputs["us06_cases"]) & {e["case_id"] for e in replay.get("open_exclusions", [])})
        if gone:
            return False, f"the unknown-outcome case is excluded: {', '.join(gone)}"
    return True, link


def render(inputs: Mapping[str, Any], card_a: Mapping[str, Any], card_b: Mapping[str, Any], replay: Mapping[str, Any]) -> str:
    _need(inputs)
    stand_in = inputs["data_label"] == "stand-in"
    results = [_row(n, inputs, card_a, card_b, replay) for n in range(1, 13)]
    met = [ok for ok, _ in results]
    notes = [str(x) for x in inputs["not_tested"] if str(x).strip()]
    verdict = "MET" if all(met) and notes else "NOT MET"
    head = "STAND-IN data, not Tushe" if stand_in else "Tushe data"
    lines = [f"# G1 evidence package ({head}) at {inputs['dispatch_sha']}", "",
             f"Run `{inputs['run_id']}`, legs `{replay.get('first', {}).get('run_id', '?')}` and `{replay.get('second', {}).get('run_id', '?')}`, seed {replay.get('seed', '?')}, FaultPlan `{inputs['fault_plan_id']}`. SYNTHETIC data.", ""]
    if stand_in:
        lines += ["Data label: **stand-in** (the enterprise is the stand-in; the model side is the replay provider). Evidence of this kind must be rerun against live Tushe: rerun against live Tushe before G1 is claimed on real data.", ""]
    lines += ["| # | Item | Result | Evidence or reason |", "|---|---|---|---|"]
    for n, (title, (ok, why)) in enumerate(zip(ROWS, results), start=1):
        lines.append(f"| {n} | {title} | **{'MET' if ok else 'NOT MET'}** | {why} |")
    cov = replay.get("case_coverage", {})
    lines += ["", f"Replay coverage: {cov.get('covered', '?')} of {cov.get('of', '?')} cases."]
    excluded = [e["case_id"] for e in replay.get("open_exclusions", [])]
    if excluded:
        lines += ["", "Excluded cases (named, never dropped): " + ", ".join(excluded) + ". The unknown-outcome case is not among them: " + ("yes" if not set(inputs["us06_cases"]) & set(excluded) else "NO, row 11 fails")]
    lines += ["", "## What was not tested", ""]
    lines += [f"- {x}" for x in notes] if notes else ["The section is empty: a package with no stated limits is not accepted."]
    lines += ["", f"G1: {verdict}"]
    return "\n".join(lines) + "\n"


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m benchmark.suites.g1_package", description=__doc__)
    for flag in ("inputs", "a-score", "b-score", "replay", "out"):
        p.add_argument(f"--{flag}", required=True)
    args = p.parse_args(argv)
    try:
        docs = [json.loads(Path(getattr(args, f)).read_text(encoding="utf-8")) for f in ("inputs", "a_score", "b_score", "replay")]
        text = render(*docs)
    except (OSError, ValueError, PackageError) as exc:
        sys.stderr.write(f"g1 package: {exc}\n")
        return 2
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    (out / "G1-EVIDENCE-PACKAGE.md").write_text(text, encoding="utf-8")
    sys.stdout.write(text.splitlines()[0] + "\n" + text.strip().splitlines()[-1] + "\n")
    return 0 if text.strip().endswith("G1: MET") else 1


if __name__ == "__main__":
    sys.exit(main())
