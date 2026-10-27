"""The live decider of the model selection: harness.decide() behind the bake-off's Decider door, with the budget guard in front.

The harness is the real one, with the real OpenAI Responses adapter; only the wire is a recorded transport (fixtures written from
the reference, SYNTHETIC; not run against a live endpoint: no access). Decisions D5 to D9 of benchmark/decisions/2026-10-27-model-selection.md."""

import json
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

import noema_model.harness as harness_module  # noqa: E402
from noema_model.harness import Harness, build_providers, load_harness_config  # noqa: E402
from noema_model.harness import AttemptOutcome, HarnessRejection, Reason  # noqa: E402,F401
from noema_model.harness.decide_types import ABSTAIN  # noqa: E402,F401
from noema_model.platform.telemetry import NullTelemetry  # noqa: E402
from noema_model.providers.llm.base import HttpResponse  # noqa: E402
from tests.support import ManualClock, raw_config  # noqa: E402

from benchmark.bakeoff.budget import BudgetGuard, projected_micros  # type: ignore[import-not-found,unused-ignore]  # noqa: E402
from benchmark.bakeoff.live import (  # type: ignore[import-not-found,unused-ignore]  # noqa: E402
    OVERHEAD_BYTES, BudgetStopped, HarnessDecider, PinDrift,
)
from benchmark.bakeoff.run import DeciderError, DeciderRejected, DeciderUnavailable  # type: ignore[import-not-found,unused-ignore]  # noqa: E402

MODEL = "gpt-6-luna"
DATED = "gpt-6-luna-2026-10-20"
MAX_OUT = 4000
RATES = (3_000_000, 15_000_000)  # the test configuration's rate card (raw_config), micro-dollars per million tokens


@dataclass
class SequenceTransport:
    """Plays the recorded responses in order and keeps every request body."""

    responses: list[HttpResponse]
    sent: list[dict[str, Any]] = field(default_factory=list)

    async def post(self, url: str, headers: Any, body: bytes, timeout_s: float, max_bytes: int) -> HttpResponse:
        self.sent.append(json.loads(body))
        return self.responses.pop(0)


def reply(answers: list[dict[str, Any]] | str, model: str = MODEL, tokens: tuple[int, int] = (100, 20)) -> HttpResponse:
    text = answers if isinstance(answers, str) else json.dumps({"answers": answers})
    body = {"status": "completed", "model": model,
            "output": [{"type": "message", "content": [{"type": "output_text", "text": text}]}],
            "usage": {"input_tokens": tokens[0], "output_tokens": tokens[1]}}
    return HttpResponse(200, json.dumps(body).encode())


def yes_no(question_id: str, selected: str, p_yes: float) -> dict[str, Any]:
    return {"question_id": question_id, "selected": selected, "probabilities": [{"option": "yes", "p": p_yes}, {"option": "no", "p": 1 - p_yes}]}


def choice(question_id: str, selected: str, probabilities: dict[str, float]) -> dict[str, Any]:
    return {"question_id": question_id, "selected": selected, "probabilities": [{"option": k, "p": v} for k, v in probabilities.items()]}


PROBE_OK = reply([yes_no("probe", "yes", 0.9)])


def make(transport: SequenceTransport, *, limit: int = 10_000_000, spent: int = 0, max_out: int = MAX_OUT) -> tuple[HarnessDecider, BudgetGuard]:
    guard = BudgetGuard(limit_micros=limit, spent_micros=spent)

    def build(reported: str | None) -> Harness:
        raw = raw_config(providers=("alpha",))
        raw["providers"]["alpha"].update(kind="openai_responses", reasoning_effort="medium", model_version=MODEL, max_output_tokens=max_out)
        if reported is not None:
            raw["providers"]["alpha"]["reported_model_version"] = reported
        config = load_harness_config(raw)
        return Harness(config, build_providers(config, transport, {}), ManualClock(), NullTelemetry())

    decider = HarnessDecider(provider="LIVE", modules=harness_module, build=build, guard=guard, input_micro_per_mtok=RATES[0],
                             output_micro_per_mtok=RATES[1], max_output_tokens=max_out, model_name=MODEL)
    return decider, guard


FLAG = {"id": "amount_missing", "type": "yes_no", "prompt": "Does the text state no amount at all?", "options": ["yes", "no"]}
INTENT = {"id": "intent_node", "type": "choice", "prompt": "Which intent?", "options": ["fin.payments.exception.investigate", "fin.meta.out_of_scope", "none"]}
BINDING = {"id": "answer_binding", "type": "choice", "prompt": "Which option?", "options": ["opt_1", "opt_2", "none"]}
CONTEXT = {"options": [{"id": "opt_1", "label": "10:00, NGN 5,000"}, {"id": "opt_2", "label": "11:15, NGN 5,000"}]}


def ready(replies: list[HttpResponse], **kw: Any) -> tuple[HarnessDecider, BudgetGuard, SequenceTransport]:
    transport = SequenceTransport([PROBE_OK, *replies])
    decider, guard = make(transport, **kw)
    decider.ready()
    return decider, guard, transport


def test_a_yes_no_answer_carries_its_probability_and_the_recorded_cost() -> None:
    decider, _, _ = ready([reply([yes_no("amount_missing", "yes", 0.8)]), reply([yes_no("amount_missing", "no", 0.2)])])
    first = decider.decide(FLAG, "I sent money", None)
    assert (first.selected, first.confidence) == ("yes", 0.8)
    assert first.cost_usd_micros == 100 * 3 + 20 * 15  # tokens x the rate card, rounded up: 600 micro-dollars
    second = decider.decide(FLAG, "I sent 2k", None)
    assert (second.selected, round(second.confidence, 6)) == ("no", 0.8)  # the probability of the selected answer


def test_a_choice_goes_out_with_neutral_ids_and_comes_back_as_the_cases_option() -> None:
    decider, _, transport = ready([reply([choice("intent_node", "o2", {"o1": 0.1, "o2": 0.85, ABSTAIN: 0.05})]),
                                   reply([choice("intent_node", ABSTAIN, {"o1": 0.1, "o2": 0.1, ABSTAIN: 0.8})])])
    answer = decider.decide(INTENT, "Book a flight", None)
    assert (answer.selected, answer.confidence) == ("fin.meta.out_of_scope", 0.85)
    sent = transport.sent[1]["input"]  # the user text, with the questions block inside
    assert '"option_id":"o1"' in sent and ABSTAIN in sent and "fin.payments.exception.investigate" in sent and "o3" not in sent
    assert decider.decide(INTENT, "hmm", None).selected == "none"


def test_a_choice_answer_with_no_probability_for_its_selection_counts_at_zero_confidence() -> None:
    decider, _, _ = ready([reply([choice("intent_node", "o1", {})])])
    answer = decider.decide(INTENT, "x", None)
    assert (answer.selected, answer.confidence) == ("fin.payments.exception.investigate", 0.0)


def test_answer_binding_shows_the_models_the_offered_options_labels() -> None:
    decider, _, transport = ready([reply([choice("answer_binding", "o1", {"o1": 0.9, "o2": 0.05, ABSTAIN: 0.05})])])
    assert decider.decide(BINDING, "the 10 o'clock one", CONTEXT).selected == "opt_1"
    assert "10:00, NGN 5,000" in json.dumps(transport.sent[1])


def test_the_prior_turn_travels_as_data() -> None:
    decider, _, transport = ready([reply([yes_no("amount_missing", "no", 0.7)])])
    decider.decide(FLAG, "ok", {"prior_turn": "send 5k to Bola"})
    assert "send 5k to Bola" in json.dumps(transport.sent[1])


def test_an_unknown_question_is_a_bug_and_not_an_answer() -> None:
    decider, _, _ = ready([])
    with pytest.raises(KeyError):
        decider.decide({"id": "not_a_question", "type": "yes_no", "prompt": "x", "options": ["yes", "no"]}, "x", None)


def test_a_decider_that_was_not_made_ready_cannot_decide() -> None:
    decider, _ = make(SequenceTransport([]))
    with pytest.raises(DeciderUnavailable):
        decider.decide(FLAG, "x", None)


# --- the identifier the API returns (D5, D9) -----------------------------------------------------------------------------

def test_the_probe_learns_a_dated_identifier_and_the_run_is_pinned_to_it() -> None:
    transport = SequenceTransport([reply([yes_no("probe", "yes", 0.9)], model=DATED), reply([yes_no("amount_missing", "no", 0.9)], model=DATED)])
    decider, _ = make(transport)
    decider.ready()
    assert decider.reported_model_id == DATED and decider.model_info() == {"provider": "openai", "name": MODEL, "version": DATED}
    assert decider.decide(FLAG, "x", None).selected == "no"
    assert transport.sent[0]["model"] == MODEL and transport.sent[1]["model"] == MODEL  # the ID sent never changes


def test_when_the_api_reports_the_id_sent_that_is_the_identifier() -> None:
    decider, _, _ = ready([])
    assert decider.reported_model_id == MODEL


def test_a_different_identifier_after_the_probe_is_a_drift_and_nothing_more_is_sent() -> None:
    transport = SequenceTransport([reply([yes_no("probe", "yes", 0.9)], model=DATED), reply([yes_no("amount_missing", "no", 0.9)], model="gpt-6-luna-2026-11-02"),
                                   reply([yes_no("amount_missing", "no", 0.9)], model=DATED)])
    decider, guard = make(transport)
    decider.ready()
    with pytest.raises(PinDrift):
        decider.decide(FLAG, "x", None)
    sent, spent = len(transport.sent), guard.spent_micros
    with pytest.raises(PinDrift):
        decider.decide(FLAG, "y", None)
    assert len(transport.sent) == sent and guard.spent_micros == spent and decider.drifted
    assert spent > 0  # the drifted call was billed and counted


def test_a_probe_that_fails_leaves_the_path_not_run() -> None:
    decider, _ = make(SequenceTransport([HttpResponse(500, b"")]))
    with pytest.raises(DeciderUnavailable):
        decider.ready()


# --- the budget (D7) ---------------------------------------------------------------------------------------------------

def test_the_probe_and_every_call_count_against_the_budget_at_their_actual_cost() -> None:
    decider, guard, _ = ready([reply([yes_no("amount_missing", "no", 0.9)])])
    after_probe = guard.spent_micros
    assert after_probe == 600
    decider.decide(FLAG, "x", None)
    assert guard.spent_micros == after_probe + 600


def test_a_request_that_would_pass_the_remaining_budget_is_not_sent_and_the_stop_is_sticky() -> None:
    projected = projected_micros(OVERHEAD_BYTES + 200, MAX_OUT, *RATES)
    decider, guard, transport = ready([reply([yes_no("amount_missing", "no", 0.9)])], limit=600 + projected + 5, spent=0)
    with pytest.raises(BudgetStopped):
        decider.decide(FLAG, "x" * 2000, None)  # larger than the 5 micro-dollars of headroom
    sent = len(transport.sent)
    with pytest.raises(BudgetStopped):
        decider.decide(FLAG, "x", None)
    assert len(transport.sent) == sent and guard.stopped


def test_prior_spending_counts_and_can_stop_the_run_before_the_probe() -> None:
    decider, _ = make(SequenceTransport([PROBE_OK]), limit=100, spent=100)
    with pytest.raises(DeciderUnavailable):
        decider.ready()


def test_a_call_whose_cost_exceeds_its_projection_is_counted_not_hidden() -> None:
    decider, guard, _ = ready([reply([yes_no("amount_missing", "no", 0.9)], tokens=(100, 900_000))], limit=100_000_000, max_out=1)
    decider.decide(FLAG, "x", None)
    assert decider.diagnostics.projection_exceeded == 1
    assert guard.spent_micros >= 900_000 * 15


# --- outcomes ------------------------------------------------------------------------------------------------------------

def test_a_reply_that_fails_the_schema_is_a_rejection_and_is_billed() -> None:
    decider, guard, _ = ready([reply("not json at all")])
    before = guard.spent_micros
    with pytest.raises(DeciderRejected):
        decider.decide(FLAG, "x", None)
    assert guard.spent_micros == before + 600


def test_a_reply_for_the_wrong_question_is_a_rejection() -> None:
    decider, _, _ = ready([reply([yes_no("someone_else", "yes", 0.9)])])
    with pytest.raises(DeciderRejected):
        decider.decide(FLAG, "x", None)


def test_a_question_the_harness_refuses_before_any_request_costs_nothing_and_is_not_a_rejection_by_a_model() -> None:
    decider, guard, transport = ready([])
    before, sent = guard.spent_micros, len(transport.sent)
    with pytest.raises(HarnessRejection) as caught:
        decider.decide({**FLAG, "prompt": ""}, "x", None)
    assert caught.value.call is None and caught.value.reason is Reason.QUESTION_INVALID
    assert guard.spent_micros == before and len(transport.sent) == sent


def test_a_reply_with_no_usage_report_is_counted_at_its_projection_not_at_zero() -> None:
    no_usage = HttpResponse(200, json.dumps({"status": "completed", "model": MODEL, "output": [{"type": "message", "content": [
        {"type": "output_text", "text": json.dumps({"answers": [yes_no("amount_missing", "no", 0.1)]})}]}]}).encode())
    decider, guard, _ = ready([no_usage])
    before = guard.spent_micros
    answer = decider.decide(FLAG, "x", None)
    projected = projected_micros(len(json.dumps([FLAG, {"utterance": "x"}], ensure_ascii=False).encode()) + OVERHEAD_BYTES, MAX_OUT, *RATES)
    assert guard.spent_micros - before == projected and answer.cost_usd_micros == projected


def test_a_provider_failure_is_an_error_not_a_rejection_and_is_counted_at_its_projection() -> None:
    decider, guard, _ = ready([HttpResponse(500, b"")])
    before = guard.spent_micros
    with pytest.raises(DeciderError) as caught:
        decider.decide(FLAG, "x", None)
    assert not isinstance(caught.value, (DeciderRejected, PinDrift, BudgetStopped))
    assert guard.spent_micros - before == projected_micros(len(json.dumps([FLAG, {"utterance": "x"}], ensure_ascii=False).encode()) + OVERHEAD_BYTES,
                                                           MAX_OUT, *RATES)


def test_the_diagnostics_keep_the_model_only_latency_tokens_and_cost_per_call() -> None:
    decider, _, _ = ready([reply([yes_no("amount_missing", "no", 0.9)])])
    decider.decide(FLAG, "x", None)
    (probe, call) = decider.diagnostics.calls
    assert call["tokens_in"] == 100 and call["tokens_out"] == 20 and call["cost_micro"] == 600 and "model_latency_ms" in call
    assert probe["cost_micro"] == 600


def test_nothing_in_a_decider_carries_the_key() -> None:
    decider, _, transport = ready([reply([yes_no("amount_missing", "no", 0.9)])])
    decider.decide(FLAG, "x", None)
    assert "authorization" not in json.dumps(transport.sent).lower() and "sk-" not in json.dumps(decider.diagnostics.calls)


def test_a_partial_cost_is_never_taken_for_the_cost() -> None:
    """With failover one attempt can be priced and another not: cost_micro is then a partial sum and cost_complete is false."""
    from types import SimpleNamespace

    decider, guard, _ = ready([])
    before = guard.spent_micros
    decider._settle(SimpleNamespace(cost_micro=300, cost_complete=False, latency_ms=1, tokens_in=1, tokens_out=1), 5_000)  # noqa: SLF001
    assert guard.spent_micros - before == 5_000


# --- the frozen harness configurations, through the real loader --------------------------------------------------------

DECISIONS = Path(__file__).resolve().parents[3] / "benchmark" / "decisions"


@pytest.mark.parametrize("file,model,cost_for_100_in_20_out", [("harness-gpt-6-luna.json", "gpt-6-luna", 20), ("harness-gpt-6.1-terra.json", "gpt-6.1-terra", 200)])
def test_each_frozen_configuration_loads_and_drives_a_decider_that_sends_the_frozen_request(file: str, model: str, cost_for_100_in_20_out: int) -> None:
    from benchmark.bakeoff.live import build_real_decider  # type: ignore[import-not-found,unused-ignore]

    probe = reply([yes_no("probe", "yes", 0.9)], model=model)
    answer = reply([yes_no("amount_missing", "no", 0.1)], model=model)
    transport = SequenceTransport([probe, answer])
    guard = BudgetGuard(limit_micros=2_000_000)
    decider = build_real_decider("T", str(DECISIONS / file), {"OPENAI_API_KEY": "sk-synthetic-not-a-real-key"}, guard, transport)
    decider.ready()
    result = decider.decide(FLAG, "I sent 2k", None)
    sent = transport.sent[1]
    assert (sent["model"], sent["reasoning"], sent["max_output_tokens"], sent["store"]) == (model, {"effort": "medium"}, 4000, False)
    assert result.cost_usd_micros == cost_for_100_in_20_out  # the frozen rate card x 100 tokens in, 20 out
    assert decider.model_info()["version"] == model
    assert "sk-synthetic" not in json.dumps(transport.sent) and "sk-synthetic" not in repr(decider.diagnostics)


def test_a_frozen_configuration_without_the_key_in_the_environment_does_not_start() -> None:
    from benchmark.bakeoff.live import build_real_decider  # type: ignore[import-not-found,unused-ignore]
    from noema_model.platform.config import ConfigError

    with pytest.raises(ConfigError) as caught:
        build_real_decider("T", str(DECISIONS / "harness-gpt-6-luna.json"), {}, BudgetGuard(limit_micros=1)).ready()
    assert "OPENAI_API_KEY" in str(caught.value)
