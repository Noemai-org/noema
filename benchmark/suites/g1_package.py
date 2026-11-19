"""BEN-05: the G1 evidence package skeleton (noema#566, section 4), rendered in minutes after the run from the run's own files.

`python -m benchmark.suites.g1_package --inputs F --a-score F --b-score F --replay F --check-runs F --leak-scan F --probes F --out DIR`. It decides nothing (AGENT-16) and never says MET for what
it cannot tie to evidence. Rows 5, 8 and 9 are computed from the run's artifacts, not attested (ruling A355): row 5 from the check-runs JSON (every check run at the dispatch SHA completed and
green, the required checks present and successful), row 8 from the leak-scan output (the probe saw every planted kind, a clean scan that read something, no failure line), row 9 from the probe results
(the two separate refusals, the control and the unknown correlation for every case, recomputed from status and code, never from an `ok` flag). Rows 10 (both scorecards bound to their replay legs,
complete or only named exclusions, Tier 0 at zero) and 11 (every expected G1 case of both stories IDENTICAL in the replay, once each, unless it is a named exclusion; never the unknown-outcome case)
are computed from the run too. Every other row is MET only with an https link the Benchmark Owner opened and says they read. No hand-written verdict overrides a computed row.
The headline keeps the stand-in label (Founder, 4 Nov 2026). Exit 0 only when every row is MET and "what was not tested" is not empty. SYNTHETIC inputs; it reads no held-out or expectation data (INV-22)."""
import argparse
import hashlib
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
COMPUTED = (5, 8, 9)
GREEN = frozenset({"success", "skipped", "neutral"})
PROBE_KINDS = ("canary", "account_number", "card_number", "named_secret_value", "jwt", "bearer_token")
PROBE_LINE = re.compile(r"^probe: every planted kind was found \((?P<kinds>[^)]*)\): the scanner can see$", re.MULTILINE)
CLEAN_LINE = re.compile(r"^leakscan OK: scanned (?P<files>\d+) files, (?P<lines>\d+) lines$", re.MULTILINE)
BAD_LINE = re.compile(r"^(?:leakscan FAIL|LEAK SCAN FAILED|NOT RUN|refused)", re.MULTILINE)
NOT_FOUND = (404, "NOT_FOUND_OR_NOT_AUTHORIZED")
SESSION_INVALID = (401, "SESSION_INVALID")
ROWS = (
    "US-01 and US-06 end to end on text through the public API", "The unknown outcome is verified by key and never retried", "The Record reconstructs both cases",
    "Property tests green at the final head", "One final head", "The fault is a hostility-proxy FaultPlan, not a mock", "INV-22 holds and is shown", "Leak scan over every artifact, with a canary",
    "Identity and tenant are exercised (two separate refusals, A220)", "The result is scored", "Two clean legs and a replay diff", "No secret or key in any artifact or summary",
)


class PackageError(Exception):
    pass


def _url(value: Any) -> bool:
    return isinstance(value, str) and value.startswith("https://") and " " not in value


def _strings(value: Any) -> bool:
    return isinstance(value, list) and bool(value) and all(isinstance(c, str) and c for c in value)


def _need(inputs: Mapping[str, Any]) -> None:
    for key in ("dispatch_sha", "run_id", "fault_plan_id"):
        if not isinstance(inputs.get(key), str) or not inputs[key]:
            raise PackageError(f"inputs.{key} is required")
    if inputs.get("data_label") not in LABELS:
        raise PackageError(f"inputs.data_label must be one of {LABELS}")
    if not _strings(inputs.get("us06_cases")):
        raise PackageError("inputs.us06_cases must name the unknown-outcome case or cases")
    if not _strings(inputs.get("g1_cases")) or not set(inputs["us06_cases"]) <= set(inputs["g1_cases"]):
        raise PackageError("inputs.g1_cases must list every case the G1 replay is expected to compare, the unknown-outcome cases included")
    if not _strings(inputs.get("required_checks")):
        raise PackageError("inputs.required_checks must name the check runs that must be present and successful (for example g1-stack)")
    if not isinstance(inputs.get("rows"), dict) or not isinstance(inputs.get("not_tested"), list):
        raise PackageError("inputs.rows and inputs.not_tested are required")


def _linked(entry: Any) -> tuple[bool, str]:
    if not isinstance(entry, dict) or not _url(entry.get("link")):
        return False, "no https link"
    if entry.get("read_by_bo") is not True:
        return False, "the Benchmark Owner has not said they read it"
    return True, str(entry["link"])


def _digest(artifact: Any) -> str:
    raw = artifact.encode("utf-8") if isinstance(artifact, str) else json.dumps(artifact, sort_keys=True, separators=(",", ":"), default=repr).encode("utf-8")
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def _leg_id(replay: Mapping[str, Any], which: str) -> str | None:
    leg = replay.get(which)
    run_id = leg.get("run_id") if isinstance(leg, Mapping) else None
    return run_id if isinstance(run_id, str) and run_id else None


def _compared(replay: Mapping[str, Any]) -> tuple[dict[str, Any] | None, str]:
    """case id to result for every row of the replay's `cases`. None, with the reason, when the list is missing, any row is unreadable or a case id appears twice (a later row must never
    overwrite an earlier failure)."""
    rows = replay.get("cases")
    if not isinstance(rows, list) or not all(isinstance(r, Mapping) and isinstance(r.get("case_id"), str) for r in rows):
        return None, "the replay report has no readable list of compared cases"
    ids = [r["case_id"] for r in rows]
    repeated = sorted({c for c in ids if ids.count(c) > 1})
    if repeated:
        return None, f"a case appears more than once in the replay: {', '.join(repeated)}"
    return {r["case_id"]: r.get("result") for r in rows}, ""


def _row5(inputs: Mapping[str, Any], replay: Mapping[str, Any], doc: Any) -> tuple[bool, str]:
    sha = inputs["dispatch_sha"]
    pages = doc if isinstance(doc, list) else [doc]
    if not pages or not all(isinstance(p, Mapping) and isinstance(p.get("check_runs"), list) and type(p.get("total_count")) is int for p in pages):
        return False, "the check-runs artifact is not readable"
    totals = {p["total_count"] for p in pages}
    runs = [r for p in pages for r in p["check_runs"]]
    if len(totals) != 1 or len(runs) != next(iter(totals)):
        return False, "the check-runs artifact is truncated or inconsistent: its pages do not add up to its total"
    if not runs or not all(isinstance(r, Mapping) and isinstance(r.get("name"), str) and r["name"] for r in runs):
        return False, "the check-runs artifact has no runs, or a run without a name"
    if not SHA.fullmatch(sha):
        return False, "the dispatch SHA is not a full lowercase 40-hex SHA"
    if replay.get("commit_sha") != sha:
        return False, "the run was not made at the dispatch SHA"
    stray = sorted(str(r["name"]) for r in runs if r.get("head_sha") != sha)
    if stray:
        return False, f"check runs not at the dispatch SHA: {', '.join(stray)}"
    not_green = sorted(f"{r['name']} ({r.get('status')}/{r.get('conclusion')})" for r in runs if r.get("status") != "completed" or r.get("conclusion") not in GREEN)
    if not_green:
        return False, f"check runs not completed and green: {', '.join(not_green)}"
    unmet = sorted(n for n in inputs["required_checks"] if not any(r["name"] == n for r in runs) or any(r["name"] == n and r.get("conclusion") != "success" for r in runs))
    if unmet:
        return False, f"required check runs absent or not successful: {', '.join(unmet)}"
    return True, f"computed from the check-runs artifact {_digest(doc)}"


def _row8(text: Any) -> tuple[bool, str]:
    if not isinstance(text, str) or not text.strip():
        return False, "the leak-scan output is empty or unreadable"
    bad = BAD_LINE.search(text)
    if bad:
        return False, f"the leak-scan output carries a failure line ({bad.group(0)})"
    probe = PROBE_LINE.search(text)
    if not probe or not set(PROBE_KINDS) <= {k.strip() for k in probe.group("kinds").split(",")}:
        return False, "the leak-scan output does not show the probe seeing every planted kind"
    clean = CLEAN_LINE.search(text)
    if not clean or int(clean.group("files")) < 1 or int(clean.group("lines")) < 1:
        return False, "the leak-scan output has no clean scan that read at least one file and one line"
    return True, f"computed from the leak-scan output {_digest(text)}"


def _probe_ok(row: Any) -> bool:
    if not isinstance(row, Mapping):
        return False

    def got(name: str) -> Mapping[str, Any] | None:
        value = row.get(name)
        return value if isinstance(value, Mapping) else None

    control, wrong, foreign, unknown = got("control"), got("wrong_role"), got("foreign_tenant"), got("unknown_correlation")
    if None in (control, wrong, foreign, unknown) or row.get("ok") is not True:
        return False
    assert control is not None and wrong is not None and foreign is not None and unknown is not None
    return (control.get("status") == 200 and control.get("ok") is True
            and (wrong.get("status"), wrong.get("code")) == NOT_FOUND and (foreign.get("status"), foreign.get("code")) == SESSION_INVALID
            and (unknown.get("status"), unknown.get("code"), unknown.get("message")) == (wrong.get("status"), wrong.get("code"), wrong.get("message")))


def _row9(inputs: Mapping[str, Any], replay: Mapping[str, Any], doc: Any) -> tuple[bool, str]:
    rows = doc.get("cases") if isinstance(doc, Mapping) else None
    if not isinstance(doc, Mapping) or doc.get("met") is not True or not isinstance(rows, list) or not rows or not all(isinstance(r, Mapping) and isinstance(r.get("case_id"), str) for r in rows):
        return False, "the probe results are not readable, or do not say met"
    ids = [r["case_id"] for r in rows]
    repeated = sorted({c for c in ids if ids.count(c) > 1})
    if repeated:
        return False, f"a case is probed more than once: {', '.join(repeated)}"
    named = {e["case_id"] for e in replay.get("open_exclusions", []) if isinstance(e, Mapping) and isinstance(e.get("case_id"), str)}
    by_id = {r["case_id"]: r for r in rows}
    missing = sorted(c for c in inputs["g1_cases"] if c not in named and c not in by_id)
    if missing:
        return False, f"cases with no probe: {', '.join(missing)}"
    failing = sorted(c for c in inputs["g1_cases"] if c in by_id and not _probe_ok(by_id[c]))
    if failing:
        return False, f"probes that fail (recomputed from status and code): {', '.join(failing)}"
    return True, f"computed from the probe results {_digest(doc)}"


def _row(n: int, inputs: Mapping[str, Any], a: Mapping[str, Any], b: Mapping[str, Any], replay: Mapping[str, Any], artifacts: Mapping[str, Any]) -> tuple[bool, str]:
    if n == 5:
        return _row5(inputs, replay, artifacts.get("check_runs"))
    if n == 8:
        return _row8(artifacts.get("leak_scan"))
    if n == 9:
        return _row9(inputs, replay, artifacts.get("probes"))
    ok, link = _linked(inputs["rows"].get(str(n)))
    if not ok:
        return False, link
    if n == 10:
        named = {e["case_id"] for e in replay.get("open_exclusions", []) if isinstance(e, dict) and isinstance(e.get("case_id"), str)} - set(inputs["us06_cases"])
        for name, card, leg_id in (("a", a, _leg_id(replay, "first")), ("b", b, _leg_id(replay, "second"))):
            if not isinstance(card, Mapping) or not isinstance(card.get("run_id"), str) or card["run_id"] != leg_id:
                return False, f"scorecard {name} is not the scorecard of replay leg {leg_id!r} (run_id {card.get('run_id') if isinstance(card, Mapping) else None!r})"
            tier0 = card.get("tier0")
            clean = card.get("complete") is True and isinstance(tier0, Mapping) and tier0.get("all_zero") is True
            if not clean and not named_exclusion_only(card, named):
                return False, f"leg {name} is incomplete for a reason that is not a named exclusion, or Tier 0 is not at zero"
    if n == 11:
        if replay.get("verdict") != "IDENTICAL":
            return False, f"replay verdict {replay.get('verdict')}"
        excluded = {e["case_id"] for e in replay.get("open_exclusions", []) if isinstance(e, Mapping) and isinstance(e.get("case_id"), str)}
        gone = sorted(set(inputs["us06_cases"]) & excluded)
        if gone:
            return False, f"the unknown-outcome case is excluded: {', '.join(gone)}"
        compared, why = _compared(replay)
        if compared is None:
            return False, why
        missing = sorted(c for c in inputs["g1_cases"] if c not in excluded and compared.get(c) != "IDENTICAL")
        if missing:
            return False, f"expected cases not in the replay, or not IDENTICAL there: {', '.join(missing)}"
    return True, link


def render(inputs: Mapping[str, Any], card_a: Mapping[str, Any], card_b: Mapping[str, Any], replay: Mapping[str, Any], artifacts: Mapping[str, Any]) -> str:
    _need(inputs)
    stand_in = inputs["data_label"] == "stand-in"
    results = [_row(n, inputs, card_a, card_b, replay, artifacts) for n in range(1, 13)]
    met = [ok for ok, _ in results]
    notes = [str(x) for x in inputs["not_tested"] if str(x).strip()]
    verdict = "MET" if all(met) and notes else "NOT MET"
    head = "STAND-IN data, not Tushe" if stand_in else "Tushe data"
    lines = [f"# G1 evidence package ({head}) at {inputs['dispatch_sha']}", "",
             f"Run `{inputs['run_id']}`, legs `{replay.get('first', {}).get('run_id', '?')}` and `{replay.get('second', {}).get('run_id', '?')}`, seed {replay.get('seed', '?')}, "
             f"FaultPlan `{inputs['fault_plan_id']}`. SYNTHETIC data.", ""]
    if stand_in:
        lines += ["Data label: **stand-in** (the enterprise is the stand-in; the model side is the replay provider). Evidence of this kind must be rerun against live Tushe: "
                  "rerun against live Tushe before G1 is claimed on real data.", ""]
    lines += ["| # | Item | Result | Evidence or reason |", "|---|---|---|---|"]
    for n, (title, (ok, why)) in enumerate(zip(ROWS, results), start=1):
        lines.append(f"| {n} | {title} | **{'MET' if ok else 'NOT MET'}** | {why} |")
    cov = replay.get("case_coverage", {})
    lines += ["", f"Replay coverage: {cov.get('covered', '?')} of {cov.get('of', '?')} cases."]
    excluded = [e["case_id"] for e in replay.get("open_exclusions", [])]
    if excluded:
        lines += ["", "Excluded cases (named, never dropped): " + ", ".join(excluded) + ". The unknown-outcome case is not among them: "
                  + ("yes" if not set(inputs["us06_cases"]) & set(excluded) else "NO, row 11 fails")]
    lines += ["", "## What was not tested", ""]
    lines += [f"- {x}" for x in notes] if notes else ["The section is empty: a package with no stated limits is not accepted."]
    lines += ["", f"G1: {verdict}"]
    return "\n".join(lines) + "\n"


def _json_documents(text: str) -> list[Any]:
    """One document, or the concatenated pages `gh api --paginate` writes."""
    decoder, at, found = json.JSONDecoder(), 0, []
    while at < len(text):
        while at < len(text) and text[at].isspace():
            at += 1
        if at >= len(text):
            break
        value, at = decoder.raw_decode(text, at)
        found.append(value)
    return found


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m benchmark.suites.g1_package", description=__doc__)
    for flag in ("inputs", "a-score", "b-score", "replay", "check-runs", "leak-scan", "probes", "out"):
        p.add_argument(f"--{flag}", required=True)
    args = p.parse_args(argv)
    try:
        inputs, a, b, replay, probes = (json.loads(Path(getattr(args, f)).read_text(encoding="utf-8")) for f in ("inputs", "a_score", "b_score", "replay", "probes"))
        pages = _json_documents(Path(args.check_runs).read_text(encoding="utf-8"))
        text = render(inputs, a, b, replay, {"check_runs": pages[0] if len(pages) == 1 else pages, "leak_scan": Path(args.leak_scan).read_text(encoding="utf-8"), "probes": probes})
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
