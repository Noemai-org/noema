"""Publication rules as a check (BEN-31; REQ-BEN-010, DEC-2026-10-31 D28; SoT §38.8, G7).

A publication is a set of CLAIMS plus the DISTRIBUTIONS they sit in. `check` is a pure function of a publication and the evidence bundle (scorecards by run ID) and returns every
problem it finds, sorted. It decides nothing about whether a number is good; it enforces that no external number can be stated without its evidence:

- every claim names a run ID that has a scorecard in the bundle, and a JSON pointer to a rate in that scorecard; its k, n, rate and interval must equal the scorecard's;
- the method is the one the scorer used (Wilson 95%), a zero-failure rate carries the scorer's rule-of-three bound, and every number written in the claim's text is one of the claim's own;
- outcomes are judged against the final state of the systems (`ORACLE_FINAL_STATE`), and the Tier 0 status of the run is stated and equals the scorecard's;
- the run is complete; a run that is not a release result says so in the claim's population; pass^k is publishable only when it was available (exactly five runs);
- the text avoids the words the release rules forbid (claims.md section 3);
- every run cited has a Tier 0 claim, listed before any other claim of that run, and the FULL distribution (every family and every category the scorecard has, with matching counts) is present: nothing is cherry-picked.

Generated layer only; the held-out set is private (OD-16, INV-22). SYNTHETIC in tests."""
import re
from collections.abc import Mapping
from typing import Any

Json = dict[str, Any]
METHOD = "Wilson 95% interval"
OUTCOME_BASIS = "ORACLE_FINAL_STATE"
REQUIRED = ("id", "text", "run_id", "scorecard_ref", "k", "n", "rate", "ci95", "method", "population", "outcome_basis", "tier0_all_zero")
SLICES = ("by_family", "by_category")
_STRIPPED = re.compile(r"\bTier [0-3]\b|\b95\s?%|\bk\s?=\s?5\b|pass\^k")
_NUMBER = re.compile(r"(?<![\w.-])\d[\d,]*(?:\.\d+)?(?![\w-])")


def resolve(card: Json, ref: str) -> Any:
    """A JSON pointer into a scorecard (`/tier1/correct_terminal_handling`, `/tier0/gates/0/rate`)."""
    node: Any = card
    for part in [p for p in ref.split("/") if p]:
        if isinstance(node, list) and part.isdigit() and int(part) < len(node):
            node = node[int(part)]
        elif isinstance(node, dict) and part in node:
            node = node[part]
        else:
            raise KeyError(ref)
    return node


def _is_rate(node: Any) -> bool:
    return isinstance(node, dict) and {"k", "n", "rate", "wilson95", "rule_of_three_upper"} <= set(node)


def population_for(card: Json) -> str:
    layers = ", ".join(f"{count} {layer.lower().replace('_', '-')}" for layer, count in card["layers"].items() if count)
    return f"{card['n_cases']} cases scored in run {card['run_id']} ({layers})" + ("" if card["release_result"] else "; not a release result")


def build_claim(card: Json, claim_id: str, ref: str, text: str) -> Json:
    """A claim whose numbers are copied from the scorecard, so an author never types one."""
    rate = resolve(card, ref)
    if not _is_rate(rate):
        raise ValueError(f"{ref} is not a rate in the scorecard")
    claim: Json = {
        "id": claim_id, "text": text, "run_id": card["run_id"], "scorecard_ref": ref, "k": rate["k"], "n": rate["n"], "rate": rate["rate"], "ci95": rate["wilson95"],
        "method": METHOD, "population": population_for(card), "outcome_basis": OUTCOME_BASIS, "tier0_all_zero": card["tier0"]["all_zero"],
    }
    if rate["rule_of_three_upper"] is not None:
        claim["bound"] = rate["rule_of_three_upper"]
    return claim


def _allowed_numbers(claim: Json) -> set[str]:
    allowed: set[str] = set()
    for value in (claim.get("k"), claim.get("n")):
        if isinstance(value, int):
            allowed.add(str(value))
    percents = [v for v in [claim.get("rate"), *(claim.get("ci95") or []), claim.get("bound")] if isinstance(v, (int, float))]
    for v in percents:
        for places in (0, 1, 2):
            allowed.add(f"{v * 100:.{places}f}")
    return allowed


FORBIDDEN_WORDS = re.compile(r"\b(tamper[- ]proof|immutable|compliant|certified|guaranteed|zero risk|proven in production)\b", re.IGNORECASE)


def _text_problems(claim: Json) -> list[str]:
    allowed = _allowed_numbers(claim)
    out = [f"claim {claim.get('id')}: the word '{m.group(0)}' is one the release rules forbid (docs/poc/execution/release/claims.md section 3; the Record is tamper-evident)"
           for m in FORBIDDEN_WORDS.finditer(str(claim.get("text", "")))]
    for token in _NUMBER.findall(_STRIPPED.sub(" ", str(claim.get("text", "")))):
        if token.replace(",", "") not in allowed:
            out.append(f"claim {claim.get('id')}: the number {token} in its text is not one of the claim's own k, n, rate, interval or bound")
    return out


def _claim_problems(claim: Json, cards: Mapping[str, Json]) -> list[str]:
    cid = claim.get("id", "?")
    missing = [f for f in REQUIRED if f not in claim]
    if missing:
        return [f"claim {cid}: missing {', '.join(missing)}"]
    out: list[str] = []
    if claim["method"] != METHOD:
        out.append(f"claim {cid}: method must be '{METHOD}' (the scorer's), not '{claim['method']}'")
    if claim["outcome_basis"] != OUTCOME_BASIS:
        out.append(f"claim {cid}: outcome_basis must be {OUTCOME_BASIS} (outcomes are judged against the final state of the systems), not {claim['outcome_basis']}")
    card = cards.get(claim["run_id"])
    if card is None:
        return out + [f"claim {cid}: run {claim['run_id']} has no scorecard in the evidence bundle"]
    if not card["complete"]:
        out.append(f"claim {cid}: run {claim['run_id']} is not complete (an incomplete run is not published)")
    if claim["tier0_all_zero"] != card["tier0"]["all_zero"]:
        out.append(f"claim {cid}: tier0_all_zero is {claim['tier0_all_zero']} but the scorecard says {card['tier0']['all_zero']}")
    if not card["release_result"] and "not a release result" not in claim["population"]:
        out.append(f"claim {cid}: run {claim['run_id']} is not a release result and the claim's population must say \"not a release result\"")
    try:
        rate = resolve(card, claim["scorecard_ref"])
    except KeyError:
        return out + [f"claim {cid}: {claim['scorecard_ref']} does not exist in the scorecard of run {claim['run_id']}"]
    if claim["scorecard_ref"].startswith("/tier1/diagnostics/pass_k"):
        pass_k = card["tier1"]["diagnostics"]["pass_k"]
        if not pass_k["available"]:
            out.append(f"claim {cid}: pass^k was not available in run {claim['run_id']} ({pass_k['reason']}) and cannot be published")
    if not _is_rate(rate):
        return out + [f"claim {cid}: {claim['scorecard_ref']} is not a rate (k, n, rate, wilson95, rule_of_three_upper)"]
    for field, truth in (("k", rate["k"]), ("n", rate["n"]), ("rate", rate["rate"])):
        if claim[field] != truth:
            out.append(f"claim {cid}: {field} {claim[field]!r} does not match the scorecard's {truth!r}")
    if claim["ci95"] != rate["wilson95"]:
        out.append(f"claim {cid}: ci95 {claim['ci95']!r} does not match the scorecard's {rate['wilson95']!r}")
    if rate["rule_of_three_upper"] is not None and claim.get("bound") != rate["rule_of_three_upper"]:
        out.append(f"claim {cid}: zero failures in {rate['n']} cases needs its rule-of-three bound {rate['rule_of_three_upper']!r}; the claim has bound {claim.get('bound')!r}")
    return out + _text_problems(claim)


def _distribution_problems(run_id: str, card: Json, rows_by_slice: Mapping[str, Json]) -> list[str]:
    out = []
    for name in SLICES:
        rows = rows_by_slice.get(name)
        truth = card[name]
        if rows is None:
            out.append(f"run {run_id}: the {name} distribution is missing (the full distribution is published, not a selection)")
            continue
        for key in sorted(set(truth) - set(rows)):
            out.append(f"run {run_id}: {name} row {key} is missing from the published full distribution (cherry-picking)")
        for key in sorted(set(rows) - set(truth)):
            out.append(f"run {run_id}: {name} row {key} is not in the scorecard")
        for key in sorted(set(truth) & set(rows)):
            expected = {"n": truth[key]["n"], "k": truth[key]["correct"]["k"]}
            if rows[key] != expected:
                out.append(f"run {run_id}: {name} row {key} is {rows[key]!r} but the scorecard has {expected!r}")
    return out


def check(publication: Json, cards: Mapping[str, Json]) -> list[str]:
    claims = publication.get("claims") or []
    problems: list[str] = []
    if not claims:
        return ["the publication has no claims"]
    seen: set[str] = set()
    for claim in claims:
        if claim.get("id") in seen:
            problems.append(f"claim {claim.get('id')}: duplicate claim id")
        seen.add(claim.get("id"))
        problems += _claim_problems(claim, cards)
    runs = sorted({c["run_id"] for c in claims if "run_id" in c})
    for run_id in runs:
        card = cards.get(run_id)
        if card is None:
            continue
        if not any(c.get("run_id") == run_id and str(c.get("scorecard_ref", "")).startswith("/tier0/") for c in claims):
            problems.append(f"run {run_id}: no Tier 0 claim (Tier 0 is published first, beside every other number)")
        seen_tier0 = False
        for claim in claims:
            if claim.get("run_id") != run_id:
                continue
            if str(claim.get("scorecard_ref", "")).startswith("/tier0/"):
                seen_tier0 = True
            elif not seen_tier0:
                problems.append(f"claim {claim.get('id')}: listed before the Tier 0 claim of run {run_id}; Tier 0 is published first (R4)")
        rows = {d["slice"]: d["rows"] for d in publication.get("distributions") or [] if d.get("run_id") == run_id}
        problems += _distribution_problems(run_id, card, rows)
    return sorted(problems)
