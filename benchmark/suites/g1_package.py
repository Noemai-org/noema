"""BEN-05: the G1 evidence package skeleton (noema#566, section 4), rendered in minutes after the run from the run's own files.

`python -m benchmark.suites.g1_package --inputs F --a-score F --b-score F --replay F --check-runs F --leak-scan F --probes F --reconstructions F --mock-case-writes F --correlations F --run-stamp F --out DIR`. It decides nothing (AGENT-16) and never says MET for what
it cannot tie to evidence. Row 2 is computed from the reconstructions and the mock-case write counts (A375): US-06 shows one GRANT_ISSUED (no second grant), one GRANT_SPENT, one DISPATCH_ATTEMPTED and one RECEIPT classified
OUTCOME_UNKNOWN, then a read by key and an ACHIEVED verdict, the same counts after a recovery sweep and after a replay, and one write; US-01 shows ALLOW with no GRANT_* event, no dispatch and zero
writes (a grant on US-01 fails the row). Every reconstruction must be the run's tenant's and the case's own correlation (the run's correlations.json), the same in every phase. A444: the dispatch SHA and the run id are stamped into every artifact
(`stamp: <sha> <run id>` as the first line of the leak-scan output, `{"stamp": {"sha", "run_id"}, ...}` in the JSON files, and a `stamp.json` for the run directory that holds `correlations.json`), and an artifact of another
dispatch or run is refused; the probes must be of the run's own correlations. Rows 5, 8 and 9 are computed from the run's artifacts, not attested (ruling A355): row 5 from the check-runs JSON (every check run at the dispatch SHA completed and
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
COMPUTED = (2, 5, 8, 9)
PHASES = ("initial", "after_recovery", "after_replay")
COUNTED = ("GRANT_ISSUED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "VERIFICATION_READ", "VERDICT")
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
    us01 = inputs.get("us01_cases")
    if not _strings(us01) or not set(us01 or []) <= set(inputs["g1_cases"]) or set(us01 or []) & set(inputs["us06_cases"]):
        raise PackageError("inputs.us01_cases must name the US-01 cases, inside g1_cases and apart from the unknown-outcome cases")
    if not _text_value(inputs.get("tenant_id")):
        raise PackageError("inputs.tenant_id is required: the reconstructions must be the run's tenant's")
    if not isinstance(inputs.get("rows"), dict) or not isinstance(inputs.get("not_tested"), list):
        raise PackageError("inputs.rows and inputs.not_tested are required")


def _linked(entry: Any) -> tuple[bool, str]:
    if not isinstance(entry, dict) or not _url(entry.get("link")):
        return False, "no https link"
    if entry.get("read_by_bo") is not True:
        return False, "the Benchmark Owner has not said they read it"
    return True, str(entry["link"])


def _stamp_problem(stamp: Any, inputs: Mapping[str, Any], what: str) -> str | None:
    """A444: the dispatch SHA and the run id are stamped into every artifact. An artifact of another dispatch or another run, or one with no stamp, is refused."""
    if not isinstance(stamp, Mapping) or set(stamp) != {"sha", "run_id"}:
        return f"{what} carries no stamp of the form {{sha, run_id}}"
    if stamp["sha"] != inputs["dispatch_sha"] or stamp["run_id"] != inputs["run_id"]:
        return f"{what} is stamped for another dispatch or another run"
    return None


def _unwrap(doc: Any, key: str, inputs: Mapping[str, Any], what: str) -> tuple[Any, str | None]:
    """The files the runner writes for the package are `{"stamp": {...}, "<key>": <content>}`."""
    if not isinstance(doc, Mapping) or key not in doc:
        return None, f"{what} is not readable (expected a stamp and {key!r})"
    problem = _stamp_problem(doc.get("stamp"), inputs, what)
    return (None, problem) if problem else (doc[key], None)


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


def _row8(inputs: Mapping[str, Any], text: Any) -> tuple[bool, str]:
    if not isinstance(text, str) or not text.strip():
        return False, "the leak-scan output is empty or unreadable"
    if text.split("\n", 1)[0] != f"stamp: {inputs['dispatch_sha']} {inputs['run_id']}":
        return False, "the leak-scan output does not begin with this dispatch's and run's stamp line"
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


SECRET_NAME = re.compile(r"(\.(key|pem|p12|pfx|jks)$|(^|/)id_(rsa|dsa|ecdsa|ed25519)|(^|/)\.env|credential|secret|token|private)", re.IGNORECASE)


def _row12(inputs: Mapping[str, Any], files: Any, leak_text: Any) -> tuple[bool, str]:
    """No secret or key in any artifact or summary, computed (A355): the artifact file list is this run's (stamp line first), has no file named like a secret or a key, names no absolute or escaping
    path and no file twice, and the leak scan passes AND read at least as many files as the list names."""
    if not isinstance(files, str) or not files.strip():
        return False, "no artifact file list: the row is computed from it and the leak scan (--artifact-files)"
    head, _, rest = files.partition("\n")
    if head != f"stamp: {inputs['dispatch_sha']} {inputs['run_id']}":
        return False, "the artifact file list does not begin with this dispatch's and run's stamp line"
    names = rest.split("\n")
    if names and names[-1] == "":
        names = names[:-1]
    if not names or any(not n.strip() for n in names):
        return False, "the artifact file list is empty or has a blank line"
    if len(set(names)) != len(names):
        return False, "the artifact file list names a file twice"
    bad = sorted(n for n in names if n.startswith("/") or "\\" in n or re.match(r"^[A-Za-z]:", n) or ".." in n.split("/"))
    if bad:
        return False, f"an absolute or escaping path is listed: {', '.join(bad[:3])}"
    secret = sorted(n for n in names if SECRET_NAME.search(n))
    if secret:
        return False, f"a file named like a secret or a key is in the uploaded set: {', '.join(secret[:3])}"
    scanned, why = _row8(inputs, leak_text)
    if not scanned:
        return False, f"the leak scan does not pass (row 8): {why}"
    clean = CLEAN_LINE.search(str(leak_text))
    if not clean or int(clean.group("files")) < len(names):
        return False, f"the leak scan read fewer files than the {len(names)} listed"
    return True, f"computed from the artifact file list ({len(names)} files, none named like a secret or key) and the leak scan, which read at least all of them"


def _row9(inputs: Mapping[str, Any], replay: Mapping[str, Any], doc: Any, run_stamp: Any, correlations: Any) -> tuple[bool, str]:
    stamp_problem = _stamp_problem(doc.get("stamp") if isinstance(doc, Mapping) else None, inputs, "the probe results") or _stamp_problem(run_stamp, inputs, "the run's stamp")
    if stamp_problem:
        return False, stamp_problem
    if not isinstance(correlations, Mapping):
        return False, "the run's correlations are not readable"
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
    unbound = sorted(c for c in inputs["g1_cases"] if c not in named and (not _text_value(correlations.get(c)) or by_id[c].get("correlation_id") != correlations.get(c)))
    if unbound:
        return False, f"probes not of this run's correlations (or the run names none): {', '.join(unbound)}"
    failing = sorted(c for c in inputs["g1_cases"] if c in by_id and not _probe_ok(by_id[c]))
    if failing:
        return False, f"probes that fail (recomputed from status and code): {', '.join(failing)}"
    return True, f"computed from the probe results {_digest(doc)}"


def _events(doc: Any) -> list[tuple[int, str]] | None:
    """(seq, event type) of every Record event of a reconstruction, in seq order; None when the list or any row is unreadable."""
    rows = doc.get("events") if isinstance(doc, Mapping) else None
    if not isinstance(rows, list) or not all(isinstance(r, Mapping) and _plain_int(r.get("seq")) and _text_value(r.get("event_type")) for r in rows):
        return None
    return sorted((r["seq"], r["event_type"]) for r in rows)


def _plain_int(value: Any) -> bool:
    return type(value) is int


def _text_value(value: Any) -> bool:
    return isinstance(value, str) and bool(value)


def _facts(doc: Any, kind: str) -> list[Mapping[str, Any]] | None:
    sections = doc.get("sections") if isinstance(doc, Mapping) else None
    if not isinstance(sections, list):
        return None
    found: list[Mapping[str, Any]] = []
    for sec in sections:
        if isinstance(sec, Mapping) and sec.get("kind") == kind:
            for item in sec.get("items", []) if isinstance(sec.get("items"), list) else []:
                if isinstance(item, Mapping) and isinstance(item.get("facts"), Mapping):
                    found.append(item["facts"])
    return found


def _us06_problem(doc: Any) -> tuple[str | None, tuple[int, ...]]:
    events, action, verification = _events(doc), _facts(doc, "ACTION"), _facts(doc, "VERIFICATION")
    if events is None or action is None or verification is None:
        return "the reconstruction is unreadable", ()
    kinds = [k for _, k in events]
    counts = tuple(kinds.count(k) for k in COUNTED)
    for kind in ("GRANT_ISSUED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "VERDICT"):
        if kinds.count(kind) != 1:
            return f"{kinds.count(kind)} {kind} events, not exactly one", counts
    if not kinds.count("VERIFICATION_READ"):
        return "no VERIFICATION_READ: the outcome was not verified by key", counts
    at = {k: [seq for seq, kind in events if kind == k] for k in ("GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "VERIFICATION_READ", "VERDICT")}
    if not (at["GRANT_SPENT"][0] < at["DISPATCH_ATTEMPTED"][0] < at["RECEIPT"][0] < min(at["VERIFICATION_READ"]) < at["VERDICT"][0] and max(at["VERIFICATION_READ"]) < at["VERDICT"][0]):
        return "the events are not in order: spend, dispatch, receipt, the read by key, the verdict", counts
    if [f.get("execution_state") for f in action] != ["OUTCOME_UNKNOWN"]:
        return "the receipt is not classified OUTCOME_UNKNOWN (exactly one ACTION item)", counts
    if [f.get("verdict") for f in verification] != ["ACHIEVED"]:
        return "the key-verified verdict is not ACHIEVED (exactly one VERIFICATION item)", counts
    return None, counts


def _us01_problem(doc: Any) -> str | None:
    events, decision = _events(doc), _facts(doc, "DECISION")
    if events is None or decision is None:
        return "the reconstruction is unreadable"
    bad = sorted({k for _, k in events if k.startswith("GRANT_") or k == "DISPATCH_ATTEMPTED"})
    if bad:
        return f"a grant or a dispatch on US-01: {', '.join(bad)}"
    if not decision or any(f.get("decision_outcome") != "ALLOW" for f in decision):
        return "the decision is not ALLOW"
    return None


def _identity_problem(doc: Any, tenant: str, correlation: Any) -> str | None:
    """A reconstruction must be this run's tenant's and the case's own conversation (`correlations.json` of the run), or equal counts of another document would satisfy the row."""
    if not isinstance(doc, Mapping) or not _text_value(doc.get("tenant_id")) or not _text_value(doc.get("correlation_id")):
        return "the reconstruction carries no tenant or correlation id"
    if doc["tenant_id"] != tenant:
        return f"the reconstruction is of tenant {doc['tenant_id']!r}, not the run's"
    if doc["correlation_id"] != correlation:
        return f"the reconstruction is of correlation {doc['correlation_id']!r}, not the run's {correlation!r}"
    return None


def _line_stamp(text: Any) -> Mapping[str, Any] | None:
    first = text.split("\n", 1)[0] if isinstance(text, str) else ""
    parts = first.split(" ")
    return {"sha": parts[1], "run_id": parts[2]} if len(parts) == 3 and parts[0] == "stamp:" else None


def _stamps_across(inputs: Mapping[str, Any], artifacts: Mapping[str, Any]) -> tuple[bool, str]:
    """A517: one stamp on every artifact that carries one, compared with each other and with the inputs, so a mixed set names which artifact is of another dispatch or run."""
    found: dict[str, Any] = {}
    for name in ("reconstructions", "mock_case_writes", "probes"):
        doc = artifacts.get(name)
        found[name] = doc.get("stamp") if isinstance(doc, Mapping) else None
    found["run_stamp"] = artifacts.get("run_stamp")
    found["leak_scan"] = _line_stamp(artifacts.get("leak_scan"))
    found["artifact_files"] = _line_stamp(artifacts.get("artifact_files"))
    want = {"sha": inputs["dispatch_sha"], "run_id": inputs["run_id"]}
    off = sorted(n for n, st in found.items() if not isinstance(st, Mapping) or dict(st) != want)
    if off:
        return False, "no stamp, or another dispatch's or run's, on: " + ", ".join(off)
    return True, f"all {len(found)} artifacts carry the dispatch SHA and run id of the inputs"


def _canon(doc: Any) -> str:
    return json.dumps(doc, sort_keys=True, separators=(",", ":"))


def _row2(inputs: Mapping[str, Any], docs: Any, writes: Any, correlations: Any) -> tuple[bool, str]:
    if not isinstance(docs, Mapping) or not isinstance(writes, Mapping) or not isinstance(correlations, Mapping):
        return False, "the reconstructions, the mock-case write counts or the run's correlations are not readable"
    for case in [*inputs["us06_cases"], *inputs["us01_cases"]]:
        if not _text_value(correlations.get(case)):
            return False, f"{case}: the run's correlations.json names no correlation id for it"
    for case in inputs["us06_cases"]:
        phases = docs.get(case)
        if not isinstance(phases, Mapping) or not all(isinstance(phases.get(ph), Mapping) for ph in PHASES):
            return False, f"{case}: the reconstructions initially, after a recovery sweep and after a replay are all required"
        for ph in PHASES:
            identity = _identity_problem(phases[ph], inputs["tenant_id"], correlations.get(case))
            if identity:
                return False, f"{case} ({ph}): {identity}"
        if _canon(phases["after_recovery"]) != _canon(phases["after_replay"]):
            return False, f"{case}: after_replay is a second read of the same audit-only route (a re-read, not a replay) and it differs from the read after the recovery"
        results = {ph: _us06_problem(phases[ph]) for ph in PHASES}
        for ph in PHASES:
            if results[ph][0]:
                return False, f"{case} ({ph}): {results[ph][0]}"
        if len({results[ph][1] for ph in PHASES}) != 1:
            return False, f"{case}: the event counts differ after a recovery sweep or a replay"
        if writes.get(case) != 1 or type(writes.get(case)) is not int:
            return False, f"{case}: the mock-case log shows {writes.get(case)!r} writes, not exactly one"
    for case in inputs["us01_cases"]:
        phases = docs.get(case)
        problem = (_identity_problem(phases.get("initial"), inputs["tenant_id"], correlations.get(case)) or _us01_problem(phases.get("initial"))) if isinstance(phases, Mapping) else "no reconstruction"
        if problem:
            return False, f"{case}: {problem}"
        if writes.get(case) != 0 or type(writes.get(case)) is not int:
            return False, f"{case}: the mock-case log shows {writes.get(case)!r} writes, not zero"
    return True, f"computed from the reconstructions {_digest(docs)} and the mock-case write counts {_digest(writes)}"


def _row(n: int, inputs: Mapping[str, Any], a: Mapping[str, Any], b: Mapping[str, Any], replay: Mapping[str, Any], artifacts: Mapping[str, Any]) -> tuple[bool, str]:
    if n == 2:
        docs, p1 = _unwrap(artifacts.get("reconstructions"), "cases", inputs, "the reconstructions")
        writes, p2 = _unwrap(artifacts.get("mock_case_writes"), "writes", inputs, "the mock-case write counts")
        problem = p1 or p2 or _stamp_problem(artifacts.get("run_stamp"), inputs, "the run's stamp")
        return (False, problem) if problem else _row2(inputs, docs, writes, artifacts.get("correlations"))
    if n == 5:
        return _row5(inputs, replay, artifacts.get("check_runs"))
    if n == 8:
        return _row8(inputs, artifacts.get("leak_scan"))
    if n == 12:
        return _row12(inputs, artifacts.get("artifact_files"), artifacts.get("leak_scan"))
    if n == 9:
        return _row9(inputs, replay, artifacts.get("probes"), artifacts.get("run_stamp"), artifacts.get("correlations"))
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
    agree, agree_why = _stamps_across(inputs, artifacts)
    verdict = "MET" if all(met) and notes and agree else "NOT MET"
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
    lines += ["", "In row 2, `after_replay` is a second read of the same audit-only route after the recovery: a re-read of the Record, not a replay. Deterministic replay (INV-21) is not claimed from this evidence."]
    lines += ["", f"Run stamp across artifacts: **{'AGREE' if agree else 'DISAGREE'}** ({agree_why})."]
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
    for flag in ("inputs", "a-score", "b-score", "replay", "check-runs", "leak-scan", "probes", "reconstructions", "mock-case-writes", "correlations", "run-stamp", "out"):
        p.add_argument(f"--{flag}", required=True)
    p.add_argument("--artifact-files", help="the stamped list of the uploaded artifact files, one relative path per line (row 12)")
    args = p.parse_args(argv)
    try:
        inputs, a, b, replay, probes, reconstructions, writes, correlations, run_stamp = (json.loads(Path(getattr(args, f)).read_text(encoding="utf-8"))
                                                                                         for f in ("inputs", "a_score", "b_score", "replay", "probes", "reconstructions", "mock_case_writes", "correlations", "run_stamp"))
        pages = _json_documents(Path(args.check_runs).read_text(encoding="utf-8"))
        text = render(inputs, a, b, replay, {"check_runs": pages[0] if len(pages) == 1 else pages, "leak_scan": Path(args.leak_scan).read_text(encoding="utf-8"), "probes": probes,
                                  "reconstructions": reconstructions, "mock_case_writes": writes, "correlations": correlations,
                                  "run_stamp": run_stamp,
                                  "artifact_files": Path(args.artifact_files).read_text(encoding="utf-8") if args.artifact_files else None})
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
