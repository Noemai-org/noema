"""UND-18, the optional slot as a gate and a pick (ruling A603, interim; A533, A605). All data is SYNTHETIC.

Run #7 (45268856727) ended with goldens G-A, G-B and G-E MISSED on `requested_remedy`: asked as one choice with a one-in-21 abstention, the model picks a capability the customer never asked for.
A603 splits it. Stage 1 is a yes/no gate with one fixed sentence; it is read as a probability against a threshold that lives in PACK DATA (the Architect's interim 9000 basis points, a Founder
decision to confirm, never set by an agent, AGENT-16). Stage 2 picks the capability only when the gate passed, and has NO abstention. Doubt abstains: the slot is then absent, because a false
assertion is the harmful error. The wording of the optional-slot question for slots that are not gated does not change (A533), and no golden or input is touched.

What these tests prove: the gate is asked once, in the fixed words; the comparison is exact at 8999, 9000 and 9001 basis points; a model's own "yes" at a low probability is doubt; the pick has no
abstention and is a separate call that is made only after a passed gate; an answer outside the options leaves the slot absent and does not stop the turn; required slots and non-gated slots are
unchanged; a pack declares the gate as data, and the loader refuses a gate that is malformed or that names a slot that cannot be gated; the core names no slot.
"""

import copy
import dataclasses
import json
from pathlib import Path
from typing import Any

import pytest

from noema_model.harness import ABSTAIN, ChoiceQuestion, HarnessRejection, Option, YesNoQuestion
from noema_model.harness.decide_types import validate_questions
from noema_model.harness.errors import Reason
from noema_model.platform.config import ConfigError
from noema_model.understanding.compiler.packconfig import load_pipeline_config
from noema_model.understanding.pipeline import run_pipeline
from noema_model.understanding.pipeline.templates import SLOT_GATE_V1
from tests.support import run, timeout_error
from tests.support_pipeline import (
    G_A,
    GOOD_MENTIONS,
    OUT_OF_SCOPE,
    PACK,
    ROOT,
    FakeSource,
    ScriptedModel,
    harness_for,
    make_input,
    no_errors,
    normaliser,
    pipeline_config,
)

GATE = "requested_remedy_gate"
SLOT = "requested_remedy"
LOOKUP = "fin.cap.lookup_transaction"
THRESHOLD_BP = 9000
GATED = dataclasses.replace(pipeline_config(), slot_gates={SLOT: THRESHOLD_BP})


def decide_calls(model: ScriptedModel) -> list[dict[str, dict[str, Any]]]:
    """The questions of every decide call the model received, in order, by question id."""
    calls = []
    for request in model.requests:
        if request.schema_name == "harness_decide":
            body = request.user.split("QUESTIONS:\n", 1)[1].split("\n\n<<<DATA", 1)[0]
            calls.append({q["question_id"]: q for q in json.loads(body)})
    return calls


def piped(model: ScriptedModel, config: Any = GATED) -> Any:
    harness, _ = harness_for(model)
    return run(run_pipeline(make_input(), config, harness, FakeSource(()), normaliser, no_errors))


def model_for(*, gate: bool | None, p: float, pick: str | None = LOOKUP) -> ScriptedModel:
    answers: dict[str, Any] = dict(G_A)
    if gate is not None:
        answers[GATE] = gate
    if pick is not None:
        answers[SLOT] = pick
    return ScriptedModel(answers=answers, mentions=GOOD_MENTIONS, probability=p)


def remedy(result: Any) -> Any:
    assert result.proposal is not None
    return result.proposal.reference_slots[SLOT]  # the key is always there for a gated slot: a value, or None for absent


# --- stage 1: one fixed question, in one call with the other enumerated slots ---------------------------------------------------------------------

def test_the_gate_is_one_yes_no_in_the_fixed_words_and_the_slot_is_not_asked_as_a_choice_first() -> None:
    model = model_for(gate=True, p=0.95)
    piped(model)
    calls = decide_calls(model)
    gate_call = next(c for c in calls if GATE in c)
    assert gate_call[GATE]["kind"] == "yes_no"
    assert gate_call[GATE]["prompt"] == SLOT_GATE_V1 == "Does the customer explicitly ask for something to be done for them?"
    slots_call = next(c for c in calls if "question_type" in c)  # the required slot is still asked, in its old words, and without the gated one
    assert slots_call["question_type"]["prompt"] == "Which value of 'question_type' does the customer's message express?"
    assert SLOT not in slots_call and SLOT not in gate_call  # the capability is not offered with abstention, as a choice, in the first stage


def test_the_gate_wording_names_no_slot_no_value_and_no_example() -> None:
    for word in (SLOT, "remedy", "capability", "fin.", "resend", "transfer", "Tunde", "morning", "G-"):
        assert word not in SLOT_GATE_V1  # fixed words, data-neutral (INV-14, A533)


# --- the threshold: exact at 8999, 9000 and 9001 basis points; doubt abstains --------------------------------------------------------------------

@pytest.mark.parametrize(("p_yes", "passes"), [(0.8999, False), (0.9, True), (0.9001, True), (0.5, False), (0.0, False), (1.0, True)])
def test_the_gate_passes_exactly_at_and_above_the_pack_threshold(p_yes: float, passes: bool) -> None:
    result = piped(model_for(gate=True, p=p_yes))
    assert (remedy(result) == LOOKUP) is passes
    if not passes:
        assert remedy(result) is None  # absent, not a value with a doubt: doubt abstains


def test_a_models_own_yes_at_a_low_probability_is_doubt_and_abstains() -> None:
    """The model said "yes" but gave it 0.6: that is the harmful case (a false assertion), so the slot stays absent. `asserted` alone would take the model at its word."""
    assert remedy(piped(model_for(gate=True, p=0.6))) is None


def test_a_gate_that_says_no_leaves_the_slot_absent_and_asks_no_pick() -> None:
    model = model_for(gate=False, p=0.95)
    result = piped(model)
    assert remedy(result) is None
    assert not any(SLOT in call for call in decide_calls(model))  # no second question was ever put to the model


@pytest.mark.parametrize(("bp", "at", "below"), [(9500, 0.95, 0.9499), (7000, 0.7, 0.6999), (2900, 0.29, 0.2899), (1, 0.0001, 0.0)])
def test_exactly_at_any_threshold_passes_whatever_the_binary_value_of_the_probability(bp: int, at: float, below: float) -> None:
    """0.7 as a double lies below 7/10: read as the decimal the provider wrote, "exactly at the threshold" still passes, and one hair below does not."""
    config = dataclasses.replace(GATED, slot_gates={SLOT: bp})
    assert remedy(piped(model_for(gate=True, p=at), config)) == LOOKUP
    assert remedy(piped(model_for(gate=True, p=below), config)) is None


def test_the_threshold_is_the_packs_and_not_a_constant_in_the_core() -> None:
    strict = dataclasses.replace(GATED, slot_gates={SLOT: 9500})
    assert remedy(piped(model_for(gate=True, p=0.9), strict)) is None
    assert remedy(piped(model_for(gate=True, p=0.95), strict)) == LOOKUP
    loose = dataclasses.replace(GATED, slot_gates={SLOT: 5000})
    assert remedy(piped(model_for(gate=True, p=0.6), loose)) == LOOKUP


# --- stage 2: the pick has no abstention and is its own call ------------------------------------------------------------------------------------

def test_the_pick_is_a_second_call_without_the_abstention_and_carries_the_pack_vocabulary() -> None:
    model = model_for(gate=True, p=0.95)
    result = piped(model)
    assert remedy(result) == LOOKUP
    calls = decide_calls(model)
    pick_call = next(c for c in calls if SLOT in c)
    assert calls.index(pick_call) > calls.index(next(c for c in calls if GATE in c))  # the gate was asked first
    options = pick_call[SLOT]["options"]
    assert ABSTAIN not in [o["option_id"] for o in options]
    assert LOOKUP in " ".join(o["label"] for o in options)
    assert len(options) == len(PACK["capabilities"])  # the closed list, the whole of it, nothing added


def test_an_answer_outside_the_options_leaves_the_slot_absent_and_does_not_stop_the_turn() -> None:
    model = model_for(gate=True, p=0.95, pick=None)  # unscripted: the model "selects" none_or_other, which was not offered
    result = piped(model)
    assert result.proposal is not None and remedy(result) is None
    assert result.outcome.name == "PROPOSED"


class FailsOnThePick(ScriptedModel):
    """A provider that answers the gate and then cannot answer the pick: not an answer outside the options, a failure to answer."""

    async def complete(self, request: Any) -> Any:
        if request.schema_name == "harness_decide" and f'"question_id":"{SLOT}"' in request.user.replace(" ", ""):
            self.requests.append(request)
            raise timeout_error()
        return await super().complete(request)


def test_a_provider_that_cannot_answer_the_pick_still_stops_the_turn() -> None:
    result = piped(FailsOnThePick(answers={**G_A, GATE: True}, mentions=GOOD_MENTIONS, probability=0.95))
    assert result.outcome.name == "REJECTED_AT_GATE"  # fail closed: only an answer outside the options is "absent"


def test_the_pick_call_is_accounted_for() -> None:
    result = piped(model_for(gate=True, p=0.95))
    plain = piped(model_for(gate=False, p=0.95))
    assert len(result.calls) == len(plain.calls) + 1  # the pick is one more call, and it is on the record


def test_the_gate_probability_is_kept_on_the_proposal_for_the_log() -> None:
    result = piped(model_for(gate=True, p=0.95))
    assert result.proposal is not None
    assert result.proposal.probabilities[GATE]["yes"] == pytest.approx(0.95)


# --- what does not change -------------------------------------------------------------------------------------------------------------------

def test_a_slot_that_is_not_gated_keeps_the_optional_slot_question_exactly() -> None:
    model = ScriptedModel(answers={**G_A, SLOT: LOOKUP}, mentions=GOOD_MENTIONS)
    result = piped(model, pipeline_config())  # no slot_gates: the earlier behaviour (A533), prompt and abstention
    calls = decide_calls(model)
    assert all(GATE not in call for call in calls)
    asked = next(call for call in calls if SLOT in call)
    assert "explicitly ask" in asked[SLOT]["prompt"] and any(o["option_id"] == ABSTAIN for o in asked[SLOT]["options"])
    assert remedy(result) == LOOKUP


def test_a_required_slot_cannot_be_gated() -> None:
    with pytest.raises(ConfigError):
        load_pipeline_config(_pack_with_gates([{"slot": "question_type", "assertion_threshold_bp": 9000}]), ROOT / "packs", out_of_scope_node=OUT_OF_SCOPE)


# --- the harness: a pick may leave out the abstention, nothing else may -------------------------------------------------------------------

def _options(*ids: str) -> tuple[Option, ...]:
    return tuple(Option(i, f"label {i}") for i in ids)


def test_a_pick_only_choice_is_accepted_without_the_abstention() -> None:
    validate_questions((ChoiceQuestion("pick", "Which?", _options("o0", "o1"), pick_only=True),))


def test_a_pick_only_choice_may_not_carry_the_abstention() -> None:
    with pytest.raises(HarnessRejection) as caught:
        validate_questions((ChoiceQuestion("pick", "Which?", _options("o0", ABSTAIN), pick_only=True),))
    assert caught.value.reason is Reason.QUESTION_INVALID


def test_an_ordinary_choice_still_needs_the_abstention() -> None:
    with pytest.raises(HarnessRejection) as caught:
        validate_questions((ChoiceQuestion("pick", "Which?", _options("o0", "o1")),))
    assert caught.value.reason is Reason.NO_ABSTENTION
    validate_questions((YesNoQuestion("gate", "Yes or no?"),))


# --- the pack declares it, and the loader reads it as data ---------------------------------------------------------------------------------

def _pack_with_gates(gates: Any) -> dict[str, Any]:
    pack: dict[str, Any] = copy.deepcopy(PACK)
    if gates is None:
        pack["understanding"].pop("slot_gates", None)
    else:
        pack["understanding"]["slot_gates"] = gates
    return pack


def _load(pack: dict[str, Any]) -> Any:
    return load_pipeline_config(pack, ROOT / "packs", out_of_scope_node=OUT_OF_SCOPE)


def test_the_real_pack_gates_the_optional_slot_at_the_architects_interim_value() -> None:
    assert _load(copy.deepcopy(PACK)).slot_gates == {SLOT: THRESHOLD_BP}  # 9000: a Founder decision to confirm (A603)


def test_a_pack_without_gates_loads_with_none() -> None:
    assert _load(_pack_with_gates(None)).slot_gates == {}


@pytest.mark.parametrize("bad", [
    [{"slot": SLOT}],                                              # no threshold
    [{"slot": SLOT, "assertion_threshold_bp": 0}],                 # outside 1..10000
    [{"slot": SLOT, "assertion_threshold_bp": 10001}],
    [{"slot": SLOT, "assertion_threshold_bp": 90.5}],              # not an integer
    [{"slot": SLOT, "assertion_threshold_bp": True}],              # a bool is not a number
    [{"slot": SLOT, "assertion_threshold_bp": 9000, "extra": 1}],  # an unknown member
    [{"slot": SLOT, "assertion_threshold_bp": 9000}, {"slot": SLOT, "assertion_threshold_bp": 8000}],  # named twice
    [{"slot": "no_such_slot", "assertion_threshold_bp": 9000}],    # names no slot of any node
    [{"slot": "amount", "assertion_threshold_bp": 9000}],          # a slot that has no closed vocabulary
    "yes",
])
def test_a_malformed_or_impossible_gate_fails_at_load(bad: Any) -> None:
    with pytest.raises(ConfigError):
        _load(_pack_with_gates(bad))


def test_the_core_names_no_slot() -> None:
    """INV-14: the slot is pack data. The model service's pipeline code never spells it as a value."""
    root = Path(ROOT) / "services" / "model" / "noema_model"
    for path in (root / "understanding" / "pipeline").glob("*.py"):
        text = path.read_text(encoding="utf-8")
        assert f'"{SLOT}"' not in text and f"'{SLOT}'" not in text, path.name
