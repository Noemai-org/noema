"""UND-05, the pack loader: `PipelineConfig` from the pack's `understanding` block, the node's `disambiguation` and the matcher file the pack names
(REQ-AMB-002, REQ-AMB-003, REQ-INT-005; INV-14, INV-21; AGENT-16: no value is written in code and none is defaulted). All data is SYNTHETIC.

Written before `noema_model.understanding.compiler.packconfig` exists: these fail with an import error until then.
"""

import copy
import json
from fractions import Fraction
from pathlib import Path
from typing import Any

import pytest

from noema_model.platform.config import ConfigError
from noema_model.understanding.compiler.packconfig import load_pipeline_config
from noema_model.understanding.pipeline import NodeCatalogue
from noema_model.understanding.resolution import RecipientMatcher, ResolutionState

ROOT = Path(__file__).resolve().parents[3]
PACK_DIR = ROOT / "packs"
PACK: dict[str, Any] = json.loads((PACK_DIR / "fintech_v1.json").read_text(encoding="utf-8"))
OUT_OF_SCOPE = "fin.meta.out_of_scope"
NODE = "fin.payments.exception.investigate"


def load(pack: dict[str, Any] | None = None, pack_dir: Path = PACK_DIR) -> Any:
    return load_pipeline_config(pack if pack is not None else PACK, pack_dir, out_of_scope_node=OUT_OF_SCOPE)


def variant(edit: Any) -> dict[str, Any]:
    pack = copy.deepcopy(PACK)
    edit(pack)
    return pack


def node(pack: dict[str, Any]) -> dict[str, Any]:
    return next(n for n in pack["intent_nodes"] if n["node_id"] == NODE)


# --- the real pack ----------------------------------------------------------------------------------------------------------------------

def test_the_real_pack_loads_with_the_values_the_perd_gives() -> None:
    config = load()
    ranking = config.ranking
    assert ranking.weights == {"amount": Fraction(7, 20), "recipient": Fraction(1, 4), "temporal": Fraction(1, 5), "relationship": Fraction(1, 10), "type_direction": Fraction(1, 10)}
    assert ranking.absolute_floor == Fraction(7, 10) and ranking.max_raw_candidates == 20 and ranking.time_tolerance_seconds == 7200
    assert ranking.amount_tolerance_ratio == Fraction(1, 100)


def test_the_margin_and_the_limit_come_from_the_nodes_disambiguation_as_exact_values() -> None:
    ranking = load().ranking
    assert ranking.auto_resolve_margin == Fraction(7, 20)  # a JSON 0.35 is read as its decimal, not as the binary 0.34999...
    assert ranking.max_candidates_before_escalate == 4


def test_the_flags_the_locales_and_the_states_come_from_the_pack() -> None:
    config = load()
    assert [f.flag for f in config.proposable_flags] == ["MULTIPLE_INTENTS", "CONTRADICTS_PRIOR_TURN"]
    assert all(f.question for f in config.proposable_flags)
    assert [lo.code for lo in config.locales] == ["en-NG", "pcm", "yo", "ha", "ig"]
    assert config.intent_unclear_flag == "INTENT_UNCLEAR" and config.out_of_scope_flag == "OUT_OF_SCOPE_REQUEST"
    assert "NO_TRANSACTION_MATCH" in config.material_flags and isinstance(config.material_flags, frozenset)
    assert config.state_flags == {
        ResolutionState.NO_CANDIDATES: "NO_TRANSACTION_MATCH", ResolutionState.AMBIGUOUS: "MULTIPLE_TRANSACTIONS_MATCH",
        ResolutionState.TOO_MANY_CANDIDATES: "MULTIPLE_TRANSACTIONS_MATCH",
    }


def test_the_matcher_is_the_file_the_pack_names_and_the_catalogue_is_the_packs() -> None:
    config = load()
    assert config.matcher.digest == RecipientMatcher.from_file(PACK_DIR / "fintech" / "lexicons" / "recipient_matcher.json").digest
    assert set(config.catalogue.nodes) == set(NodeCatalogue.from_pack(PACK, out_of_scope_node=OUT_OF_SCOPE).nodes)


def test_nothing_is_defaulted_the_yes_threshold_stays_unset_until_a_founder_sets_it() -> None:
    assert load().native_yes_threshold is None  # AGENT-16: a benchmark and a founder decision set it; the pack carries none


def test_loading_twice_gives_the_same_configuration() -> None:
    assert load().ranking.digest == load().ranking.digest and load().matcher.digest == load().matcher.digest


# --- refusals: a pack that is not whole fails at load, never at the first request ------------------------------------------------------------

def test_a_pack_without_the_understanding_block_is_refused() -> None:
    with pytest.raises(ConfigError):
        load(variant(lambda p: p.pop("understanding")))


def test_an_unknown_member_of_the_understanding_block_is_refused() -> None:
    with pytest.raises(ConfigError):
        load(variant(lambda p: p["understanding"].update({"surprise": 1})))


@pytest.mark.parametrize("member", ["ranking", "proposable_flags", "locales", "material_flags", "intent_unclear_flag", "out_of_scope_flag", "state_flags"])
def test_each_member_of_the_understanding_block_is_required(member: str) -> None:
    with pytest.raises(ConfigError):
        load(variant(lambda p: p["understanding"].pop(member)))


def test_a_state_the_ranking_does_not_have_is_refused() -> None:
    with pytest.raises(ConfigError):
        load(variant(lambda p: p["understanding"]["state_flags"].update({"INVENTED_STATE": "SOME_FLAG"})))


def test_a_state_flag_that_is_not_a_string_is_refused() -> None:
    with pytest.raises(ConfigError):
        load(variant(lambda p: p["understanding"]["state_flags"].update({"AMBIGUOUS": 3})))


@pytest.mark.parametrize("key,bad", [("proposable_flags", {"flag": "MULTIPLE_INTENTS", "question": ""}), ("proposable_flags", {"flag": "lower_case", "question": "Q?"}),
                                     ("locales", {"code": "en-NG", "label": ""}), ("locales", {"code": "NOT A CODE", "label": "X"})])
def test_a_flag_or_locale_with_no_text_or_a_bad_shape_is_refused(key: str, bad: dict[str, str]) -> None:
    with pytest.raises(ConfigError):
        load(variant(lambda p: p["understanding"][key].append(bad)))


def test_a_repeated_flag_locale_or_material_flag_is_refused() -> None:
    for edit in (lambda p: p["understanding"]["proposable_flags"].append(dict(p["understanding"]["proposable_flags"][0])),
                 lambda p: p["understanding"]["locales"].append(dict(p["understanding"]["locales"][0])),
                 lambda p: p["understanding"]["material_flags"].append(p["understanding"]["material_flags"][0])):
        with pytest.raises(ConfigError):
            load(variant(edit))


def test_a_pack_with_no_node_that_searches_for_a_candidate_is_refused() -> None:
    def edit(p: dict[str, Any]) -> None:
        for slot in node(p)["slots"]:
            slot.pop("resolution", None)

    with pytest.raises(ConfigError):
        load(variant(edit))


@pytest.mark.parametrize("margin", [None, "high", True, 1.5, -0.1])
def test_a_margin_that_is_missing_not_a_number_or_outside_zero_to_one_is_refused(margin: Any) -> None:
    def edit(p: dict[str, Any]) -> None:
        if margin is None:
            node(p)["disambiguation"].pop("auto_resolve_margin")
        else:
            node(p)["disambiguation"]["auto_resolve_margin"] = margin

    with pytest.raises(ConfigError):
        load(variant(edit))


@pytest.mark.parametrize("limit", [None, 0, -1, 2.5, "4", True])
def test_a_limit_that_is_missing_or_not_a_positive_integer_is_refused(limit: Any) -> None:
    def edit(p: dict[str, Any]) -> None:
        if limit is None:
            node(p)["disambiguation"].pop("max_candidates_before_escalate")
        else:
            node(p)["disambiguation"]["max_candidates_before_escalate"] = limit

    with pytest.raises(ConfigError):
        load(variant(edit))


@pytest.mark.parametrize("ref", ["../fintech_v1.json", "/etc/passwd", "fintech/../../fintech_v1.json", "", "fintech/lexicons/missing.json"])
def test_a_matcher_reference_that_leaves_the_pack_folder_or_names_no_file_is_refused(ref: str) -> None:
    with pytest.raises(ConfigError):
        load(variant(lambda p: node(p)["disambiguation"].update({"matcher_ref": ref})))


def test_a_candidate_node_with_no_matcher_reference_is_refused() -> None:
    with pytest.raises(ConfigError):
        load(variant(lambda p: node(p)["disambiguation"].pop("matcher_ref")))
