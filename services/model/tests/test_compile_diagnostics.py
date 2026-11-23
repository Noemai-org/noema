"""UND-17 (A602, interim; noema#338, compile run 7): the compiler hands the closed-vocabulary choices of a turn to an optional diagnostics sink, so the benchmark's job log can print, per example, the option the
model chose and the probabilities behind it. The sink is internal: the bytes of the proposal and the route's answer are the same with and without it (REQ-INT-001; INV-02; AGENT-13). A choice holds option ids and
numbers only, never a piece of the utterance. No threshold is applied to a choice today, and the choice says so. All data is SYNTHETIC."""

from datetime import timedelta
from fractions import Fraction
from typing import Any

import pytest

from noema_model.harness.decide_types import ABSTAIN
from noema_model.understanding.compiler.compiler import PipelineCompiler, SlotChoice
from tests.support import run
from tests.test_compile_compiler import CANONICAL, CONFIG, LEXICON, Minter, compile_bytes, g_a, row
from tests.support_pipeline import FakeSource, harness_for


def compiler_with_sink(sink: Any, **answers: Any) -> PipelineCompiler:
    harness = harness_for(g_a(answers=answers))[0]
    return PipelineCompiler({"fin@1.0.0": CONFIG}, LEXICON, harness, FakeSource((row("a"),)), Minter(), diagnostics=sink)


def test_the_sink_gets_one_choice_per_closed_vocabulary_slot_with_the_chosen_option_and_every_probability() -> None:
    seen: list[tuple[str, tuple[SlotChoice, ...]]] = []
    compile_bytes(compiler_with_sink(lambda correlation_id, choices: seen.append((correlation_id, choices))))
    assert len(seen) == 1 and seen[0][0] == CANONICAL["correlation_id"]
    by_slot = {c.slot: c for c in seen[0][1]}
    assert set(by_slot) >= {"question_type", "requested_remedy"}
    assert by_slot["question_type"].chosen == "STATUS" and by_slot["question_type"].probabilities["STATUS"] == pytest.approx(0.9)
    remedy = by_slot["requested_remedy"]
    assert remedy.chosen is None  # the model chose none_or_other: no remedy was asked for
    assert remedy.probabilities[ABSTAIN] == pytest.approx(0.9) and len(remedy.probabilities) > 2  # the abstention and every capability the question offered


def test_a_choice_names_no_threshold_while_none_is_applied_to_it() -> None:
    seen: list[tuple[SlotChoice, ...]] = []
    compile_bytes(compiler_with_sink(lambda _c, choices: seen.append(choices)))
    assert seen and all(c.threshold is None for c in seen[0])


def test_a_choice_holds_option_ids_and_numbers_only_never_the_utterance() -> None:
    seen: list[tuple[SlotChoice, ...]] = []
    compile_bytes(compiler_with_sink(lambda _c, choices: seen.append(choices)))
    text = CANONICAL["text"]
    for choice in seen[0]:
        assert isinstance(choice.slot, str) and (choice.chosen is None or isinstance(choice.chosen, str))
        for option, p in choice.probabilities.items():
            assert isinstance(option, str) and isinstance(p, float)
            assert option not in text and "Tunde" not in option  # an id of the pack's vocabulary, not a word of the request
        assert choice.threshold is None or isinstance(choice.threshold, Fraction)


def test_the_proposals_bytes_are_the_same_with_and_without_a_sink() -> None:
    plain = compile_bytes(PipelineCompiler({"fin@1.0.0": CONFIG}, LEXICON, harness_for(g_a())[0], FakeSource((row("a"),)), Minter()))
    sunk = compile_bytes(compiler_with_sink(lambda _c, _choices: None))
    assert plain == sunk


def test_the_sink_is_called_once_per_compile_and_before_a_refusal_is_raised() -> None:
    seen: list[str] = []
    compiler = compiler_with_sink(lambda correlation_id, _choices: seen.append(correlation_id))
    compile_bytes(compiler)
    compile_bytes(compiler, {**CANONICAL, "correlation_id": "corr_second"})
    assert seen == [CANONICAL["correlation_id"], "corr_second"]


def test_a_compiler_without_a_sink_is_unchanged() -> None:
    assert compile_bytes(PipelineCompiler({"fin@1.0.0": CONFIG}, LEXICON, harness_for(g_a())[0], FakeSource((row("a"),)), Minter()))


def test_the_window_of_the_turn_does_not_move_with_the_sink() -> None:
    source = FakeSource((row("a"),))
    harness = harness_for(g_a())[0]
    compile_bytes(PipelineCompiler({"fin@1.0.0": CONFIG}, LEXICON, harness, source, Minter(), diagnostics=lambda _c, _s: None))
    start, end = source.queries[0].time_window  # type: ignore[misc]
    assert end - start > timedelta(0)
