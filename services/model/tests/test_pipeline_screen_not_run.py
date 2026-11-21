"""UND-05, A437 (committed in docs/decisions): a screen that did not run has its own INTERNAL class, SCREEN_NOT_RUN; the public error stays PROVIDER_UNAVAILABLE and there is no
contract change (REQ-CTX-007, REQ-AUD-001, REQ-INT-002; INV-02, INV-19, AGENT-09, AGENT-13, SoT §29). All data is SYNTHETIC.

Since noema#792 a refused screen ends the turn at the gate with the harness's code, and a provider outage on any required step ends it the same way: nothing in the result told the two
apart. The result now names the screen that did not run (`screen_not_run`: the utterance's, the candidates', or both), and the compiler raises `ScreenNotRun`, a ProviderUnavailable, so the
public answer is unchanged. A finding (the screen ran and flagged) is not a screen that did not run.
"""

import json
from typing import Any

import pytest

from noema_model.harness import Harness
from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.llm.base import ProviderError, ProviderFailure
from noema_model.understanding.compiler.service import ProviderUnavailable, ScreenNotRun
from noema_model.understanding.pipeline import Outcome, ScreenStage, run_pipeline
from tests.support import MASKING, ManualClock, ScriptedProvider, make_config, run
from tests.support_pipeline import (
    G_A,
    GOOD_MENTIONS,
    FakeSource,
    ScriptedModel,
    candidate,
    full_routes,
    harness_for,
    make_input,
    no_errors,
    normaliser,
    pipeline_config,
)
from tests.test_compile_compiler import CANONICAL, compiler_for, row
from tests.test_compile_route import PACK_REF, ROUTE, Compiler, client, post
from tests.test_pipeline_screen_closed import HOSTILE_CANDIDATE, SCREEN_ROUTE, refusing

CONFIG = pipeline_config()
MODES = ["unavailable", "timeout", "not_json", "no_answers"]


@pytest.mark.parametrize("mode", MODES)
def test_a_refused_utterance_screen_is_named_in_the_result(mode: str) -> None:
    """A437: the utterance screen did not run, and the result says so in its own field, not only through the harness code."""
    harness, _ = harness_for(refusing(mode))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource((candidate("a"),)), normaliser, no_errors))
    assert result.outcome is Outcome.REJECTED_AT_GATE
    assert result.screen_not_run == (ScreenStage.UTTERANCE,)


@pytest.mark.parametrize("mode", MODES)
def test_a_refused_candidate_screen_is_named_in_the_result(mode: str) -> None:
    """A437: the candidates' display names are enterprise text; their screen not running is the second stage."""
    harness, _ = harness_for(refusing(mode, only_containing=HOSTILE_CANDIDATE))
    source = FakeSource((candidate("a", HOSTILE_CANDIDATE),))
    result = run(run_pipeline(make_input(), CONFIG, harness, source, normaliser, no_errors))
    assert result.outcome is Outcome.REJECTED_AT_GATE
    assert result.screen_not_run == (ScreenStage.CANDIDATES,)


def test_no_route_for_the_screen_is_a_screen_that_did_not_run() -> None:
    harness, _ = harness_for(ScriptedModel(answers=dict(G_A), mentions=GOOD_MENTIONS), routes=full_routes(drop=(SCREEN_ROUTE,)))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource((candidate("a"),)), normaliser, no_errors))
    assert result.outcome is Outcome.REJECTED_AT_GATE
    assert result.screen_not_run == (ScreenStage.UTTERANCE,)


def test_a_required_step_other_than_the_screen_refused_is_not_a_screen_that_did_not_run() -> None:
    """SCREEN_NOT_RUN is not a synonym for 'rejected': a route missing for the slot step, with the screen fine, is a rejection only."""
    harness, _ = harness_for(ScriptedModel(answers=dict(G_A), mentions=GOOD_MENTIONS), routes=full_routes(drop=("decide.enumerated_slot",)))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource((candidate("a"),)), normaliser, no_errors))
    assert result.outcome is Outcome.REJECTED_AT_GATE
    assert result.screen_not_run == ()


def test_a_screen_that_ran_is_not_a_screen_that_did_not_run() -> None:
    harness, _ = harness_for(ScriptedModel(answers=dict(G_A), mentions=GOOD_MENTIONS))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource((candidate("a"),)), normaliser, no_errors))
    assert result.outcome is not Outcome.REJECTED_AT_GATE
    assert result.screen_not_run == ()


def test_the_screen_and_another_required_step_both_refused_still_names_the_screen() -> None:
    """The turn is rejected with the first rejection's code, as before; the screen not running is recorded all the same, never lost behind another failure."""
    harness, _ = harness_for(refusing("unavailable"), routes=full_routes(drop=("decide.enumerated_slot",)))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource((candidate("a"),)), normaliser, no_errors))
    assert result.outcome is Outcome.REJECTED_AT_GATE
    assert result.screen_not_run == (ScreenStage.UTTERANCE,)


# --- the compiler and the route: the internal class in, the public answer unchanged ----------------------------------------------------------


def test_the_compiler_raises_screen_not_run_for_a_screen_that_did_not_run() -> None:
    compiler = compiler_for(refusing("unavailable"), FakeSource((row("a"),)))
    with pytest.raises(ScreenNotRun) as caught:
        run(compiler.compile(CANONICAL, "fin@1.0.0", None))
    assert caught.value.stages == ("UTTERANCE",)


def test_screen_not_run_is_a_provider_unavailable_to_every_caller() -> None:
    assert issubclass(ScreenNotRun, ProviderUnavailable)


def test_a_provider_outage_on_a_required_step_is_not_screen_not_run() -> None:
    dead = ScriptedProvider("alpha", [ProviderError(ProviderFailure.UNAVAILABLE, 503)])
    harness = Harness(make_config(providers=("alpha",)), {"alpha": dead}, ManualClock(), InMemoryTelemetry(), masking=MASKING)
    compiler = compiler_for(ScriptedModel(answers=dict(G_A), mentions=GOOD_MENTIONS), FakeSource((row("a"),)), harness=harness)
    with pytest.raises(ProviderUnavailable) as caught:
        run(compiler.compile(CANONICAL, "fin@1.0.0", None))
    assert not isinstance(caught.value, ScreenNotRun)


def test_the_public_answer_of_a_screen_that_did_not_run_is_the_answer_of_any_outage() -> None:
    """AGENT-13, A437 (no contract change): the route's status and body for the two are byte for byte the same; nothing of the internal class leaves the service."""

    def answer(exc: Exception) -> tuple[int, Any]:
        r = post(client(Compiler(error=exc)), {"canonical_input": CANONICAL, "pack_ref": PACK_REF})
        return r.status_code, json.loads(r.text)

    code, body = answer(ScreenNotRun(("UTTERANCE",)))
    assert code == 502
    assert body["error"]["code"] == "PROVIDER_UNAVAILABLE"
    assert "SCREEN_NOT_RUN" not in json.dumps(body) and "UTTERANCE" not in json.dumps(body)
    assert (code, body) == answer(ProviderUnavailable())
    assert ROUTE
