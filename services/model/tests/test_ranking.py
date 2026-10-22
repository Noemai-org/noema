"""Deterministic ranking and the auto-resolve rule (REQ-AMB-003, REQ-AMB-002, REQ-AMB-007, INV-21).

The numbers below are TEST VALUES copied from the adopted figures in PERD REQ-AMB-003 (weights, margin 0.35, floor 0.70,
at most 4 above the floor) and REQ-AMB-002 (plus or minus 1% and 2 hours, 20 candidates). Production reads them from the
pack; nothing in noema_model holds them.
"""

import ast
import random
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from fractions import Fraction
from pathlib import Path
from typing import Any

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from noema_model.platform.config import ConfigError
from noema_model.understanding.resolution import (
    Candidate,
    Context,
    RankingConfig,
    RecipientMatcher,
    Resolution,
    ResolutionState,
    Target,
    Why,
    candidates_resolved_payload,
    rank,
)
from noema_model.understanding.resolution.config import exact_number

SEED = Path(__file__).resolve().parents[3] / "packs" / "fintech" / "lexicons" / "recipient_matcher.json"
MATCHER = RecipientMatcher.from_file(SEED)
RAW_CONFIG: dict[str, Any] = {
    "weights": {"amount": "0.35", "recipient": "0.25", "temporal": "0.20", "relationship": "0.10", "type_direction": "0.10"},
    "auto_resolve_margin": "0.35",
    "absolute_floor": "0.70",
    "max_candidates_before_escalate": 4,
    "max_raw_candidates": 20,
    "amount_tolerance_ratio": "0.01",
    "time_tolerance_seconds": 7200,
}
CONFIG = RankingConfig.from_mapping(RAW_CONFIG)
F = Fraction
T0 = datetime(2026, 10, 21, 8, 0, tzinfo=UTC)
WINDOW = (T0 - timedelta(hours=3), T0 + timedelta(hours=4))  # "this morning", synthetic
TARGET = Target(2_000_000, "NGN", WINDOW, "Tunde", "OUT")
QUIET = Context(material_flag_present=False, key_slot_low_asr=False, phonetic=False)


def cand(cid: str, *, amount: int = 2_000_000, who: str = "Tunde Adeyemi", at: datetime = T0, direction: str = "OUT",
         alias: bool | None = True, currency: str = "NGN") -> Candidate:
    return Candidate(cid, f"res_{cid}", amount, currency, at, who, "0001", direction, alias)


def run_rank(
    cands: tuple[Candidate, ...], target: Target = TARGET, context: Context = QUIET, config: RankingConfig = CONFIG
) -> Resolution:
    return rank(cands, target, context, config, MATCHER, candidate_set_id="cs_test", candidate_set_version=1)


# --- the worked cases from the PERD ----------------------------------------------------------------------------------

def test_us01_one_exact_match_auto_resolves() -> None:
    result = run_rank((cand("a"),))
    assert result.state is ResolutionState.AUTO_RESOLVED and result.resolved_candidate_id == "a"
    assert result.top_score == 1 and result.margin == 1 and result.why_not == ()


def test_g_b_two_tundes_minutes_apart_is_a_tie_and_never_auto_resolves() -> None:
    result = run_rank((cand("a", who="Tunde Adeyemi", at=T0), cand("b", who="Tunde Bello", at=T0 + timedelta(minutes=5))))
    assert result.state is ResolutionState.AMBIGUOUS
    assert Why.TIE_AT_TOP in result.why_not and Why.MARGIN_NOT_MET in result.why_not
    assert result.resolved_candidate_id is None


def test_ec_a07_a_near_amount_is_partial_not_exact() -> None:
    near = run_rank((cand("a", amount=2_005_000),))
    # 5,000 apart on 2,000,000 with a 1% tolerance (20,000): 1 - 5000/20000 = 3/4 of the amount weight.
    assert near.ranked[0].features["amount"] == F(3, 4)
    assert near.top_score == 1 - F(35, 100) * F(1, 4)
    exact = run_rank((cand("a"),))
    assert near.top_score < exact.top_score


def test_amount_outside_tolerance_and_currency_mismatch_score_zero() -> None:
    assert run_rank((cand("a", amount=2_040_000),)).ranked[0].features["amount"] == 0
    assert run_rank((cand("a", currency="USD"),)).ranked[0].features["amount"] == 0


def test_temporal_is_one_inside_the_window_and_ramps_down_over_the_tolerance() -> None:
    inside = run_rank((cand("a", at=WINDOW[1]),)).ranked[0].features["temporal"]
    one_hour_out = run_rank((cand("a", at=WINDOW[1] + timedelta(hours=1)),)).ranked[0].features["temporal"]
    beyond = run_rank((cand("a", at=WINDOW[1] + timedelta(hours=2)),)).ranked[0].features["temporal"]
    assert (inside, one_hour_out, beyond) == (F(1), F(1, 2), F(0))


def test_g_c_nothing_but_a_time_can_never_auto_resolve() -> None:
    target = Target(None, None, WINDOW, None, None)
    result = run_rank((cand("a"),), target)
    # temporal 0.20 + relationship 0.10 at most; the floor of 0.70 cannot be reached without an amount and a name.
    assert result.top_score == F(3, 10) and result.state is ResolutionState.AMBIGUOUS and Why.BELOW_FLOOR in result.why_not


def test_no_candidates_is_its_own_state() -> None:
    result = run_rank(())
    assert result.state is ResolutionState.NO_CANDIDATES and result.ranked == () and result.resolved_candidate_id is None


# --- the margin, the floor, the crowd, the flags: each enforced on its own -----------------------------------------------

def two(second_amount: int) -> Any:
    return run_rank((cand("a"), cand("b", amount=second_amount, who="Chidi Okafor", alias=False)))


def test_the_margin_is_met_exactly_at_the_boundary_and_not_one_step_below() -> None:
    gap = two(2_040_000).margin
    assert gap >= CONFIG.auto_resolve_margin
    at_edge = replace(CONFIG, auto_resolve_margin=gap)
    below_edge = replace(CONFIG, auto_resolve_margin=gap + F(1, 1_000_000))
    pair = (cand("a"), cand("b", amount=2_040_000, who="Chidi Okafor", alias=False))
    assert run_rank(pair, config=at_edge).state is ResolutionState.AUTO_RESOLVED
    blocked = run_rank(pair, config=below_edge)
    assert blocked.state is ResolutionState.AMBIGUOUS and blocked.why_not == (Why.MARGIN_NOT_MET,)


def test_the_floor_is_met_exactly_at_the_boundary_and_not_one_step_below() -> None:
    single = (cand("a", amount=2_005_000),)
    top = run_rank(single).top_score
    assert run_rank(single, config=replace(CONFIG, absolute_floor=top)).state is ResolutionState.AUTO_RESOLVED
    blocked = run_rank(single, config=replace(CONFIG, absolute_floor=top + F(1, 1_000_000)))
    assert blocked.state is ResolutionState.AMBIGUOUS and blocked.why_not == (Why.BELOW_FLOOR,)


def test_a_clear_leader_among_more_than_four_above_the_floor_is_too_many() -> None:
    crowd = tuple(cand(f"c{i}", at=T0 - timedelta(minutes=i)) for i in range(5))
    result = run_rank(crowd)
    assert result.state is ResolutionState.TOO_MANY_CANDIDATES and result.above_floor_count == 5
    four = run_rank(crowd[:4])
    assert four.state is ResolutionState.AMBIGUOUS  # a four-way tie clarifies; it is not a crowd


def test_a_material_flag_or_low_asr_blocks_an_otherwise_clean_win() -> None:
    flagged = run_rank((cand("a"),), context=Context(True, False, False))
    asr = run_rank((cand("a"),), context=Context(False, True, False))
    assert (flagged.state, flagged.why_not) == (ResolutionState.AMBIGUOUS, (Why.MATERIAL_FLAG,))
    assert (asr.state, asr.why_not) == (ResolutionState.AMBIGUOUS, (Why.LOW_ASR_CONFIDENCE,))


def test_the_score_is_exactly_the_weighted_sum() -> None:
    scored = run_rank((cand("a", amount=2_010_000, who="Babatunde Bello", at=WINDOW[1] + timedelta(hours=1), alias=False),)).ranked[0]
    expected = sum((CONFIG.weights[k] * v for k, v in scored.features.items()), F(0))
    assert scored.score == expected and all(0 <= v <= 1 for v in scored.features.values())


# --- replay ---------------------------------------------------------------------------------------------------------------

def sample_set() -> tuple[Candidate, ...]:
    return (
        cand("a", at=T0), cand("b", who="Tunde Bello", at=T0 + timedelta(minutes=3)),
        cand("c", amount=2_010_000, alias=False), cand("d", who="Chidi Okafor", direction="IN"),
    )


def test_identical_inputs_give_identical_results_and_hash() -> None:
    assert run_rank(sample_set()) == run_rank(sample_set())
    assert run_rank(sample_set()).content_hash == run_rank(sample_set()).content_hash


def test_the_order_candidates_arrive_in_changes_nothing() -> None:
    base = run_rank(sample_set())
    for seed in range(20):
        shuffled = list(sample_set())
        random.Random(seed).shuffle(shuffled)
        assert run_rank(tuple(shuffled)) == base


def test_the_hash_moves_with_any_input() -> None:
    base = run_rank(sample_set()).content_hash
    assert run_rank(sample_set(), config=replace(CONFIG, absolute_floor=F(1, 2), digest="x")).content_hash != base
    assert run_rank(sample_set(), context=Context(True, False, False)).content_hash != base
    assert run_rank(sample_set(), target=replace(TARGET, counterparty_surface="Chidi")).content_hash != base
    assert rank(sample_set(), TARGET, QUIET, CONFIG, MATCHER, candidate_set_id="cs_test", candidate_set_version=2).content_hash != base


def test_the_audit_payload_has_ids_and_scores_only() -> None:
    payload = candidates_resolved_payload(run_rank(sample_set()))
    text = repr(payload)
    assert "Tunde" not in text and "res_" not in text and "0001" not in text
    assert isinstance(payload["candidates"], list) and payload["candidates"][0]["candidate_id"] in {"a", "b"}


# --- guards -------------------------------------------------------------------------------------------------------------

def test_more_than_the_bounded_read_or_duplicate_ids_are_refused() -> None:
    with pytest.raises(ValueError):
        run_rank(tuple(cand(f"c{i}") for i in range(21)))
    with pytest.raises(ValueError):
        run_rank((cand("a"), cand("a")))


def test_bad_candidates_and_targets_are_refused() -> None:
    with pytest.raises(ValueError):
        cand("a", amount=-1)
    with pytest.raises(ValueError):
        Candidate("a", "r", 1, "NGN", datetime(2026, 1, 1), "x", "0001", "OUT", None)
    with pytest.raises(ValueError):
        Candidate("a", "r", 1, "NGN", T0, "x", "00012", "OUT", None)
    with pytest.raises(ValueError):
        Target(None, None, (T0, T0 - timedelta(hours=1)), None, None)


@pytest.mark.parametrize(
    "mutate",
    [
        lambda r: r["weights"].pop("recipient"),
        lambda r: r["weights"].update(amount="0.36"),
        lambda r: r["weights"].update(amount=0.35),
        lambda r: r["weights"].update(extra="0"),
        lambda r: r.pop("absolute_floor"),
        lambda r: r.update(auto_resolve_margin="1.5"),
        lambda r: r.update(auto_resolve_margin="0.35e0"),
        lambda r: r.update(absolute_floor="nan"),
        lambda r: r.update(absolute_floor=0.7),
        lambda r: r.update(absolute_floor=True),
        lambda r: r.update(max_candidates_before_escalate=0),
        lambda r: r.update(surprise=1),
        lambda r: r.update(amount_tolerance_ratio="1/0"),
    ],
)
def test_a_pack_with_a_missing_or_malformed_ranking_value_fails_at_load(mutate: Any) -> None:
    import copy
    raw = copy.deepcopy(RAW_CONFIG)
    mutate(raw)
    with pytest.raises(ConfigError):
        RankingConfig.from_mapping(raw)


def test_exact_number_reads_decimals_and_fractions_without_floats() -> None:
    assert exact_number("0.35", "x") == F(7, 20) and exact_number("7/20", "x") == F(7, 20) and exact_number(1, "x") == 1


def test_the_config_digest_follows_the_values() -> None:
    other = RankingConfig.from_mapping({**RAW_CONFIG, "absolute_floor": "0.71"})
    assert other.digest != CONFIG.digest and RankingConfig.from_mapping(RAW_CONFIG).digest == CONFIG.digest


def test_no_float_literal_and_no_model_import_in_the_resolution_package() -> None:
    root = Path(__file__).resolve().parents[1] / "noema_model" / "understanding" / "resolution"
    for path in root.glob("*.py"):
        tree = ast.parse(path.read_text())
        for node in ast.walk(tree):
            if isinstance(node, ast.Constant) and isinstance(node.value, float):
                pytest.fail(f"{path.name}:{node.lineno} float literal")
            if isinstance(node, (ast.Import, ast.ImportFrom)):
                module = getattr(node, "module", None) or ""
                names = [a.name for a in node.names]
                assert "harness" not in module.split(".") and "providers" not in module.split(".") and "harness" not in names


# --- properties ---------------------------------------------------------------------------------------------------------

CAND = st.builds(
    lambda i, amount, who, minutes, direction, alias: cand(f"c{i}", amount=amount, who=who, at=T0 + timedelta(minutes=minutes), direction=direction, alias=alias),
    st.integers(0, 10_000),
    st.sampled_from([2_000_000, 2_005_000, 2_010_000, 2_100_000, 5_000]),
    st.sampled_from(["Tunde Adeyemi", "Tunde Bello", "Babatunde Okafor", "Chidi Okafor", "T. Adeyemi", ""]),
    st.integers(-400, 400),
    st.sampled_from(["OUT", "IN"]),
    st.sampled_from([True, False, None]),
)
CANDS = st.lists(CAND, max_size=8, unique_by=lambda c: c.candidate_id).map(tuple)
CONTEXTS = st.builds(Context, st.booleans(), st.booleans(), st.booleans())


@settings(max_examples=300, deadline=None)
@given(CANDS, CONTEXTS)
def test_property_auto_resolve_implies_every_condition(cands: tuple[Candidate, ...], context: Context) -> None:
    result = run_rank(cands, context=context)
    if result.state is ResolutionState.AUTO_RESOLVED:
        scores = [s.score for s in result.ranked]
        assert scores.count(scores[0]) == 1
        assert scores[0] - (scores[1] if len(scores) > 1 else 0) >= CONFIG.auto_resolve_margin
        assert scores[0] >= CONFIG.absolute_floor
        assert not context.material_flag_present and not context.key_slot_low_asr
        assert result.above_floor_count <= CONFIG.max_candidates_before_escalate
        assert result.resolved_candidate_id == result.ranked[0].candidate.candidate_id
    else:
        assert result.resolved_candidate_id is None


@settings(max_examples=300, deadline=None)
@given(CANDS, CONTEXTS, st.integers(0, 10_000))
def test_property_permuting_the_input_never_changes_the_result(cands: tuple[Candidate, ...], context: Context, seed: int) -> None:
    shuffled = list(cands)
    random.Random(seed).shuffle(shuffled)
    assert run_rank(tuple(shuffled), context=context) == run_rank(cands, context=context)


@settings(max_examples=300, deadline=None)
@given(CANDS)
def test_property_a_tie_at_the_top_never_resolves(cands: tuple[Candidate, ...]) -> None:
    result = run_rank(cands)
    if len(result.ranked) > 1 and result.ranked[0].score == result.ranked[1].score:
        assert result.state is not ResolutionState.AUTO_RESOLVED and Why.TIE_AT_TOP in result.why_not


@settings(max_examples=200, deadline=None)
@given(CANDS)
def test_property_features_and_scores_stay_in_the_unit_interval(cands: tuple[Candidate, ...]) -> None:
    for scored in run_rank(cands).ranked:
        assert all(0 <= v <= 1 for v in scored.features.values()) and 0 <= scored.score <= 1


@settings(max_examples=200, deadline=None)
@given(CANDS, CANDS)
def test_property_an_unflagged_unique_leader_is_never_displaced_by_a_flag(a: tuple[Candidate, ...], b: tuple[Candidate, ...]) -> None:
    cands = a + tuple(c for c in b if c.candidate_id not in {x.candidate_id for x in a})
    if len(cands) > CONFIG.max_raw_candidates:
        return
    clean = run_rank(cands)
    flagged = run_rank(cands, context=Context(True, False, False))
    assert flagged.state is not ResolutionState.AUTO_RESOLVED
    assert [s.score for s in flagged.ranked] == [s.score for s in clean.ranked]  # flags never change a score
