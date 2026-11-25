"""UND-18, the optional slot as a gate and a pick (ruling A603, interim; A533, A605). All data is SYNTHETIC.

Run #7 (45268856727) ended with goldens G-A, G-B and G-E MISSED on `requested_remedy`: asked as one choice with a one-in-21 abstention, the model picks a capability the customer never asked for.
A603 splits it. Stage 1 is a yes/no gate with one fixed sentence; it is read as a probability against a threshold that lives in PACK DATA (the Architect's interim 9000 basis points, a Founder
decision to confirm, never set by an agent, AGENT-16). Stage 2 picks the capability only when the gate passed, and has NO abstention. Doubt abstains: the slot is then absent, because a false
assertion is the harmful error. The wording of the optional-slot question for slots that are not gated does not change (A533), and no golden or input is touched.

What these tests prove: the gate is asked once, in the fixed words; the comparison is exact at 8999, 9000 and 9001 basis points; a model's own "yes" at a low probability is doubt; the pick has no
abstention and is a separate call that is made only after a passed gate; an answer outside the options leaves the slot absent and does not stop the turn; required slots and non-gated slots are
unchanged; a pack declares the gate as data, and the loader refuses a gate that is malformed or that names a slot that cannot be gated; the core names no slot.
"""

import ast
import copy
import dataclasses
import json
from pathlib import Path
from typing import Any, cast

import pytest

from noema_model.harness import ABSTAIN, ChoiceQuestion, HarnessRejection, Option, YesNoQuestion
from noema_model.harness.decide_types import validate_questions
from noema_model.harness.errors import Reason
from noema_model.platform.config import ConfigError
from noema_model.understanding.compiler.packconfig import load_pipeline_config
from noema_model.understanding.pipeline import run_pipeline
from noema_model.harness import DecideRequest, DecisionTask, GateAnswer, GateQuestion
from noema_model.harness.rawjson import NonNumber, RawNumber, loads_lossless
from noema_model.understanding.pipeline.steps import gate_asserted, probability_bp
from noema_model.understanding.pipeline.templates import SLOT_GATE_V1
from tests.support import reply, run, timeout_error
from tests.test_openai_decisions import stack
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
    assert result.proposal.probabilities[GATE] == {"yes_bp": 9500}  # whole basis points, an integer: the reading the gate used


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


# --- A612 and A649: the probability is the raw JSON number token, read exactly in integer basis points, and a bad one costs the slot, not the compile ------------------

def test_the_decoder_keeps_the_numeric_lexeme_and_never_makes_a_float() -> None:
    doc = loads_lossless('{"a": 9e-1, "b": 0.90, "c": 1, "d": -0.1, "e": [1.0, 9E-1]}')
    assert doc["a"] == RawNumber("9e-1") and doc["b"] == RawNumber("0.90") and doc["c"] == RawNumber("1") and doc["d"] == RawNumber("-0.1")
    assert doc["e"] == [RawNumber("1.0"), RawNumber("9E-1")]
    assert not any(isinstance(v, float) for v in (doc["a"], doc["b"], doc["c"], doc["d"]))


def test_the_decoder_names_the_non_finite_tokens_as_not_numbers() -> None:
    doc = loads_lossless('{"a": NaN, "b": Infinity, "c": -Infinity}')
    assert doc == {"a": NonNumber("NaN"), "b": NonNumber("Infinity"), "c": NonNumber("-Infinity")}


@pytest.mark.parametrize("bad", ['{"a": 1, "a": 2}', "", "{", '{"a": +0.9}', '{"a": .9}', '{"a": 0.}', '{"a": 0x1}', "not json", '{"a": 1} trailing'])
def test_invalid_json_and_a_repeated_key_are_refused_by_the_decoder(bad: str) -> None:
    with pytest.raises(ValueError):
        loads_lossless(bad)


@pytest.mark.parametrize(("text", "bp"), [
    ("0.9", 9000), ("0.9000", 9000), ("0.89999", 8999), ("1", 10000), ("1.0", 10000), ("0", 0), ("0.0", 0),
    ("0.95", 9500), ("0.7", 7000), ("0.0001", 1), ("0.00009", 0), ("0.99999999", 9999), ("1.000", 10000), ("0.12345678901234567", 1234),
])
def test_a_plain_decimal_token_in_range_becomes_basis_points_by_floor(text: str, bp: int) -> None:
    assert probability_bp(RawNumber(text)) == bp


@pytest.mark.parametrize("bad", [
    "1.0001", "1.00009", "1.000000000000000001", "1.1", "2", "10", "100.0", "-0.1", "+0.9", "1e0", "1E0", "9e-1", "9E-1", "0.9e0", ".9", "0.", "", " 0.9", "0.9 ", "0.9\n", "NaN", "Infinity", "-Infinity",
    "0x1", "0,9", "0..9", "0.9.1", "\u0660.\u0669", "٠.٩", "0.9\u00a0", "1_0", "0." + "9" * 40, "-0", "-0.0",
])
def test_a_token_that_is_not_a_plain_decimal_in_range_is_not_a_probability(bad: str) -> None:
    assert probability_bp(RawNumber(bad)) is None


@pytest.mark.parametrize("not_a_token", ["0.9", "1", " 0.9", 0.9, 1, 0, 0.0, 1.0, True, False, None, [], {}, object(), NonNumber("NaN"), float("nan")])
def test_only_a_raw_number_token_is_a_probability_a_string_a_float_or_a_bool_is_not(not_a_token: Any) -> None:
    """A quoted string "0.9" is not a number, and a Python float has already lost the text it was written with (9e-1 and 0.9 are the same float)."""
    assert probability_bp(not_a_token) is None


def test_the_gate_compares_integers_against_the_pack_threshold() -> None:
    at, below = GateAnswer("g", None, RawNumber("0.9")), GateAnswer("g", None, RawNumber("0.89999"))
    assert gate_asserted(at, 9000) is True and gate_asserted(below, 9000) is False
    assert gate_asserted(GateAnswer("g", True, RawNumber("0.89999")), 9000) is False  # a yes below the threshold abstains (A606)
    assert gate_asserted(GateAnswer("g", False, RawNumber("1")), 9000) is False  # a no is a no (A606)
    assert gate_asserted(GateAnswer("g", True, None), 9000) is False  # a missing probability is doubt


# --- end to end: raw provider JSON bytes through the real harness, the real decoder and the real pipeline -------------------------------------------------

ABSENT = object()  # the probability of "yes" is not in the reply at all
NO_PROBABILITIES = object()  # the answer has no `probabilities` member at all


class RawReplyModel(ScriptedModel):
    """Answers like ScriptedModel, but writes the gate's probability of "yes" as the exact text given, which json.dumps could never produce (a string, null, an exponent, NaN)."""

    lexeme: Any = "0.95"

    async def complete(self, request: Any) -> Any:
        response = await super().complete(request)
        if request.schema_name != "harness_decide" or GATE not in request.user:
            return response
        document = json.loads(response.output_json)
        answer = next(a for a in document["answers"] if a["question_id"] == GATE)
        if self.lexeme is NO_PROBABILITIES:
            del answer["probabilities"]
        elif self.lexeme is ABSENT:
            answer["probabilities"] = [e for e in answer["probabilities"] if e["option"] != "yes"]
        else:
            next(e for e in answer["probabilities"] if e["option"] == "yes")["p"] = "@@P@@"
        text = json.dumps(document).replace('"@@P@@"', "" if self.lexeme in (ABSENT, NO_PROBABILITIES) else str(self.lexeme))
        return dataclasses.replace(response, output_json=text)


def raw_model(lexeme: Any, *, pick: str = LOOKUP) -> RawReplyModel:
    model = RawReplyModel(answers={**G_A, GATE: True, SLOT: pick}, mentions=GOOD_MENTIONS, probability=0.95)
    model.lexeme = lexeme
    return model


@pytest.mark.parametrize("lexeme", ["0.9", "0.90", "0.9000", "1", "1.0", "0.95", "0.99999"])
def test_a_plain_decimal_at_or_above_the_threshold_passes_end_to_end(lexeme: str) -> None:
    assert remedy(piped(raw_model(lexeme))) == LOOKUP


@pytest.mark.parametrize("lexeme", ["0", "0.0", "0.89999", "0.8999", "0.5"])
def test_a_plain_decimal_below_the_threshold_abstains_end_to_end(lexeme: str) -> None:
    result = piped(raw_model(lexeme))
    assert result.outcome.name == "PROPOSED" and remedy(result) is None


@pytest.mark.parametrize("lexeme", [
    "9e-1", "9E-1", "9e-01", "0.9e0", "9e-1".upper(), '"0.9"', '" 0.9"', '"0.9 "', '""', "null", "true", "false", "-0.1", "-0", "1.0001", "1.00009", "NaN", "Infinity", "-Infinity", "[]", "{}", ABSENT, NO_PROBABILITIES,
])
def test_a_malformed_probability_abstains_for_that_slot_only_and_the_compile_goes_on(lexeme: Any) -> None:
    result = piped(raw_model(lexeme))
    assert result.outcome.name == "PROPOSED", lexeme  # not stopped
    assert remedy(result) is None, lexeme  # the slot is absent: a missing or malformed probability is doubt, never a yes
    assert result.proposal is not None and result.proposal.reference_slots["question_type"] == "STATUS"  # the other slots are untouched


@pytest.mark.parametrize("lexeme", ["+0.9", ".9", "0.", "0x1", "0,9", "1_0", "0.9.1", "--1", "01.5e"])
def test_text_that_is_not_json_at_all_still_stops_the_turn(lexeme: str) -> None:
    result = piped(raw_model(lexeme))
    assert result.outcome.name == "REJECTED_AT_GATE"  # the same refusal as any reply that is not JSON: the compile service reports ProviderUnavailable


def test_a_gate_answer_that_selects_neither_yes_nor_no_still_refuses_the_whole_reply() -> None:
    """Only the probability is lenient. The answer's own selection must still be yes or no."""
    class SelectsMaybe(RawReplyModel):
        async def complete(self, request: Any) -> Any:
            response = await super().complete(request)
            if request.schema_name == "harness_decide" and GATE in request.user:
                document = json.loads(response.output_json)
                next(a for a in document["answers"] if a["question_id"] == GATE)["selected"] = "maybe"
                return dataclasses.replace(response, output_json=json.dumps(document))
            return response

    model = SelectsMaybe(answers={**G_A, GATE: True, SLOT: LOOKUP}, mentions=GOOD_MENTIONS, probability=0.95)
    assert piped(model).outcome.name == "REJECTED_AT_GATE"


def test_a_malformed_probability_on_a_question_that_is_not_a_gate_still_refuses_the_reply() -> None:
    """Only the gate's probability is lossless and abstains. Every other yes/no keeps the harness's strict reading: a number in [0, 1], else the whole reply is refused."""
    from noema_model.harness import YesNoQuestion
    from tests.test_openai_decisions import FLAG, decide

    bad = reply({"answers": [{"question_id": "multiple_intents", "selected": "yes", "probabilities": [{"option": "yes", "p": "0.9"}, {"option": "no", "p": 0.1}]}]}, version="model-gpt-2026-01-01")
    harness, _, _ = stack([], llm_steps=[bad], route=("gpt",))
    assert isinstance(FLAG, YesNoQuestion)
    with pytest.raises(HarnessRejection):
        decide(harness, FLAG)


def test_a_gate_is_never_sent_to_a_decision_model_which_would_return_a_float() -> None:
    """A decision provider's reply is parsed to numbers at the vendor envelope, so it cannot prove what text a probability was written in: a set with a gate goes to the LLM route (A649)."""
    llm = reply({"answers": [{"question_id": "requested_remedy_gate", "selected": "yes", "probabilities": [{"option": "yes", "p": 0.95}, {"option": "no", "p": 0.05}]}]}, version="model-gpt-2026-01-01")
    harness, native, gpt = stack([], llm_steps=[llm])
    result = run(harness.decide(DecideRequest(DecisionTask.ENUMERATED_SLOT, {"utterance": "check it"}, (GateQuestion("requested_remedy_gate", SLOT_GATE_V1),), None, "corr_test")))
    assert native.calls == [] and len(gpt.calls) == 1
    assert result.answers == (GateAnswer("requested_remedy_gate", True, RawNumber("0.95")),)


def test_the_gate_reads_a_token_and_the_decode_boundary_makes_no_float() -> None:
    """A612: no float(), no Decimal, no math on floats in the gate's reading; and the decoder of the harness never builds a float for the gate."""
    pipeline = Path(ROOT) / "services" / "model" / "noema_model"
    for relative, names in (("understanding/pipeline/steps.py", ("probability_bp", "gate_asserted")), ("harness/rawjson.py", ("loads_lossless",))):
        tree = ast.parse((pipeline / relative).read_text(encoding="utf-8"))
        found = {n.name: n for n in ast.walk(tree) if isinstance(n, ast.FunctionDef) and n.name in names}
        assert set(found) == set(names), relative
        for function in found.values():
            for node in ast.walk(function):
                if isinstance(node, ast.Call) and isinstance(node.func, ast.Name):
                    assert node.func.id not in ("float", "Decimal", "round"), (function.name, node.func.id)
                if isinstance(node, ast.Constant):
                    assert not isinstance(node.value, float), (function.name, node.value)
                if isinstance(node, ast.Name):
                    assert node.id not in ("Fraction", "Decimal", "math"), (function.name, node.id)


# --- P2 of the review: a gate belongs to one node ------------------------------------------------------------------------------------------------------

def _two_node_pack() -> dict[str, Any]:
    """The real pack plus a second node that declares an optional closed-list slot of the same name: a gate on the first node must never reach it."""
    pack: dict[str, Any] = copy.deepcopy(PACK)
    other = copy.deepcopy(next(n for n in pack["intent_nodes"] if n["node_id"] == "fin.meta.out_of_scope"))
    other["node_id"] = "fin.other.node"
    other["slots"] = [{"name": SLOT, "type": "Reference", "required": False, "references": "capability"}]
    pack["intent_nodes"].append(other)
    return pack


def test_a_gated_slot_name_declared_by_two_nodes_is_refused_at_load() -> None:
    with pytest.raises(ConfigError, match="more than one node"):
        _load(_two_node_pack())


def test_a_config_built_directly_cannot_apply_one_gate_to_two_nodes_either() -> None:
    from noema_model.understanding.pipeline import NodeCatalogue

    catalogue = NodeCatalogue.from_pack(_two_node_pack(), out_of_scope_node=OUT_OF_SCOPE)
    with pytest.raises(ConfigError, match="more than one node"):
        dataclasses.replace(GATED, catalogue=catalogue)


def test_a_gate_on_one_node_never_asks_the_same_named_slot_of_another_node() -> None:
    """With the pack's own node the gate is asked; the gated names of `gated_slots` are those of the one node it is given."""
    from noema_model.understanding.pipeline import steps

    catalogue = GATED.catalogue
    node = catalogue.nodes["fin.payments.exception.investigate"]
    assert [s.name for s in steps.gated_slots(node, GATED.slot_gates)] == [SLOT]
    assert steps.gated_slots(catalogue.nodes[OUT_OF_SCOPE], GATED.slot_gates) == ()


# --- the review's finding (EL, #932): a gate answer that repeats an option contradicts itself, so the whole reply is refused (INV-02, A649) --------------------
# The ordinary path refuses a repeated option (`_answers`: OUTPUT_MISMATCH). The gate's decoder read the list itself and let the LAST "yes" win, so a reply that said 0.10 and then 0.95 asserted the slot.


def _entries(*pairs: tuple[str, str]) -> str:
    """The text of a `probabilities` array: each pair is an option and the exact text of its `p`."""
    return "[" + ",".join(f'{{"option":"{option}","p":{text}}}' for option, text in pairs) + "]"


class RawProbabilitiesModel(ScriptedModel):
    """Answers like ScriptedModel, but writes the gate answer's `probabilities` member as the exact text given, which json.dumps of a dict could never produce (an option twice)."""

    probabilities_text: str = '[{"option":"yes","p":0.95},{"option":"no","p":0.05}]'

    async def complete(self, request: Any) -> Any:
        response = await super().complete(request)
        if request.schema_name != "harness_decide" or GATE not in request.user:
            return response
        document = json.loads(response.output_json)
        next(a for a in document["answers"] if a["question_id"] == GATE)["probabilities"] = "@@PROBS@@"
        return dataclasses.replace(response, output_json=json.dumps(document).replace('"@@PROBS@@"', self.probabilities_text))


def probs_model(entries: str, *, gate: bool = True) -> RawProbabilitiesModel:
    model = RawProbabilitiesModel(answers={**G_A, GATE: gate, SLOT: LOOKUP}, mentions=GOOD_MENTIONS, probability=0.95)
    model.probabilities_text = entries
    return model


REPEATED_OPTION = {
    "the EL scenario: yes 0.10 then yes 0.95 (the last one used to win)": _entries(("yes", "0.10"), ("yes", "0.95"), ("no", "0.05")),
    "two yes, the high one first": _entries(("yes", "0.95"), ("yes", "0.10"), ("no", "0.05")),
    "two yes with the no entry between them": _entries(("yes", "0.10"), ("no", "0.05"), ("yes", "0.95")),
    "two yes after the no entry": _entries(("no", "0.05"), ("yes", "0.10"), ("yes", "0.95")),
    "two yes before the no entry": _entries(("yes", "0.95"), ("yes", "0.95"), ("no", "0.05")),
    "three yes": _entries(("yes", "0.10"), ("yes", "0.20"), ("yes", "0.95"), ("no", "0.05")),
    "two no after the yes": _entries(("yes", "0.95"), ("no", "0.05"), ("no", "0.50")),
    "two no before the yes": _entries(("no", "0.05"), ("no", "0.50"), ("yes", "0.95")),
    "two no around the yes": _entries(("no", "0.05"), ("yes", "0.95"), ("no", "0.50")),
    "two yes, the second quoted": _entries(("yes", "0.95"), ("yes", '"0.95"'), ("no", "0.05")),
    "two yes, the second null": _entries(("yes", "0.95"), ("yes", "null"), ("no", "0.05")),
    "two yes, the second NaN": _entries(("yes", "0.95"), ("yes", "NaN"), ("no", "0.05")),
    "two yes, the first malformed": _entries(("yes", '"0.95"'), ("yes", "0.95"), ("no", "0.05")),
    "two yes, the second with an exponent": _entries(("yes", "0.95"), ("yes", "9e-1"), ("no", "0.05")),
}


@pytest.mark.parametrize("entries", list(REPEATED_OPTION.values()), ids=list(REPEATED_OPTION))
def test_a_gate_answer_that_repeats_an_option_refuses_the_whole_reply_and_asserts_nothing(entries: str) -> None:
    """INV-02: whole-reply, strict, no repair. A reply that contradicts itself is not read: no token is chosen from it, so the slot cannot be asserted (the compile reports it as any refused reply)."""
    model = probs_model(entries)
    result = piped(model)
    assert result.outcome.name == "REJECTED_AT_GATE", entries
    assert result.proposal is None, entries
    assert not any(SLOT in call for call in decide_calls(model)), entries  # and no pick was asked


def test_a_gate_answer_that_says_no_and_repeats_yes_is_refused_too() -> None:
    """The repeat is refused whatever the answer selects: the ordinary path does not look at the selection first either."""
    result = piped(probs_model(REPEATED_OPTION["the EL scenario: yes 0.10 then yes 0.95 (the last one used to win)"], gate=False))
    assert result.outcome.name == "REJECTED_AT_GATE"


@pytest.mark.parametrize(("entries", "expected"), [
    (_entries(("yes", "0.90"), ("no", "0.10")), LOOKUP),  # exactly the threshold passes
    (_entries(("no", "0.05"), ("yes", "0.95")), LOOKUP),  # the no entry first is not a repeat
    (_entries(("yes", "0.95")), LOOKUP),  # the no entry is not required, only the yes is read
    (_entries(("no", "0.50"), ("yes", "0.89999")), None),  # below the threshold abstains, as before
])
def test_a_gate_answer_with_each_option_once_is_read_as_before(entries: str, expected: str | None) -> None:
    """The control: a single yes entry, in either order, asserts at the threshold and abstains below it."""
    result = piped(probs_model(entries))
    assert result.outcome.name == "PROPOSED" and remedy(result) == expected, entries


def test_a_repeated_option_on_a_question_that_is_not_a_gate_is_refused_as_before() -> None:
    """Only the gate's decoder changed: a repeated option on any other question of a decide call still refuses that call's reply, the ordinary path."""
    class RepeatsOnAnotherQuestion(ScriptedModel):
        repeated = 0

        async def complete(self, request: Any) -> Any:
            response = await super().complete(request)
            if request.schema_name != "harness_decide":
                return response
            document = json.loads(response.output_json)
            others = [a for a in document["answers"] if a["question_id"] != GATE]
            if not others:
                return response
            others[0]["probabilities"] = [*others[0]["probabilities"], others[0]["probabilities"][0]]
            self.repeated += 1
            return dataclasses.replace(response, output_json=json.dumps(document))

    model = RepeatsOnAnotherQuestion(answers={**G_A, GATE: True, SLOT: LOOKUP}, mentions=GOOD_MENTIONS, probability=0.95)
    result = piped(model)
    assert model.repeated >= 1  # the double did repeat an option on a question that is not a gate
    assert result.outcome.name == "REJECTED_AT_GATE"


def test_the_ordinary_path_refuses_a_repeated_option_with_the_same_reason() -> None:
    """The reason a gate answer's repeat must share (OUTPUT_MISMATCH), pinned on a question that is not a gate."""
    from tests.test_openai_decisions import FLAG, decide

    twice = reply({"answers": [{"question_id": "multiple_intents", "selected": "yes", "probabilities": [{"option": "yes", "p": 0.1}, {"option": "yes", "p": 0.95}, {"option": "no", "p": 0.05}]}]}, version="model-gpt-2026-01-01")
    harness, _, _ = stack([], llm_steps=[twice], route=("gpt",))
    with pytest.raises(HarnessRejection) as caught:
        decide(harness, FLAG)
    assert caught.value.reason is Reason.OUTPUT_MISMATCH


def test_a_repeated_option_in_a_gate_reply_is_refused_with_the_same_reason_at_the_harness() -> None:
    """The harness itself, not only the pipeline: the same reason as the ordinary path, and no answer is returned."""
    twice = reply({"answers": [{"question_id": GATE, "selected": "yes", "probabilities": []}]}, version="model-gpt-2026-01-01", raw='{"answers":[{"question_id":"requested_remedy_gate","selected":"yes","probabilities":' + _entries(("yes", "0.10"), ("yes", "0.95"), ("no", "0.05")) + "}]}")
    harness, _, _ = stack([], llm_steps=[twice], route=("gpt",))
    with pytest.raises(HarnessRejection) as caught:
        run(harness.decide(DecideRequest(DecisionTask.ENUMERATED_SLOT, {"utterance": "check it"}, (GateQuestion(GATE, SLOT_GATE_V1),), None, "corr_test")))
    assert caught.value.reason is Reason.OUTPUT_MISMATCH
