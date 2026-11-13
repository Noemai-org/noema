"""Model qualification records (UND-13; REQ-INT-006, DEC-2026-10-31 D25, AGENT-28; ruling A17 of 6 Nov 2026).

A model or speech provider version is used only when a qualification record exists for exactly that provider, model ID and pinned version. A record is made from the scorecard
of a run of the qualification subset that includes pass^k (exactly five runs, REQ-BEN-009) and from the drift run record (`run.py`). The format is
`contracts/schemas/qualification-record.v1.json`. This module is pure: it reads no file, runs nothing and sets no threshold.

Interim QUALIFIED (A17) means all of:

- the run is complete;
- every Tier 0 gate is at zero;
- the drift check result is PASS;
- pass^k is not below the bar. The bar is the pinned baseline's own pass^k (A17's wording, A43 of DEC-2026-11-08-rulings-a41-onward dropped "the higher of"); only a first
  qualification, which has no baseline and whose drift run compares the version with itself, uses the floor. With neither there is no bar and the verdict is NOT_QUALIFIED.
- a bar that comes from a baseline names that baseline's record by digest (`bar.baseline_record_sha256`), and the lookup re-reads it (`check_model`): a typed bar is never trusted.
- the record lists the five runs it rests on (`runs`), each with its scorecard digest, and Tier 0 and completeness hold in every one.

The model service's configuration check (C2) refuses an ID with no QUALIFIED record by the same rules as `check_model`; it is not built here.

Generated layer only; the held-out set is private (OD-16, INV-22). SYNTHETIC in tests."""
import functools
import math
import re
from collections.abc import Mapping, Sequence
from typing import Any

from benchmark.contracts import Contracts, sha256_of
from benchmark.qualification.pins import KINDS, is_pinned, parse_date
from benchmark.qualification.run import validate_run_record
from benchmark.scoring.diagnostics import PASS_K
from benchmark.scoring.stats import PLACES

Json = dict[str, Any]
DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
SCHEMA = "qualification-record.v1"
VERDICTS = ("QUALIFIED", "NOT_QUALIFIED")
# A43 of DEC-2026-11-08-rulings-a41-onward, recorded in DEC-2026-11-06-model-qualification-floor (Founders, 8 Nov 2026, noema#312): the first-qualification floor is pass^5 of at
# least 0.90 on at least 40 cases, with Tier 0 at zero. A floor can be raised and never lowered.
FIRST_QUALIFICATION_FLOOR = 0.90
FIRST_QUALIFICATION_MIN_CASES = 40
CITE = "A43 of DEC-2026-11-08-rulings-a41-onward"
REQUIRED = ("schema", "kind", "provider", "model_id", "version", "run_id", "n_cases", "complete", "tier0_all_zero", "pass_k", "scorecard_sha256", "context_sha256", "drift", "bar", "decision_record",
            "qualified_on", "verdict", "reasons", "runs")


@functools.lru_cache(maxsize=1)
def _contracts() -> Contracts:
    return Contracts()


def _five_runs(pass_k: Mapping[str, Any]) -> bool:
    """pass^k is available only when exactly five runs were supplied and five were required (REQ-BEN-009): the flag alone is not evidence."""
    return pass_k.get("k_required") == PASS_K and pass_k.get("runs_supplied") == PASS_K


def _reasons(complete: bool, tier0_all_zero: bool, pass_k: Mapping[str, Any], drift_result: str, bar: Mapping[str, Any], *, requalification: bool = False) -> list[str]:
    reasons = []
    if not complete:
        reasons.append("the run is not complete")
    if not tier0_all_zero:
        reasons.append("a Tier 0 gate is not at zero")
    if pass_k.get("available") is not True:
        reasons.append(f"pass^k was not available ({pass_k.get('reason') or 'no reason given'})")
    elif not _five_runs(pass_k):
        reasons.append(f"pass^k is marked available but k_required is {pass_k.get('k_required')!r} and runs_supplied is {pass_k.get('runs_supplied')!r}; it needs exactly five runs")
    if drift_result != "PASS":
        reasons.append(f"the drift check result is {drift_result}, not PASS")
    baseline, floor = bar.get("baseline_pass_k"), bar.get("floor")
    bar_value = baseline if baseline is not None else floor
    if baseline is None and requalification:
        reasons.append(f"this is a re-qualification (the drift run compares the version with another version) and it has no baseline pass^k: the first-qualification floor is for a first qualification only ({CITE})")
    if baseline is not None and pass_k.get("available") is True:
        block = pass_k.get("rate")
        n = block.get("n") if isinstance(block, Mapping) else None
        if type(n) is not int or n < FIRST_QUALIFICATION_MIN_CASES:
            reasons.append(f"pass^k was measured on {n!r} cases; a re-qualification needs at least {FIRST_QUALIFICATION_MIN_CASES} cases too, so a tiny run cannot clear a baseline measured on more (C2 on noema#199)")
    if baseline is None:
        if floor is not None and is_fraction(floor) and floor < FIRST_QUALIFICATION_FLOOR:
            reasons.append(f"the first-qualification floor {floor} is below the minimum {FIRST_QUALIFICATION_FLOOR}, which a floor can be raised above and never lowered below ({CITE})")
        if pass_k.get("available") is True:
            block = pass_k.get("rate")
            n = block.get("n") if isinstance(block, Mapping) else None
            if type(n) is not int or n < FIRST_QUALIFICATION_MIN_CASES:
                reasons.append(f"pass^k was measured on {n!r} cases; a first qualification needs at least {FIRST_QUALIFICATION_MIN_CASES} cases ({CITE})")
    if bar_value is None:
        reasons.append("there is no pass^k bar: a first qualification waits for a Founder-set floor (A17)")
    elif not is_fraction(bar_value):
        reasons.append(f"the pass^k bar {bar_value!r} is not a finite number between 0 and 1")
    elif pass_k.get("available") is True:
        rate_block = pass_k.get("rate")
        rate = rate_block.get("rate") if isinstance(rate_block, Mapping) else None
        if rate is None:
            reasons.append("pass^k has no rate to compare with the bar")
        elif not is_fraction(rate):
            reasons.append(f"the pass^k rate {rate!r} is not a finite number between 0 and 1")
        elif rate < bar_value:
            reasons.append(f"pass^k {rate} is below the bar {bar_value}")
    return reasons


def is_fraction(value: Any) -> bool:
    """A finite int or float from 0 to 1. A bool is an int, NaN fails every comparison, and a string is not a number: none of them is a rate (C2 on noema#199)."""
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        return False
    try:
        return math.isfinite(value) and 0 <= value <= 1
    except OverflowError:  # an int too large for a float (10**400) is not a rate
        return False


def _fraction(name: str, value: float | None) -> None:
    if value is not None and not is_fraction(value):
        raise ValueError(f"{name} must be a finite number between 0 and 1, not {value!r}")


def build_record(card: Mapping[str, Any], drift: Mapping[str, Any], *, kind: str, provider: str, model_id: str, version: str, decision_record: str, qualified_on: str,
                 baseline_pass_k: float | None, floor: float | None, floor_source: str | None, baseline_record_sha256: str | None = None,
                 repeat_cards: Sequence[Mapping[str, Any]] = (), provenance: Mapping[str, Any] | None = None,
                 repeat_provenance: Sequence[Mapping[str, Any]] = (), baseline_record: Mapping[str, Any] | None = None) -> Json:
    """A record whose numbers are copied from the scorecard and the run record, so an author never types one.

    `card` is the primary run. pass^k needs exactly five runs, and Tier 0 and completeness must hold in every one of them (Codex P1 on noema#199), so the other four runs'
    scorecards come in `repeat_cards`: five distinct run ids, each with real boolean `complete` and `tier0.all_zero`. `complete` and `tier0_all_zero` of the record are the
    conjunction over the five, and each failing run is named in `reasons`. `runs` lists the five with their scorecard digests (SR N2). A bar from a baseline names the baseline's
    record by digest, `baseline_record_sha256`, set exactly when `baseline_pass_k` is (SR N1)."""
    if kind not in KINDS:
        raise ValueError(f"kind must be one of {', '.join(KINDS)}, not {kind!r}")
    for name, value in (("provider", provider), ("model_id", model_id), ("decision_record", decision_record)):
        if not value or value != value.strip():
            raise ValueError(f"{name} must be a non-empty string without surrounding space")
    if not is_pinned(version):
        raise ValueError(f"version {version!r} is not pinned: a qualification record names a dated or numbered version, never an alias such as latest")
    parse_date(qualified_on)
    _fraction("baseline_pass_k", baseline_pass_k)
    _fraction("floor", floor)
    if (floor is None) != (floor_source is None or not floor_source.strip()):
        raise ValueError("a floor and the decision record that set it (floor_source) come together")
    if (baseline_pass_k is None) != (baseline_record_sha256 is None):
        raise ValueError("a baseline pass^k and the digest of the baseline record it came from (baseline_record_sha256) come together")
    if baseline_record_sha256 is not None and not DIGEST.fullmatch(baseline_record_sha256):
        raise ValueError(f"baseline_record_sha256 {baseline_record_sha256!r} is not a digest written sha256:<64 lowercase hex>")
    if baseline_record is not None and baseline_record_sha256 != sha256_of(baseline_record):
        raise ValueError("the baseline record given is not the record baseline_record_sha256 names (its digest differs)")
    problems = validate_run_record(drift)
    if problems:
        raise ValueError(f"the drift run record is invalid: {'; '.join(problems)}")
    if drift["pinned"] != {"kind": kind, "provider": provider, "model_id": model_id, "version": version}:
        raise ValueError(f"the drift run record is for {drift['pinned']!r}, not for {provider}/{model_id}@{version} ({kind})")
    source = card["tier1"]["diagnostics"]["pass_k"]
    for flag, value in (("complete", card["complete"]), ("tier0_all_zero", card["tier0"]["all_zero"]), ("pass_k.available", source["available"])):
        if type(value) is not bool:
            raise ValueError(f"the scorecard's {flag} must be a real boolean, not {value!r}")
    if source["available"] is True and len(repeat_cards) != PASS_K - 1:
        raise ValueError(f"pass^k over {PASS_K} runs needs the scorecards of the {PASS_K - 1} other runs (repeat_cards), not {len(repeat_cards)}: Tier 0 is checked in every run")
    if source["available"] is True:
        _bind_runs(card, repeat_cards, provenance, repeat_provenance, {"kind": kind, "provider": provider, "model_id": model_id, "version": version})
    if source["available"] is True and isinstance(source.get("rate"), Mapping) and type(source["rate"].get("n")) is int and source["rate"]["n"] > card["n_cases"]:
        raise ValueError(f"the scorecard's pass^k denominator {source['rate']['n']} is more than its n_cases {card['n_cases']}: it is a subset of the scored cases and cannot exceed them")
    run_ids = [card["run_id"], *(c["run_id"] for c in repeat_cards)]
    if len(set(run_ids)) != len(run_ids):
        raise ValueError(f"the runs must have distinct run ids, not {run_ids}")
    run_reasons: list[str] = []
    for one in (card, *repeat_cards):
        for flag, value in (("complete", one["complete"]), ("tier0_all_zero", one["tier0"]["all_zero"])):
            if type(value) is not bool:
                raise ValueError(f"run {one['run_id']}: the scorecard's {flag} must be a real boolean, not {value!r}")
        if not one["complete"]:
            run_reasons.append(f"run {one['run_id']} is not complete")
        if not one["tier0"]["all_zero"]:
            run_reasons.append(f"run {one['run_id']}: a Tier 0 gate is not at zero")
    all_complete = all(c["complete"] for c in (card, *repeat_cards))
    all_zero = all(c["tier0"]["all_zero"] for c in (card, *repeat_cards))
    rate = source.get("rate")
    pass_k = {"k_required": source["k_required"], "runs_supplied": source["runs_supplied"], "available": source["available"], "reason": source.get("reason"),
              "rate": None if rate is None else {"k": rate["k"], "n": rate["n"], "rate": rate["rate"]}, "failing_cases": list(source.get("failing_cases") or [])}
    bar = {"baseline_pass_k": baseline_pass_k, "floor": floor, "floor_source": floor_source, "baseline_record_sha256": baseline_record_sha256}
    runs = [{"run_id": one["run_id"], "scorecard_sha256": sha256_of(one), "complete": one["complete"], "tier0_all_zero": one["tier0"]["all_zero"]} for one in (card, *repeat_cards)]
    drift_block = {"result": drift["result"], "run_sha256": sha256_of(drift), "subset_id": drift["subset_id"], "cases_sha256": drift["cases_sha256"],
                   "baseline_version": drift["baseline"]["pinned"]["version"]}
    context = provenance["context_sha256"] if source["available"] is True and provenance is not None else None
    reasons = run_reasons + _reasons(all_complete, all_zero, pass_k, drift["result"], bar, requalification=drift_block["baseline_version"] != version)
    if baseline_record is not None:
        reasons += _against_baseline(pass_k, context, baseline_record)
    return {
        "schema": SCHEMA, "kind": kind, "provider": provider, "model_id": model_id, "version": version, "run_id": card["run_id"], "n_cases": card["n_cases"],
        "complete": all_complete, "tier0_all_zero": all_zero, "pass_k": pass_k, "scorecard_sha256": sha256_of(card), "context_sha256": context, "drift": drift_block,
        "bar": bar, "decision_record": decision_record, "qualified_on": qualified_on, "verdict": "NOT_QUALIFIED" if reasons else "QUALIFIED", "reasons": reasons,
        "runs": runs,
    }


def _against_baseline(pass_k: Mapping[str, Any], context: Any, base: Mapping[str, Any]) -> list[str]:
    """Why this run's pass^k cannot be held to the baseline record's: it was measured on fewer cases, or in another scoring context (cases, oracles, pack). A bar is a number measured on a
    set; comparing it with a number measured on another set proves nothing (C2 on noema#199: a one-case run cleared a 40-case baseline)."""
    out: list[str] = []
    mine = (pass_k.get("rate") or {}).get("n") if isinstance(pass_k.get("rate"), Mapping) else None
    theirs = (base.get("pass_k", {}).get("rate") or {}).get("n") if isinstance(base.get("pass_k", {}).get("rate"), Mapping) else None
    if type(mine) is int and type(theirs) is int and mine < theirs:
        out.append(f"pass^k was measured on {mine} cases, fewer than the baseline record's {theirs}: a bar measured on more cases is not cleared on fewer")
    base_context = base.get("context_sha256")
    if pass_k.get("available") is True and (context is None or base_context is None or context != base_context):
        out.append("the runs were scored in another context (cases, oracles or pack) than the baseline record's, so its pass^k is not a bar for them: re-measure the baseline in this context first")
    return out


def _bind_runs(card: Mapping[str, Any], repeat_cards: Sequence[Mapping[str, Any]], provenance: Mapping[str, Any] | None, repeat_provenance: Sequence[Mapping[str, Any]],
               identity: Mapping[str, str]) -> None:
    """The repeat scorecards are the runs pass^k rests on (Codex P1 on noema#199): the primary's provenance lists the five runs with their result digests, each repeat has its own
    provenance, and every scorecard is the one its provenance was written beside. Four unrelated clean scorecards fail here, so their safety flags are never read."""
    if provenance is None:
        raise ValueError("pass^k over five runs needs the provenance of the primary run (provenance.json, written beside its scorecard): without it the repeat scorecards are not bound to the runs behind pass^k")
    for one in (provenance, *repeat_provenance):
        problems = _contracts().errors("benchmark-run-provenance", one)
        if problems:
            raise ValueError(f"a provenance file is invalid: {problems[0]}")
    for one in (provenance, *repeat_provenance):
        declared = one["pinned"]
        if declared is None:
            raise ValueError(f"run {one['run_id']!r} was scored without a declared model identity (--pinned-* flags): its pass^k and safety evidence cannot be bound to {identity['provider']}/{identity['model_id']}@{identity['version']}")
        for field in ("kind", "provider", "model_id", "version"):
            if declared[field] != identity[field]:
                raise ValueError(f"run {one['run_id']!r} was scored for {field} {declared[field]!r}, not {identity[field]!r}: another model's evidence cannot qualify this one")
    if provenance["run_id"] != card["run_id"] or provenance["scorecard_sha256"] != sha256_of(card):
        raise ValueError("the primary provenance is not for the primary scorecard (run_id or scorecard_sha256 differs): the scorecard was edited or is another run's")
    listed = provenance["pass_k_runs"]
    ids = [r["run_id"] for r in listed] if isinstance(listed, list) else []
    if len(ids) != PASS_K or len(set(ids)) != PASS_K:
        raise ValueError(f"the primary provenance must list exactly five distinct runs for pass^k, not {ids}")
    digests = {r["run_id"]: r["results_sha256"] for r in listed}
    if digests.get(card["run_id"]) != provenance["results_sha256"]:
        raise ValueError("the primary provenance lists a results_sha256 for its own run that is not its own")
    for other in repeat_provenance:
        if other["context_sha256"] != provenance["context_sha256"]:
            raise ValueError(f"repeat run {other['run_id']!r} was scored against another case set, oracle set or pack: its context_sha256 is not the primary's, so its safety flags are not comparable")
    by_run = {r["run_id"]: r for r in repeat_provenance}
    for one in repeat_cards:
        run_id = one["run_id"]
        if run_id not in digests or run_id == card["run_id"]:
            raise ValueError(f"repeat scorecard {run_id!r} is not one of the other four runs pass^k rests on ({ids})")
        found = by_run.get(run_id)
        if found is None:
            raise ValueError(f"repeat scorecard {run_id!r} has no provenance")
        if found["scorecard_sha256"] != sha256_of(one):
            raise ValueError(f"repeat scorecard {run_id!r}: its provenance's scorecard_sha256 is not this scorecard's digest (edited after scoring)")
        if found["results_sha256"] != digests[run_id]:
            raise ValueError(f"repeat scorecard {run_id!r}: its results_sha256 is not the digest the primary's pass^k listed for that run")


def validate_record(record: Mapping[str, Any]) -> list[str]:
    """Every problem with a record's shape and with a QUALIFIED verdict its own fields do not support."""
    missing = [f for f in REQUIRED if f not in record]
    if missing:
        return [f"record is missing {', '.join(missing)}"]
    out: list[str] = [f"contract: {e}" for e in _contracts().errors("qualification-record", record)]
    if record["schema"] != SCHEMA:
        out.append(f"schema must be {SCHEMA}, not {record['schema']!r}")
    if record["kind"] not in KINDS:
        out.append(f"kind must be one of {', '.join(KINDS)}, not {record['kind']!r}")
    for name in ("provider", "model_id", "decision_record"):
        if not isinstance(record[name], str) or not record[name].strip():
            out.append(f"{name} must be a non-empty string")
    if not is_pinned(record["version"]):
        out.append(f"version {record['version']!r} is not pinned")
    try:
        parse_date(record["qualified_on"])
    except ValueError:
        out.append(f"qualified_on {record['qualified_on']!r} is not a date written YYYY-MM-DD")
    blocks = {name: record[name] for name in ("pass_k", "drift", "bar") if isinstance(record[name], Mapping)}
    out += [f"{name} must be an object" for name in ("pass_k", "drift", "bar") if name not in blocks]
    flags_ok = True
    for flag in ("complete", "tier0_all_zero"):
        if type(record[flag]) is not bool:
            out.append(f"{flag} must be a boolean, not {record[flag]!r}")
            flags_ok = False
    if "pass_k" in blocks and blocks["pass_k"].get("available") is True and not _five_runs(blocks["pass_k"]):
        out.append(f"pass^k is marked available but k_required is {blocks['pass_k'].get('k_required')!r} and runs_supplied is {blocks['pass_k'].get('runs_supplied')!r}; it needs exactly five runs")
    if "pass_k" in blocks and type(blocks["pass_k"].get("available")) is not bool:
        out.append(f"pass_k.available must be a boolean, not {blocks['pass_k'].get('available')!r}")
        flags_ok = False
    if record["context_sha256"] is not None and (not isinstance(record["context_sha256"], str) or not DIGEST.fullmatch(record["context_sha256"])):
        out.append("context_sha256 is not a sha256 digest written sha256:<64 lowercase hex>, or null")
    for name, value in (("scorecard_sha256", record["scorecard_sha256"]), *(("drift." + k, blocks["drift"].get(k)) for k in ("run_sha256", "cases_sha256") if "drift" in blocks and k in blocks["drift"])):
        if not isinstance(value, str) or not DIGEST.fullmatch(value):
            out.append(f"{name} is not a sha256 digest written sha256:<64 lowercase hex>")
    if "bar" in blocks:
        for field in ("baseline_pass_k", "floor"):
            if blocks["bar"].get(field) is not None and not is_fraction(blocks["bar"][field]):
                out.append(f"bar.{field} {blocks['bar'][field]!r} must be a finite number between 0 and 1")
        floor, source = blocks["bar"].get("floor"), blocks["bar"].get("floor_source")
        if (floor is None) != (source is None or not isinstance(source, str) or not source.strip()):
            out.append("bar.floor and bar.floor_source come together: a Founder-set floor names the decision record that set it, and a source with no floor is meaningless")
        sha = blocks["bar"].get("baseline_record_sha256")
        if (blocks["bar"].get("baseline_pass_k") is None) != (sha is None):
            out.append("bar.baseline_record_sha256 and bar.baseline_pass_k come together: a bar from a baseline names the baseline record it came from")
        if sha is not None and (not isinstance(sha, str) or not DIGEST.fullmatch(sha)):
            out.append("bar.baseline_record_sha256 is not a sha256 digest written sha256:<64 lowercase hex>")
    out += _run_problems(record, blocks, flags_ok)
    if "pass_k" in blocks and blocks["pass_k"].get("available") is True and isinstance(blocks["pass_k"].get("rate"), Mapping):
        rate = blocks["pass_k"]["rate"].get("rate")
        if rate is not None and not is_fraction(rate):
            out.append(f"pass_k.rate.rate {rate!r} must be a finite number between 0 and 1")
        out += _rate_problems(blocks["pass_k"]["rate"])
        n = blocks["pass_k"]["rate"].get("n")
        if type(n) is int and type(record["n_cases"]) is int and n > record["n_cases"]:
            out.append(f"pass_k.rate.n {n} is more than n_cases {record['n_cases']}: the pass^k denominator is a subset of the scored cases, so it cannot exceed them")
    if not isinstance(record["reasons"], list) or not all(isinstance(r, str) for r in record["reasons"]):
        out.append("reasons must be a list of strings")
    if record["verdict"] not in VERDICTS:
        out.append(f"verdict must be one of {', '.join(VERDICTS)}, not {record['verdict']!r}")
    elif record["verdict"] == "QUALIFIED" and len(blocks) == 3 and flags_ok:
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


def _run_problems(record: Mapping[str, Any], blocks: Mapping[str, Mapping[str, Any]], flags_ok: bool) -> list[str]:
    """The `runs` a record rests on: five when pass^k is available, distinct, the primary first and matching the record, and the record's two flags are the conjunction over them."""
    runs = record["runs"]
    if not isinstance(runs, list) or not all(isinstance(r, Mapping) for r in runs):
        return ["runs must be a list of objects"]
    out: list[str] = []
    for index, one in enumerate(runs):
        for key in ("run_id", "scorecard_sha256", "complete", "tier0_all_zero"):
            if key not in one:
                out.append(f"runs[{index}] is missing {key}")
        if "scorecard_sha256" in one and (not isinstance(one["scorecard_sha256"], str) or not DIGEST.fullmatch(one["scorecard_sha256"])):
            out.append(f"runs[{index}].scorecard_sha256 is not a sha256 digest written sha256:<64 lowercase hex>")
        for key in ("complete", "tier0_all_zero"):
            if key in one and type(one[key]) is not bool:
                out.append(f"runs[{index}].{key} must be a boolean, not {one[key]!r}")
    ids = [one.get("run_id") for one in runs]
    if len(set(map(str, ids))) != len(ids):
        out.append(f"runs must have distinct run ids, not {ids}")
    if runs and (runs[0].get("run_id") != record["run_id"] or runs[0].get("scorecard_sha256") != record["scorecard_sha256"]):
        out.append("runs[0] is the primary run: its run_id and scorecard_sha256 must be the record's")
    if blocks.get("pass_k", {}).get("available") is True and len(runs) != PASS_K:
        out.append(f"pass^k is available, so the record rests on exactly {PASS_K} runs, not {len(runs)}")
    if flags_ok and not out and runs:
        if record["complete"] != all(one["complete"] for one in runs):
            out.append("complete is not the conjunction of the runs' complete")
        if record["tier0_all_zero"] != all(one["tier0_all_zero"] for one in runs):
            out.append("tier0_all_zero is not the conjunction of the runs' tier0_all_zero")
    return out


def verify_against_scorecard(record: Mapping[str, Any], card: Mapping[str, Any]) -> list[str]:
    """The record still says what the scorecard it was made from says."""
    out = []
    if record.get("scorecard_sha256") != sha256_of(card):
        out.append("the scorecard digest does not match the record's scorecard_sha256 (the scorecard or the record was edited)")
    if record.get("run_id") != card.get("run_id"):
        out.append(f"run_id {record.get('run_id')!r} is not the scorecard's {card.get('run_id')!r}")
    return out


def _baseline_problems(record: Mapping[str, Any], records: Sequence[Mapping[str, Any]], seen: frozenset[str] = frozenset()) -> list[str]:
    """Why the bar of this record cannot be trusted: its baseline record is missing, invalid, not QUALIFIED, for another kind, provider, model or version, or has another pass^k.

    The bar is never read from the record alone (SR N1 on noema#199): it is re-read from the baseline record it names by digest, and that record's own lineage is checked the same way."""
    sha = record["bar"].get("baseline_record_sha256")
    if sha is None:
        return []
    if sha in seen:
        return [f"the baseline lineage of {record['version']} loops"]
    found = [b for b in records if sha256_of(b) == sha]
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


def check_model(records: Sequence[Mapping[str, Any]], provider: str, model_id: str, version: str, kind: str | None = None) -> list[str]:
    """Why this provider, model ID and version may not be used; an empty list means it may.

    Only a valid QUALIFIED record for exactly this provider, model ID and version allows. The latest record decides: a later failed re-qualification revokes an earlier
    qualification, and two records of one day that disagree refuse. Every record of the identity is validated BEFORE the kind is applied, so a malformed revoking record (a wrong
    `kind` included) still refuses (EL on noema#199). A bar that came from a baseline is re-read from the baseline record it names, which must be among `records`."""
    who = f"{provider}/{model_id}@{version}" + (f" ({kind})" if kind else "")
    if not is_pinned(version):
        return [f"{who}: the version is not pinned (a dated or numbered version, never an alias such as latest)"]
    matching = [r for r in records if r.get("provider") == provider and r.get("model_id") == model_id and r.get("version") == version]
    if not matching:
        return [f"{who}: no qualification record"]
    bad = [p for r in matching for p in validate_record(r)]
    if bad:
        return [f"{who}: an invalid qualification record exists for this identity, so the lookup refuses (fail closed): {'; '.join(bad)}"]
    if kind is not None:
        matching = [r for r in matching if r["kind"] == kind]
        if not matching:
            return [f"{who}: no qualification record"]
    latest = max(parse_date(r["qualified_on"]) for r in matching)
    newest = [r for r in matching if parse_date(r["qualified_on"]) == latest]
    verdicts = {r["verdict"] for r in newest}
    if len(verdicts) > 1:
        return [f"{who}: records of {latest.isoformat()} disagree (QUALIFIED and NOT_QUALIFIED); re-run the qualification"]
    if verdicts == {"NOT_QUALIFIED"}:
        reasons = sorted({reason for r in newest for reason in r["reasons"]})
        return [f"{who}: the latest record ({latest.isoformat()}) says NOT_QUALIFIED ({'; '.join(reasons) or 'no reason recorded'})"]
    lineage = [p for r in newest for p in _baseline_problems(r, records)]
    if lineage:
        return [f"{who}: the bar of the latest record cannot be trusted, so the lookup refuses (fail closed): {'; '.join(lineage)}"]
    return []
