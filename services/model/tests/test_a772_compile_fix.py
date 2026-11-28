"""A772 (interim; amends A605; compile run 8; UND-16, UND-17, UND-18; REQ-INT-001, REQ-INT-002, REQ-TST-006; INV-02, INV-14, INV-22, AGENT-13, AGENT-16). All data is SYNTHETIC.

Run 8 failed two ways: the gate treated a request to CHECK a status as something to be done (G-B), and the payment-investigation node did not take an advice or remedy request (G-E: "resend am for me" read
as none_or_other). Its evidence also showed `threshold=n/a` and no yes/no answer for the gate. The fix is in the wording, the pack's node description and the diagnostics, with no benchmark-only branch and no change
to a golden or to the 9000 basis-point threshold.

These tests prove: (1) the gate's fixed sentence asks for an explicit ask for an action and rules out a status question and a request to check, show, look up or explain; (2) the node carries a description, pack data,
that covers advice and remedy requests, and the classifier is shown it; neither text copies a phrasing of BO's labelled development set (INV-22, REQ-TST-006); (3) the A602 lines carry, for the gate, the model's
selected answer, the probability of yes and the pack's threshold, ids and numbers only (AGENT-13).
"""

import dataclasses
import json
import re
from fractions import Fraction
from typing import Any

import pytest

from noema_model.understanding.compiler.compiler import PipelineCompiler, SlotChoice
from noema_model.understanding.pipeline import NodeCatalogue, steps
from noema_model.understanding.pipeline.templates import SLOT_GATE_V1
from tests.support import run
from tests.support_pipeline import NODE, OUT_OF_SCOPE, PACK, ROOT, FakeSource, ScriptedModel, harness_for, make_input
from tests.test_compile_compiler import CONFIG, LEXICON, Minter, compile_bytes, g_a, row

GATE = "requested_remedy_gate"
SLOT = "requested_remedy"
THRESHOLD_BP = 9000
GATED = dataclasses.replace(CONFIG, slot_gates={SLOT: THRESHOLD_BP})
LABELLED = json.loads((ROOT / "benchmark" / "compile_acceptance" / "remedy_labelled.v1.json").read_text(encoding="utf-8"))["items"]


def words(text: str) -> list[str]:
    return re.findall(r"[a-z0-9']+", text.lower())


def description() -> str | None:
    return getattr(NodeCatalogue.from_pack(PACK, out_of_scope_node=OUT_OF_SCOPE).nodes[NODE], "description", None)


# --- (1) the gate's wording ---------------------------------------------------------------------------------------------------------------------------

def test_the_gate_asks_for_an_explicit_ask_for_an_action_and_rules_out_a_status_question_or_a_lookup() -> None:
    text = SLOT_GATE_V1.lower()
    for needed in ("explicitly", "action", "status", "check", "show", "look up", "explain"):
        assert needed in text, needed
    assert "something to be done" not in text  # the wording run 8 showed read a status check as an action


def test_the_gate_wording_fits_the_frozen_harness_limit_so_it_is_never_clipped() -> None:
    limit = json.loads((ROOT / "benchmark" / "decisions" / "harness-gpt-6-luna.json").read_text(encoding="utf-8"))["data_limits"]["default_max_chars"]
    assert len(SLOT_GATE_V1) <= limit, (len(SLOT_GATE_V1), limit)  # the harness clips a question's prompt at that length: a longer sentence would reach the model cut off


def test_the_gate_wording_stays_data_neutral_and_is_one_question() -> None:
    assert SLOT_GATE_V1.count("?") >= 1 and "\n" not in SLOT_GATE_V1
    for word in (SLOT, "remedy", "capability", "fin.", "transfer", "Tunde", "morning", "G-"):
        assert word not in SLOT_GATE_V1


# --- (2) the node description, and no copied phrasing -------------------------------------------------------------------------------------------------------

def test_the_investigation_node_has_a_description_in_pack_data_that_covers_advice_and_remedy_requests() -> None:
    text = (description() or "").lower()
    assert text, "the node carries no description"
    assert "advice" in text and "remedy" in text


def test_the_classifier_is_shown_the_node_description() -> None:
    catalogue = NodeCatalogue.from_pack(PACK, out_of_scope_node=OUT_OF_SCOPE)
    model = ScriptedModel(answers={"intent_node": NODE})
    harness, _ = harness_for(model)
    run(steps.classify_node(harness, catalogue, make_input()))
    body = model.requests[0].user.split("QUESTIONS:\n", 1)[1].split("\n\n<<<DATA", 1)[0]
    labels = [o["label"] for o in json.loads(body)[0]["options"]]
    shown = next(label for label in labels if NODE in label)
    assert (description() or "<none>") in shown


def test_the_option_label_of_every_node_fits_the_frozen_harness_limit_so_the_node_id_is_never_clipped() -> None:
    limit = json.loads((ROOT / "benchmark" / "decisions" / "harness-gpt-6-luna.json").read_text(encoding="utf-8"))["data_limits"]["default_max_chars"]
    for node in NodeCatalogue.from_pack(PACK, out_of_scope_node=OUT_OF_SCOPE).nodes.values():
        label = f"{node.display_name}: {node.description} ({node.node_id})" if node.description else f"{node.display_name} ({node.node_id})"
        assert len(label) <= limit, (node.node_id, len(label), limit)


@pytest.mark.parametrize("which", ["the gate wording", "the node description"])
def test_no_labelled_phrasing_is_copied_into_a_prompt_text(which: str) -> None:
    text = SLOT_GATE_V1 if which == "the gate wording" else (description() or "")
    mine = words(text)
    runs = {tuple(mine[i : i + 5]) for i in range(len(mine) - 4)}
    for item in LABELLED:
        theirs = words(item["text"])
        copied = [tuple(theirs[i : i + 5]) for i in range(len(theirs) - 4) if tuple(theirs[i : i + 5]) in runs]
        assert not copied, (item["id"], which)
        assert " ".join(theirs) not in " ".join(mine), (item["id"], which)


# --- (3) the A602 lines carry the gate ----------------------------------------------------------------------------------------------------------------------

def compiler(**answers: Any) -> tuple[PipelineCompiler, list[tuple[SlotChoice, ...]]]:
    seen: list[tuple[SlotChoice, ...]] = []
    model = g_a(answers=answers)
    c = PipelineCompiler({"fin@1.0.0": GATED}, LEXICON, harness_for(model)[0], FakeSource((row("a"),)), Minter(), diagnostics=lambda _c, choices: seen.append(choices))
    return c, seen


def gate_line(seen: list[tuple[SlotChoice, ...]]) -> SlotChoice:
    assert seen, "the sink was not called"
    lines = [c for c in seen[0] if c.slot == GATE]
    assert len(lines) == 1, [c.slot for c in seen[0]]
    return lines[0]


def test_a_gate_that_the_model_answered_yes_logs_the_answer_its_probability_and_the_packs_threshold() -> None:
    c, seen = compiler(**{GATE: True, SLOT: "fin.cap.lookup_transaction"})
    compile_bytes(c)
    line = gate_line(seen)
    assert line.chosen == "yes" and line.threshold == Fraction(THRESHOLD_BP, 10000)
    assert line.probabilities == {"yes": pytest.approx(0.9)}


def test_a_gate_the_model_answered_no_logs_the_no_with_the_same_threshold() -> None:
    c, seen = compiler(**{GATE: False})
    compile_bytes(c)
    line = gate_line(seen)
    assert line.chosen == "no" and line.threshold == Fraction(THRESHOLD_BP, 10000)
    assert line.probabilities == {"yes": pytest.approx(0.1)}


def test_a_yes_below_the_threshold_is_logged_as_the_models_yes_with_the_probability_that_made_it_doubt() -> None:
    c, seen = compiler_low_confidence()
    compile_bytes(c)
    line = gate_line(seen)
    assert line.chosen == "yes" and line.probabilities == {"yes": pytest.approx(0.5)} and line.threshold == Fraction(THRESHOLD_BP, 10000)


def compiler_low_confidence() -> tuple[PipelineCompiler, list[tuple[SlotChoice, ...]]]:
    seen: list[tuple[SlotChoice, ...]] = []
    model = g_a(answers={GATE: True}, probability=0.5)
    c = PipelineCompiler({"fin@1.0.0": GATED}, LEXICON, harness_for(model)[0], FakeSource((row("a"),)), Minter(), diagnostics=lambda _c, choices: seen.append(choices))
    return c, seen


def test_a_pick_line_still_names_no_threshold_and_a_pack_with_no_gate_has_no_gate_line() -> None:
    c, seen = compiler(**{GATE: True, SLOT: "fin.cap.lookup_transaction"})
    compile_bytes(c)
    assert all(ch.threshold is None for ch in seen[0] if ch.slot != GATE)
    plain: list[tuple[SlotChoice, ...]] = []
    ungated = PipelineCompiler({"fin@1.0.0": CONFIG}, LEXICON, harness_for(g_a())[0], FakeSource((row("a"),)), Minter(), diagnostics=lambda _c, choices: plain.append(choices))
    compile_bytes(ungated)
    assert plain and all(ch.slot != GATE for ch in plain[0])


def test_a_gate_line_holds_ids_and_numbers_only() -> None:
    c, seen = compiler(**{GATE: True, SLOT: "fin.cap.lookup_transaction"})
    compile_bytes(c)
    line = gate_line(seen)
    assert isinstance(line.chosen, str) and line.chosen in {"yes", "no"}
    assert all(isinstance(k, str) and isinstance(v, float) for k, v in line.probabilities.items())
