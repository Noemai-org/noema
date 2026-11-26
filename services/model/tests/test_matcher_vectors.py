"""A710 (REQ-AMB-003, INV-21): the recipient matcher's golden vectors. The Python reference matcher and the Go matcher
(the recipientmatch package of the core's evidence layer) both load `contracts/fixtures/recipient_match_vectors.json`, build their matcher from the pack's
`recipient_matcher.json` and must give the same exact fraction, in lowest terms, for every case. The expected values were produced by this
reference (the authority); a Go result that differs is a Go defect. All names are SYNTHETIC."""

import json
from fractions import Fraction
from pathlib import Path
from typing import Any

from noema_model.understanding.resolution.matcher import RecipientMatcher

ROOT = Path(__file__).resolve().parents[3]
VECTORS = ROOT / "contracts" / "fixtures" / "recipient_match_vectors.json"
DATA = ROOT / "packs" / "fintech" / "lexicons" / "recipient_matcher.json"


def _doc() -> dict[str, Any]:
    raw: dict[str, Any] = json.loads(VECTORS.read_text(encoding="utf-8"))
    return raw


def test_the_vector_file_is_synthetic_and_names_the_pack_data_it_is_built_from() -> None:
    doc = _doc()
    assert doc["label"] == "SYNTHETIC"
    assert (ROOT / doc["data"]).resolve() == DATA.resolve()
    assert len(doc["cases"]) >= 300
    assert len({c["name"] for c in doc["cases"]}) == len(doc["cases"])


def test_every_vector_is_the_reference_matchers_exact_fraction_in_lowest_terms() -> None:
    matcher = RecipientMatcher.from_file(DATA)
    for case in _doc()["cases"]:
        got = matcher.score(case["query"], case["display"], phonetic=case["phonetic"])
        want = Fraction(case["want"]["numerator"], case["want"]["denominator"])
        assert got == want, case["name"]
        assert (got.numerator, got.denominator) == (case["want"]["numerator"], case["want"]["denominator"]), case["name"]  # lowest terms
        assert 0 <= got <= 1, case["name"]


def test_the_vectors_cover_every_tier_and_both_phonetic_modes() -> None:
    matcher = RecipientMatcher.from_file(DATA)
    seen: set[Fraction] = set()
    on_off_differ = False
    cases = {c["name"]: c for c in _doc()["cases"]}
    for case in cases.values():
        seen.add(matcher.score(case["query"], case["display"], phonetic=case["phonetic"]))
    for name, case in cases.items():
        if name.endswith(" on") and cases[name[:-3] + " off"]["want"] != case["want"]:
            on_off_differ = True
    assert {Fraction(0), Fraction(1), Fraction(4, 5), Fraction(3, 5), Fraction(2, 5)} <= seen
    assert on_off_differ, "no vector shows the phonetic tier changing a score"
