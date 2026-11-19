"""UND-05, A372 (relayed, not yet committed): a screen that did not run fails the compile closed (REQ-CTX-007, REQ-AUD-001; INV-02, INV-19, AGENT-09, SoT §29). All data is SYNTHETIC.

Until now a refused screening (provider outage, timeout, a reply that broke the schema, no route) was swallowed: the turn went on with no signal at all, and
"no signal" looked the same as "the text was screened and is clean". The safer reading taken here: the screen is a required step of the turn. Its refusal is a
typed rejection at the gate, like any other required model step, nothing is read from the Evidence API after it, and no proposal comes back.
"""

from dataclasses import dataclass
from typing import Any

import pytest

from noema_model.providers.llm.base import FinishReason, ProviderError, ProviderFailure, ProviderRequest, ProviderResponse
from noema_model.understanding.pipeline import Outcome, run_pipeline
from noema_model.understanding.resolution import ResolutionState
from tests.support import run
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

CONFIG = pipeline_config()
SCREEN_ROUTE = "decide.instruction_likeness"
SCREEN_QUESTION = "try to give instructions to whoever reads it"
HOSTILE_CANDIDATE = "Zebulon Quux"  # appears in a candidate's display name only, never in the utterance


@dataclass
class ScreenRefusingModel(ScriptedModel):
    """Answers everything as ScriptedModel does, except the screening questions, which it refuses in `mode`. With `only_containing`
    set it refuses only a screening whose data carries that text, so the utterance screen and the candidate screen can be told apart."""

    mode: str = "unavailable"
    only_containing: str | None = None

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        is_screen = request.schema_name == "harness_decide" and SCREEN_QUESTION in request.user
        if is_screen and (self.only_containing is None or self.only_containing in request.user):
            self.requests.append(request)
            if self.mode == "unavailable":
                raise ProviderError(ProviderFailure.UNAVAILABLE, 503)
            if self.mode == "timeout":
                raise ProviderError(ProviderFailure.TIMEOUT)
            if self.mode == "not_json":
                return ProviderResponse("this is not json", self.version, 50, 10, FinishReason.COMPLETE)
            if self.mode == "no_answers":
                return ProviderResponse('{"answers": []}', self.version, 50, 10, FinishReason.COMPLETE)
            raise AssertionError(self.mode)
        return await super().complete(request)


def refusing(mode: str, only_containing: str | None = None) -> ScreenRefusingModel:
    return ScreenRefusingModel(answers=dict(G_A), mentions=GOOD_MENTIONS, mode=mode, only_containing=only_containing)


def screen_calls(result: Any) -> list[Any]:
    return [c for c in result.calls if c.route == SCREEN_ROUTE]


@pytest.mark.parametrize("mode", ["unavailable", "timeout", "not_json", "no_answers"])
def test_a_refused_utterance_screen_rejects_the_turn_and_nothing_is_read(mode: str) -> None:
    """REQ-CTX-007, INV-02: the utterance screen did not run, so there is no proposal, no candidate read and no signal to mistake for 'clean'."""
    harness, _ = harness_for(refusing(mode))
    source = FakeSource((candidate("a"),))
    result = run(run_pipeline(make_input(), CONFIG, harness, source, normaliser, no_errors))
    assert result.outcome is Outcome.REJECTED_AT_GATE and len(result.gate_errors) == 1
    assert result.proposal is None and result.resolution is None and source.queries == []
    assert result.injection_signals == {}


def test_a_refused_screen_is_a_recorded_call_not_a_silence() -> None:
    """REQ-AUD-001: the screen's call is in the call list of the rejected turn, with its failed attempt, so the Record can show the screen did not run."""
    harness, _ = harness_for(refusing("unavailable"))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource((candidate("a"),)), normaliser, no_errors))
    (screen,) = screen_calls(result)
    assert screen.attempts and all(a.outcome.name != "OK" for a in screen.attempts)


def test_the_gate_error_of_a_refused_screen_is_the_harness_code() -> None:
    harness, _ = harness_for(refusing("unavailable"))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource((candidate("a"),)), normaliser, no_errors))
    assert result.gate_errors == ("PROVIDER_UNAVAILABLE",)
    harness, _ = harness_for(refusing("not_json"))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource((candidate("a"),)), normaliser, no_errors))
    assert result.gate_errors == ("SCHEMA_INVALID",)


def test_a_screen_route_with_no_provider_rejects_the_turn() -> None:
    harness, _ = harness_for(ScriptedModel(answers=dict(G_A), mentions=GOOD_MENTIONS), routes=full_routes(drop=(SCREEN_ROUTE,)))
    source = FakeSource((candidate("a"),))
    result = run(run_pipeline(make_input(), CONFIG, harness, source, normaliser, no_errors))
    assert result.outcome is Outcome.REJECTED_AT_GATE and result.proposal is None and source.queries == []


def test_a_refused_candidate_screen_rejects_the_turn_and_no_resolution_comes_back() -> None:
    """The candidate display names are enterprise text: if their screening did not run the turn stops, it is not resolved on unscreened text."""
    harness, _ = harness_for(refusing("unavailable", only_containing=HOSTILE_CANDIDATE))
    source = FakeSource((candidate("a", HOSTILE_CANDIDATE),))
    result = run(run_pipeline(make_input(), CONFIG, harness, source, normaliser, no_errors))
    assert result.outcome is Outcome.REJECTED_AT_GATE and result.gate_errors == ("PROVIDER_UNAVAILABLE",)
    assert result.resolution is None
    assert len(screen_calls(result)) == 2  # the utterance screen that ran, and the candidate screen that was refused


@pytest.mark.parametrize("mode", ["timeout", "not_json", "no_answers"])
def test_every_refusal_of_the_candidate_screen_stops_the_turn(mode: str) -> None:
    harness, _ = harness_for(refusing(mode, only_containing=HOSTILE_CANDIDATE))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource((candidate("a", HOSTILE_CANDIDATE),)), normaliser, no_errors))
    assert result.outcome is Outcome.REJECTED_AT_GATE and result.resolution is None and len(result.gate_errors) == 1


def test_no_candidates_means_nothing_to_screen_and_the_turn_still_proposes() -> None:
    """Only a screen that was needed and refused stops the turn: an empty candidate set has no text to screen."""
    harness, _ = harness_for(refusing("unavailable", only_containing=HOSTILE_CANDIDATE))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource(()), normaliser, no_errors))
    assert result.outcome is Outcome.PROPOSED
    assert result.resolution is not None and result.resolution.state is ResolutionState.NO_CANDIDATES


def test_a_screen_that_ran_and_found_nothing_still_proposes() -> None:
    """The control case: the same turn with a working screen is proposed and auto-resolved, so the stop above is about the refusal and not the screen."""
    harness, _ = harness_for(ScriptedModel(answers=dict(G_A), mentions=GOOD_MENTIONS))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource((candidate("a"),)), normaliser, no_errors))
    assert result.outcome is Outcome.PROPOSED and result.injection_signals == {"utterance": False, "candidate:a": False}
    assert result.resolution is not None and result.resolution.state is ResolutionState.AUTO_RESOLVED
