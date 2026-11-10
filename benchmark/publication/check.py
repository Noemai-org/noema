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
import unicodedata
from collections.abc import Mapping
from typing import Any

Json = dict[str, Any]
METHOD = "Wilson 95% interval"
OUTCOME_BASIS = "ORACLE_FINAL_STATE"
HEADLINE_MIN_N = 300  # ruling A40: no headline safety claim from fewer than 300 cases. The Tier 0 claims are the headline safety claims
REQUIRED = ("id", "text", "run_id", "scorecard_ref", "k", "n", "rate", "ci95", "method", "population", "outcome_basis", "tier0_all_zero")
SLICES = ("by_family", "by_category")
_SIGN = "[-+−–]"
# Method words are not numbers: Tier names, k = 5, and the 95% of the Wilson interval when it is written as a confidence level ("95% confidence", "95% interval", "95% CI",
# "95% Wilson", "Wilson 95%"). Any other 95% (a claimed rate such as "95% of cases") is a number and must be the claim's own. A signed 95% (-95%) is a number too.
_STRIPPED = re.compile(r"\bTier [0-3]\b|(?<!" + _SIGN + r")\b95\s?%(?=\s+(?:confidence|interval|CI\b|Wilson))|\bWilson\s+95\s?%|\bk\s?=\s?5\b|pass\^k")
# A number is refused unless it is the claim's own, whatever surrounds it: a sign (-99%), a range (5-99%) or a unit (99th, 1.5x). A digit that continues a word or follows a letter and a
# hyphen is part of an identifier (B4, US-01, run-12) and is not a number.
_NUMBER = re.compile(r"(?<![\w.])(?<!\w-)" + _SIGN + r"?\d[\d,]*(?:\.\d+)?(?:[eE][-+]?\d+)?(?:[-–]\d[\d,]*(?:\.\d+)?)?(?!\d)")


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


# The words of docs/poc/execution/release/claims.md section 3, matched as STEMS (Security Reviewer's ruling on noema#124, F1 option a): `immutab`, `guarante`, `unforgeab`, `complian`, `certif`
# take any ending, so `immutably`, `guarantees`, `compliance-ready` and `certification` are the same claim as the listed word. A stem needs a letter boundary BEFORE it and none after, so `uncertified`,
# `uncertain` and `tamper-evident` stay allowed. The text is normalised first (NFKC, so full-width letters and the non-breaking hyphen and space fold to plain ones; invisible characters removed;
# case folded), and a two-word term takes any run of spaces, hyphens, dashes, minus signs and underscores, or none, between its parts.
_SEP = r"[\s\-_\u2010-\u2015\u2212]*"
FORBIDDEN_WORDS = re.compile(r"(?<![a-z])(tamper" + _SEP + r"proof|immutab|guarante|unforgeab|complian|certif|zero" + _SEP + r"risk|proven" + _SEP + r"in" + _SEP + r"production)[a-z]*")  # the trailing letters only complete the word for the message
_INVISIBLE = dict.fromkeys(map(ord, "\u00ad\u034f\u061c\u180e\u200b\u200c\u200d\u200e\u200f\u2060\u2061\u2062\u2063\u2064\ufeff"))


def forbidden_words(text: str) -> list[str]:
    folded = unicodedata.normalize("NFKC", text).translate(_INVISIBLE).casefold()
    return [m.group(0) for m in FORBIDDEN_WORDS.finditer(folded)]


def _text_problems(claim: Json) -> list[str]:
    allowed = _allowed_numbers(claim)
    out = [f"claim {claim.get('id')}: the word '{word}' is one the release rules forbid (docs/poc/execution/release/claims.md section 3; the Record is tamper-evident)"
           for word in forbidden_words(str(claim.get("text", "")))]
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
    if claim["population"] != population_for(card):
        out.append(f"claim {cid}: population {claim['population']!r} is not the scorecard's population {population_for(card)!r} (case count, layers and run must be the scorecard's, REQ-BEN-010)")
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
    if _is_rate(rate) and claim["scorecard_ref"].startswith("/tier0/") and rate["n"] < HEADLINE_MIN_N:
        out.append(f"claim {cid}: a headline safety claim (Tier 0) needs n of at least {HEADLINE_MIN_N} (ruling A40, METRICS-v1 section 4); run {claim['run_id']} measured it on n = {rate['n']}")
    if not _is_rate(rate):
        return out + [f"claim {cid}: {claim['scorecard_ref']} is not a rate (k, n, rate, wilson95, rule_of_three_upper)"]
    for field, truth in (("k", rate["k"]), ("n", rate["n"]), ("rate", rate["rate"])):
        if claim[field] != truth:
            out.append(f"claim {cid}: {field} {claim[field]!r} does not match the scorecard's {truth!r}")
    if claim["ci95"] != rate["wilson95"]:
        out.append(f"claim {cid}: ci95 {claim['ci95']!r} does not match the scorecard's {rate['wilson95']!r}")
    if rate["rule_of_three_upper"] is not None and claim.get("bound") != rate["rule_of_three_upper"]:
        out.append(f"claim {cid}: zero failures in {rate['n']} cases needs its rule-of-three bound {rate['rule_of_three_upper']!r}; the claim has bound {claim.get('bound')!r}")
    elif rate["rule_of_three_upper"] is None and "bound" in claim:
        out.append(f"claim {cid}: the scorecard has no rule-of-three bound for this rate (it is not a zero-failure rate), so the claim must not state a bound; it has bound {claim['bound']!r}")
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


SCORECARD_MEMBERS = ("run_id", "complete", "release_result", "n_cases", "layers", "tier0", "tier1", "by_family", "by_category")


def _scorecard_missing(card: Any) -> list[str]:
    """The members of a bundle scorecard this check reads that it does not have. A scorecard that lacks them is a problem to list, never a crash (Voice N2 on noema#124)."""
    if not isinstance(card, Mapping):
        return list(SCORECARD_MEMBERS)
    missing = [m for m in SCORECARD_MEMBERS if m not in card]
    if "tier0" not in missing and not (isinstance(card["tier0"], Mapping) and "all_zero" in card["tier0"]):
        missing.append("tier0.all_zero")
    return missing


def check(publication: Json, all_cards: Mapping[str, Json]) -> list[str]:
    claims = publication.get("claims") or []
    problems: list[str] = []
    broken = {rid: _scorecard_missing(c) for rid, c in all_cards.items() if _scorecard_missing(c)}
    cards = {rid: c for rid, c in all_cards.items() if rid not in broken}
    cited = {c.get("run_id") for c in claims if isinstance(c, Mapping)}
    for rid in sorted(broken.keys() & cited, key=str):
        problems.append(f"run {rid}: the scorecard in the bundle is missing {', '.join(broken[rid])}, so no claim on it can be checked")
    if not claims:
        return ["the publication has no claims"]
    seen: set[str] = set()
    for claim in claims:
        if claim.get("id") in seen:
            problems.append(f"claim {claim.get('id')}: duplicate claim id")
        seen.add(claim.get("id"))
        if claim.get("run_id") not in broken:
            problems += _claim_problems(claim, cards)
    for d in publication.get("distributions") or []:
        if not isinstance(d, Mapping):
            problems.append(f"a distribution record is not an object: {d!r} (every distribution is an object naming its run, its slice and its rows)")
    keys = [(d.get("run_id"), d.get("slice")) for d in publication.get("distributions") or [] if isinstance(d, Mapping)]
    for run_id, name in sorted({k for k in keys if keys.count(k) > 1}, key=str):
        problems.append(f"run {run_id}: the {name} distribution appears more than one time in the publication (one record per run and slice; a later record must never hide an earlier one)")
    runs = sorted({c["run_id"] for c in claims if "run_id" in c})
    for d in publication.get("distributions") or []:
        absent = [f for f in ("run_id", "slice", "rows") if not isinstance(d, Mapping) or f not in d]
        if absent:
            problems.append(f"a distribution record is missing {', '.join(absent)} (every distribution names its run, its slice and its rows)")
        elif d["run_id"] not in runs:
            problems.append(f"run {d['run_id']}: a {d['slice']} distribution is published for a run no claim cites, so no claim ties these numbers to a scorecard in the evidence bundle")
        elif d["slice"] not in SLICES:
            problems.append(f"run {d['run_id']}: the distribution slice {d['slice']!r} is not one of {', '.join(SLICES)}")
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
        rows = {d["slice"]: d["rows"] for d in publication.get("distributions") or [] if isinstance(d, Mapping) and d.get("run_id") == run_id and "slice" in d and "rows" in d}
        problems += _distribution_problems(run_id, card, rows)
    return sorted(problems)
