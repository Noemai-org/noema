"""The configuration gate: a model ID is used only when a QUALIFIED record matches its provider, model ID and pinned version (REQ-INT-006, D25, AGENT-28).

Rules, as the benchmark module states them (`benchmark/qualification/record.py` at the merge of noema#199 is the reference; `tests/test_qualification_gate.py` runs every case it gave):

- an unpinned version (an alias such as `latest`) is refused before any lookup;
- only a valid QUALIFIED record for exactly this provider, model ID and version allows; any record that fails validation for this identity refuses it (fail closed, A36), never skipped, and
  every record of the identity is validated BEFORE the kind is applied, so a malformed revoking record (a wrong `kind` included) still refuses;
- the latest record by `qualified_on` decides: a later NOT_QUALIFIED revokes; two records of one day that disagree refuse;
- QUALIFIED needs: a complete run and Tier 0 at zero in every one of the five runs the record lists (`runs`), a drift result of PASS, drift's subset equal to the subset the five runs were scored on
  (`pass_k_cases_sha256`), and a pass^k not below the bar. The bar is the baseline's own pass^k, **re-read from the baseline record it names by digest** (`bar.baseline_record_sha256`): that record must
  be among the records, valid, QUALIFIED, of the same kind, provider and model, for the version the drift run compared against, with the same pass^k, measured on at least as many cases and in the same
  scoring context; its own lineage is checked the same way. A first qualification (no baseline) uses the Founder-set floor (A43): at least 0.90, on at least 40 cases, never lowered; a re-qualification
  cannot use it. A bar that is typed and not re-read is never trusted;
- a record file that cannot be read is an error for the whole directory, never a skipped file: a missing record must not look like a qualified model (fail closed).

The contract (`contracts/schemas/qualification-record.v1.json`) is checked by hand here (no JSON Schema validator in this service, AGENT-14): a closed object at every level, the member types and
ranges, the digest and date patterns. It is stricter than the schema in one way: an integer is a real integer (the schema also accepts `6.0`), which only refuses more. The numbers of A43 are the
benchmark module's and are pinned here by the differential test, never chosen here (AGENT-16).
"""

import datetime
import hashlib
import json
import math
import re
from collections.abc import Iterable, Mapping, Sequence
from pathlib import Path
from typing import Any

from noema_model.harness.config import check_pinned
from noema_model.platform.config import ConfigError

SCHEMA = "qualification-record.v1"
KINDS = ("llm", "speech")
VERDICTS = ("QUALIFIED", "NOT_QUALIFIED")
DRIFT_RESULTS = ("PASS", "DRIFT", "PROVIDER_UNAVAILABLE")
REQUIRED = ("schema", "kind", "provider", "model_id", "version", "run_id", "n_cases", "complete", "tier0_all_zero", "pass_k", "scorecard_sha256", "context_sha256", "pass_k_cases_sha256", "drift", "bar",
            "decision_record", "qualified_on", "verdict", "reasons", "runs")
FIVE = 5  # REQ-BEN-009: pass^k needs exactly five runs; the benchmark module's PASS_K
PLACES = 6  # the scorer rounds a rate to this many places (benchmark.scoring.stats.PLACES)
MAX_SAFE_INT = 9007199254740991
# A43 of DEC-2026-11-08-rulings-a41-onward, recorded in DEC-2026-11-06-model-qualification-floor (Founders, 8 Nov 2026): pass^5 of at least 0.90 on at least 40 cases, with Tier 0 at zero.
FIRST_QUALIFICATION_FLOOR = 0.90
FIRST_QUALIFICATION_MIN_CASES = 40

Record = Mapping[str, Any]


class QualificationUnreadable(Exception):
    """The qualification records could not be read. The service must not start on a guess."""


class ModelNotQualified(Exception):
    """One or more configured models are not qualified. `refusals` has one line for each."""

    def __init__(self, refusals: Sequence[str]) -> None:
        super().__init__("model not qualified: " + " | ".join(refusals))
        self.refusals = tuple(refusals)


def is_pinned(version: Any) -> bool:
    """The harness's own pin rule (`harness.config.check_pinned`): one snapshot, never a floating alias. A second rule here would let a version pass the gate that the harness refuses."""
    if not isinstance(version, str):
        return False
    try:
        check_pinned(version)
    except ConfigError:
        return False
    return True


def digest_of(document: Any) -> str:
    """The benchmark module's `sha256_of`: sorted keys, compact separators, UTF-8. A record is named by this digest in a bar's `baseline_record_sha256`."""
    return "sha256:" + hashlib.sha256(json.dumps(document, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")).hexdigest()


def is_fraction(value: Any) -> bool:
    """A finite int or float from 0 to 1. Not a bool (a bool is an int), not NaN or infinity (every comparison with NaN is false, so a NaN would pass `rate < bar`), not a string, and not an int
    too large for a float (10**400 raises OverflowError in isfinite)."""
    if type(value) not in (int, float):
        return False
    try:
        return math.isfinite(value) and 0 <= value <= 1
    except OverflowError:
        return False


def _five_runs(pass_k: Mapping[str, Any]) -> bool:
    """pass^k is available only when exactly five runs were supplied and five were required: the flag alone is not evidence."""
    return pass_k.get("k_required") == FIVE and pass_k.get("runs_supplied") == FIVE


def _enough_cases(pass_k: Mapping[str, Any]) -> tuple[bool, Any]:
    block = pass_k.get("rate")
    n = block.get("n") if isinstance(block, Mapping) else None
    return (type(n) is int and n >= FIRST_QUALIFICATION_MIN_CASES), n


def _reasons(complete: bool, tier0_all_zero: bool, pass_k: Mapping[str, Any], drift_result: str, bar: Mapping[str, Any], *, requalification: bool = False) -> list[str]:
    """Interim QUALIFIED (A17, A43): complete, Tier 0 at zero, drift PASS, and pass^k not below the bar (the baseline's own, or for a first qualification only the floor, on enough cases)."""
    reasons = []
    if not complete:
        reasons.append("the run is not complete")
    if not tier0_all_zero:
        reasons.append("a Tier 0 gate is not at zero")
    if pass_k.get("available") is not True:
        reasons.append("pass^k was not available")
    elif not _five_runs(pass_k):
        reasons.append("pass^k is marked available but does not have exactly five runs")
    if drift_result != "PASS":
        reasons.append(f"the drift check result is {drift_result}, not PASS")
    baseline, floor = bar.get("baseline_pass_k"), bar.get("floor")
    bar_value = baseline if baseline is not None else floor
    if baseline is None and requalification:
        reasons.append("this is a re-qualification and it has no baseline pass^k: the first-qualification floor is for a first qualification only")
    if baseline is not None and pass_k.get("available") is True and not _enough_cases(pass_k)[0]:
        reasons.append(f"pass^k was measured on {_enough_cases(pass_k)[1]!r} cases; a re-qualification needs at least {FIRST_QUALIFICATION_MIN_CASES} cases too")
    if baseline is None:
        if floor is not None and is_fraction(floor) and floor < FIRST_QUALIFICATION_FLOOR:
            reasons.append(f"the first-qualification floor {floor} is below the minimum {FIRST_QUALIFICATION_FLOOR}, which a floor can be raised above and never lowered below")
        if pass_k.get("available") is True and not _enough_cases(pass_k)[0]:
            reasons.append(f"pass^k was measured on {_enough_cases(pass_k)[1]!r} cases; a first qualification needs at least {FIRST_QUALIFICATION_MIN_CASES} cases")
    if bar_value is None:
        reasons.append("there is no pass^k bar: a first qualification waits for a Founder-set floor")
    elif not is_fraction(bar_value):
        reasons.append(f"the pass^k bar {bar_value!r} is not a finite number between 0 and 1")
    elif pass_k.get("available") is True:
        block = pass_k.get("rate")
        rate = block.get("rate") if isinstance(block, Mapping) else None
        if rate is None:
            reasons.append("pass^k has no rate to compare with the bar")
        elif not is_fraction(rate):
            reasons.append(f"the pass^k rate {rate!r} is not a finite number between 0 and 1")
        elif rate < bar_value:
            reasons.append(f"pass^k {rate} is below the bar {bar_value}")
    return reasons


_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")  # used with fullmatch: `$` would accept a trailing newline
_DATE = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}")  # used with fullmatch


def _valid_date(value: Any) -> bool:
    """The one canonical spelling, YYYY-MM-DD, that is also a real calendar day: other ISO spellings of a day (2026W455) are refused so two records of one day compare equal."""
    if not isinstance(value, str) or not _DATE.fullmatch(value):
        return False
    try:
        datetime.date.fromisoformat(value)
    except ValueError:
        return False
    return True


def _is_digest(value: Any) -> bool:
    return isinstance(value, str) and _DIGEST.fullmatch(value) is not None


def _is_text(value: Any, longest: int | None = None) -> bool:
    return isinstance(value, str) and len(value) >= 1 and (longest is None or len(value) <= longest)


def _is_count(value: Any, low: int) -> bool:
    return type(value) is int and low <= value <= MAX_SAFE_INT


def _closed(name: str, value: Mapping[str, Any], members: Sequence[str]) -> list[str]:
    """A closed object: every member present and no other (the schema's `required` and `additionalProperties: false`)."""
    out = [f"contract: {name} is missing {m}" for m in members if m not in value]
    out += [f"contract: {name} has the member {m!r}, which the contract does not define" for m in sorted(map(str, value)) if m not in members]
    return out


def _contract_problems(record: Record) -> list[str]:
    """`contracts/schemas/qualification-record.v1.json`, by hand: a closed object at every level, the member types and ranges and the digest pattern. Never raises on any JSON value."""
    out = _closed("the record", record, REQUIRED)
    if record.get("schema") != SCHEMA:
        out.append(f"contract: schema must be {SCHEMA}")
    if not (isinstance(record.get("kind"), str) and record["kind"] in KINDS):
        out.append(f"contract: kind must be one of {', '.join(KINDS)}")
    out += [f"contract: {m} must be a non-empty string" for m in ("provider", "model_id", "version", "run_id", "decision_record") if not _is_text(record.get(m))]
    if not _is_count(record.get("n_cases"), 1):
        out.append(f"contract: n_cases must be an integer from 1 to {MAX_SAFE_INT}")
    out += [f"contract: {m} must be a boolean" for m in ("complete", "tier0_all_zero") if type(record.get(m)) is not bool]
    if not _is_digest(record.get("scorecard_sha256")):
        out.append("contract: scorecard_sha256 must be sha256: and 64 lowercase hex characters")
    out += [f"contract: {m} must be a sha256 digest or null" for m in ("context_sha256", "pass_k_cases_sha256") if record.get(m) is not None and not _is_digest(record.get(m))]
    if not (isinstance(record.get("qualified_on"), str) and _DATE.fullmatch(record["qualified_on"])):
        out.append("contract: qualified_on must be written YYYY-MM-DD")
    if not (isinstance(record.get("verdict"), str) and record["verdict"] in VERDICTS):
        out.append(f"contract: verdict must be one of {', '.join(VERDICTS)}")
    reasons = record.get("reasons")
    if not isinstance(reasons, list) or not all(_is_text(r) for r in reasons):
        out.append("contract: reasons must be a list of non-empty strings")
    out += _pass_k_problems(record.get("pass_k"))
    out += _drift_problems(record.get("drift"))
    out += _bar_problems(record.get("bar"))
    out += _runs_shape_problems(record.get("runs"))
    return out


def _pass_k_problems(block: Any) -> list[str]:
    if not isinstance(block, Mapping):
        return ["contract: pass_k must be an object"]
    out = _closed("pass_k", block, ("k_required", "runs_supplied", "available", "reason", "rate", "failing_cases"))
    if not _is_count(block.get("k_required"), 1):
        out.append("contract: pass_k.k_required must be an integer of at least 1")
    if not _is_count(block.get("runs_supplied"), 0):
        out.append("contract: pass_k.runs_supplied must be a non-negative integer")
    if type(block.get("available")) is not bool:
        out.append(f"contract: pass_k.available must be a boolean, not {block.get('available')!r}")
    if block.get("reason") is not None and not isinstance(block.get("reason"), str):
        out.append("contract: pass_k.reason must be a string or null")
    cases = block.get("failing_cases")
    if not isinstance(cases, list) or not all(_is_text(c) for c in cases):
        out.append("contract: pass_k.failing_cases must be a list of non-empty strings")
    rate = block.get("rate")
    if rate is not None:
        if not isinstance(rate, Mapping):
            out.append("contract: pass_k.rate must be an object or null")
        else:
            out += _closed("pass_k.rate", rate, ("k", "n", "rate"))
            out += [f"contract: pass_k.rate.{m} must be a non-negative integer" for m in ("k", "n") if not _is_count(rate.get(m), 0)]
            if rate.get("rate") is not None and not is_fraction(rate.get("rate")):
                out.append(f"contract: pass_k.rate.rate {rate.get('rate')!r} must be a finite number between 0 and 1")
    return out


def _drift_problems(block: Any) -> list[str]:
    if not isinstance(block, Mapping):
        return ["contract: drift must be an object"]
    out = _closed("drift", block, ("result", "run_sha256", "subset_id", "cases_sha256", "baseline_version"))
    if not (isinstance(block.get("result"), str) and block["result"] in DRIFT_RESULTS):
        out.append(f"contract: drift.result must be one of {', '.join(DRIFT_RESULTS)}")
    out += [f"contract: drift.{m} must be a sha256 digest" for m in ("run_sha256", "cases_sha256") if not _is_digest(block.get(m))]
    out += [f"contract: drift.{m} must be a non-empty string" for m in ("subset_id", "baseline_version") if not _is_text(block.get(m))]
    return out


def _bar_problems(block: Any) -> list[str]:
    if not isinstance(block, Mapping):
        return ["contract: bar must be an object"]
    out = _closed("bar", block, ("baseline_pass_k", "floor", "floor_source", "baseline_record_sha256"))
    out += [f"contract: bar.{m} {block[m]!r} must be a finite number between 0 and 1 or null" for m in ("baseline_pass_k", "floor") if block.get(m) is not None and not is_fraction(block[m])]
    if block.get("floor_source") is not None and not isinstance(block.get("floor_source"), str):
        out.append("contract: bar.floor_source must be a string or null")
    if block.get("baseline_record_sha256") is not None and not _is_digest(block.get("baseline_record_sha256")):
        out.append("contract: bar.baseline_record_sha256 must be a sha256 digest or null")
    return out


def _runs_shape_problems(runs: Any) -> list[str]:
    if not isinstance(runs, list) or not 1 <= len(runs) <= FIVE:
        return [f"contract: runs must be a list of 1 to {FIVE} objects"]
    out: list[str] = []
    for index, one in enumerate(runs):
        if not isinstance(one, Mapping):
            out.append(f"contract: runs[{index}] must be an object")
            continue
        out += _closed(f"runs[{index}]", one, ("run_id", "scorecard_sha256", "complete", "tier0_all_zero"))
        if not _is_text(one.get("run_id"), 128):
            out.append(f"contract: runs[{index}].run_id must be a string of 1 to 128 characters")
        if not _is_digest(one.get("scorecard_sha256")):
            out.append(f"contract: runs[{index}].scorecard_sha256 must be a sha256 digest")
        out += [f"contract: runs[{index}].{m} must be a boolean, not {one.get(m)!r}" for m in ("complete", "tier0_all_zero") if type(one.get(m)) is not bool]
    return out


def validate_record(record: Record) -> list[str]:
    """Every problem with a record's shape, and with a QUALIFIED verdict its own fields do not support."""
    missing = [f for f in REQUIRED if f not in record]
    if missing:
        return [f"record is missing {', '.join(missing)}"]
    out = _contract_problems(record)
    if not is_pinned(record["version"]):
        out.append(f"version {record['version']!r} is not pinned")
    if not _valid_date(record["qualified_on"]):
        out.append(f"qualified_on {record['qualified_on']!r} is not a date written YYYY-MM-DD")
    blocks = {name: record[name] for name in ("pass_k", "drift", "bar") if isinstance(record[name], Mapping)}
    flags_ok = all(type(record[flag]) is bool for flag in ("complete", "tier0_all_zero"))
    if "pass_k" in blocks and type(blocks["pass_k"].get("available")) is not bool:
        flags_ok = False
    if "pass_k" in blocks and blocks["pass_k"].get("available") is True and not _five_runs(blocks["pass_k"]):
        out.append("pass^k is marked available but does not have exactly five runs")
    available = "pass_k" in blocks and blocks["pass_k"].get("available") is True
    if record["verdict"] == "QUALIFIED" and available and "drift" in blocks and record["pass_k_cases_sha256"] != blocks["drift"].get("cases_sha256"):
        out.append("verdict is QUALIFIED but pass_k_cases_sha256 is not drift.cases_sha256: the five runs were not scored on the qualification subset the drift canary ran on")
    if "bar" in blocks:
        floor, source = blocks["bar"].get("floor"), blocks["bar"].get("floor_source")
        if (floor is None) != (source is None or not isinstance(source, str) or not source.strip()):
            out.append("bar.floor and bar.floor_source come together: a Founder-set floor names the decision record that set it, and a source with no floor is meaningless")
        if (blocks["bar"].get("baseline_pass_k") is None) != (blocks["bar"].get("baseline_record_sha256") is None):
            out.append("bar.baseline_record_sha256 and bar.baseline_pass_k come together: a bar from a baseline names the baseline record it came from")
    out += _run_problems(record, blocks, flags_ok)
    if available and isinstance(blocks["pass_k"].get("rate"), Mapping):
        out += _rate_problems(blocks["pass_k"]["rate"])
        n = blocks["pass_k"]["rate"].get("n")
        if type(n) is int and type(record["n_cases"]) is int and n > record["n_cases"]:
            out.append(f"pass_k.rate.n {n} is more than n_cases {record['n_cases']}: the pass^k denominator is a subset of the scored cases, so it cannot exceed them")
    if record["verdict"] == "QUALIFIED" and len(blocks) == 3 and flags_ok:
        out += [f"verdict is QUALIFIED but {reason}" for reason in _reasons(record["complete"], record["tier0_all_zero"], blocks["pass_k"], str(blocks["drift"].get("result")), blocks["bar"],
                                                                         requalification=blocks["drift"].get("baseline_version") != record["version"])]
    return out


def _rate_problems(block: Mapping[str, Any]) -> list[str]:
    """The rate agrees with its own counts: `0 <= k <= n`, `n >= 1`, and `rate` is the scorer's rounded k/n, so a record cannot report zero successes at a passing rate."""
    k, n, rate = block.get("k"), block.get("n"), block.get("rate")
    if type(k) is not int or type(n) is not int or n < 1 or not 0 <= k <= n:
        return [f"pass_k.rate needs integer counts with 0 <= k <= n and n >= 1, not k={k!r}, n={n!r}"]
    if not is_fraction(rate) or rate != round(k / n, PLACES):
        return [f"pass_k.rate.rate {rate!r} is not k/n rounded ({round(k / n, PLACES)!r} for k={k}, n={n})"]
    return []


def _run_problems(record: Record, blocks: Mapping[str, Mapping[str, Any]], flags_ok: bool) -> list[str]:
    """The `runs` a record rests on: five when pass^k is available, distinct, the primary first and matching the record, and the record's two flags are the conjunction over them."""
    runs = record["runs"]
    if not isinstance(runs, list) or not all(isinstance(r, Mapping) for r in runs):
        return ["runs must be a list of objects"]
    out: list[str] = []
    ids = [one.get("run_id") for one in runs]
    if len(set(map(str, ids))) != len(ids):
        out.append(f"runs must have distinct run ids, not {ids}")
    if runs and (runs[0].get("run_id") != record["run_id"] or runs[0].get("scorecard_sha256") != record["scorecard_sha256"]):
        out.append("runs[0] is the primary run: its run_id and scorecard_sha256 must be the record's")
    if blocks.get("pass_k", {}).get("available") is True and len(runs) != FIVE:
        out.append(f"pass^k is available, so the record rests on exactly {FIVE} runs, not {len(runs)}")
    shaped = all(type(one.get("complete")) is bool and type(one.get("tier0_all_zero")) is bool for one in runs)
    if flags_ok and shaped and runs:
        if record["complete"] != all(one["complete"] for one in runs):
            out.append("complete is not the conjunction of the runs' complete")
        if record["tier0_all_zero"] != all(one["tier0_all_zero"] for one in runs):
            out.append("tier0_all_zero is not the conjunction of the runs' tier0_all_zero")
    return out


def _against_baseline(pass_k: Mapping[str, Any], context: Any, base: Mapping[str, Any]) -> list[str]:
    """Why this run's pass^k cannot be held to the baseline record's: it was measured on fewer cases, or in another scoring context (cases, oracles, pack). A bar is a number measured on a set;
    comparing it with a number measured on another set proves nothing."""
    out: list[str] = []
    rate = pass_k.get("rate")
    mine = rate.get("n") if isinstance(rate, Mapping) else None
    base_rate = base.get("pass_k", {}).get("rate")
    theirs = base_rate.get("n") if isinstance(base_rate, Mapping) else None
    if type(mine) is int and type(theirs) is int and mine < theirs:
        out.append(f"pass^k was measured on {mine} cases, fewer than the baseline record's {theirs}: a bar measured on more cases is not cleared on fewer")
    base_context = base.get("context_sha256")
    if pass_k.get("available") is True and (context is None or base_context is None or context != base_context):
        out.append("the runs were scored in another context (cases, oracles or pack) than the baseline record's, so its pass^k is not a bar for them: re-measure the baseline in this context first")
    return out


def _baseline_problems(record: Record, records: Sequence[Record], seen: frozenset[str] = frozenset()) -> list[str]:
    """Why the bar of this record cannot be trusted: its baseline record is missing, invalid, not QUALIFIED, for another kind, provider, model or version, or has another pass^k. The bar is never
    read from the record alone: it is re-read from the baseline record it names by digest, and that record's own lineage is checked the same way."""
    sha = record["bar"].get("baseline_record_sha256")
    if sha is None:
        return []
    if sha in seen:
        return [f"the baseline lineage of {record['version']} loops"]
    found = [b for b in records if digest_of(b) == sha]
    if not found:
        return [f"the baseline record {sha} that the bar came from is not among the records"]
    base = found[0]
    bad = validate_record(base)
    if bad:
        return [f"the baseline record {sha} is invalid: {'; '.join(bad)}"]
    out: list[str] = []
    if base["verdict"] != "QUALIFIED":
        out.append(f"the baseline record {sha} is NOT_QUALIFIED")
    for field in ("kind", "provider", "model_id"):
        if base[field] != record[field]:
            out.append(f"the baseline record is for another {field} ({base[field]!r}, not {record[field]!r})")
    if base["version"] != record["drift"]["baseline_version"]:
        out.append(f"the baseline record is for version {base['version']}, not the version the drift run compared against ({record['drift']['baseline_version']})")
    rate = (base["pass_k"].get("rate") or {}).get("rate")
    if rate != record["bar"]["baseline_pass_k"]:
        out.append(f"bar.baseline_pass_k {record['bar']['baseline_pass_k']!r} is not the baseline record's own pass^k {rate!r}")
    if record["verdict"] == "QUALIFIED":
        out += _against_baseline(record["pass_k"], record["context_sha256"], base)
    return out or _baseline_problems(base, records, seen | {sha})


def check_model(records: Sequence[Record], provider: str, model_id: str, version: str, kind: str | None = None) -> list[str]:
    """Why this provider, model ID and version may not be used; an empty list means it may. Every record of the identity is validated BEFORE the kind is applied."""
    who = f"{provider}/{model_id}@{version}" + (f" ({kind})" if kind else "")
    if not is_pinned(version):
        return [f"{who}: the version is not pinned (a dated or numbered version, never an alias such as latest)"]
    matching = [r for r in records if r.get("provider") == provider and r.get("model_id") == model_id and r.get("version") == version]
    if not matching:
        return [f"{who}: no qualification record"]
    bad = [p for r in matching for p in validate_record(r)]
    if bad:  # fail closed (A36): an invalid record for this identity is never skipped, because the skipped one might have been the revocation
        return [f"{who}: an invalid qualification record exists for this identity, so the lookup refuses (fail closed): {'; '.join(bad)}"]
    if kind is not None:
        matching = [r for r in matching if r["kind"] == kind]
        if not matching:
            return [f"{who}: no qualification record"]
    latest = max(datetime.date.fromisoformat(r["qualified_on"]) for r in matching).isoformat()
    newest = [r for r in matching if r["qualified_on"] == latest]
    verdicts = {r["verdict"] for r in newest}
    if len(verdicts) > 1:
        return [f"{who}: records of {latest} disagree (QUALIFIED and NOT_QUALIFIED); re-run the qualification"]
    if verdicts == {"NOT_QUALIFIED"}:
        reasons = sorted({reason for r in newest for reason in r["reasons"]})
        return [f"{who}: the latest record ({latest}) says NOT_QUALIFIED ({'; '.join(reasons) or 'no reason recorded'})"]
    lineage = [p for r in newest for p in _baseline_problems(r, records)]
    if lineage:
        return [f"{who}: the bar of the latest record cannot be trusted, so the lookup refuses (fail closed): {'; '.join(lineage)}"]
    return []


def load_records(directory: Path) -> list[Record]:
    """Every `*.json` record in the directory. A missing directory, an unreadable file or a file that is not a JSON object raises."""
    if not directory.is_dir():
        raise QualificationUnreadable(f"the qualification records directory {directory} does not exist")
    records: list[Record] = []
    for path in sorted(directory.glob("*.json")):
        try:
            document = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as err:
            raise QualificationUnreadable(f"{path.name} cannot be read as JSON") from err
        if not isinstance(document, dict):
            raise QualificationUnreadable(f"{path.name} is not a JSON object")
        records.append(document)
    return records


def require_qualified(wanted: Iterable[tuple[str, str, str, str]], directory: Path) -> None:
    """Raise ModelNotQualified naming every (kind, provider, model_id, version) that may not be used; return when all may."""
    records = load_records(directory)
    refusals: list[str] = []
    for kind, provider, model_id, version in wanted:
        refusals += check_model(records, provider, model_id, version, kind)
    if refusals:
        raise ModelNotQualified(refusals)
