"""The live decider of the model selection (benchmark/decisions/2026-10-27-model-selection.md): `harness.decide()` behind the
bake-off's `Decider` door, with the budget guard in front of every request.

The model service is reached only through its harness (`noema_model.harness`), which is imported when a decider is built, not
when this module is: the benchmark's own tests do not need the service's dependencies. The key is never seen here: the harness
configuration names the environment variable and `build_providers` reads it. Nothing in this file prints, logs or stores it.

A budget stop and a model-identifier drift are both sticky: after one, no request is sent, and each remaining call is recorded as
an error. The run file then says so (`budget_stopped`, `pin_drift`) and gate G7 fails, so a short run never has a verdict.
"""
import asyncio
import json
from dataclasses import dataclass, field
from typing import Any, Callable

from benchmark.bakeoff.budget import BudgetExceeded, BudgetGuard, projected_micros
from benchmark.bakeoff.run import Answer, DeciderError, DeciderRejected, DeciderUnavailable

Json = dict[str, Any]

# Which of the six decision tasks a bake-off question belongs to (the harness needs the task; the Decider door passes the question).
TASK_OF_QUESTION = {
    "intent_node": "INTENT_NODE", "question_type": "ENUMERATED_SLOT", "requested_remedy": "ENUMERATED_SLOT", "locale": "LOCALE",
    "answer_binding": "ANSWER_BINDING", "instruction_like": "INSTRUCTION_SCREEN",
    "amount_missing": "AMBIGUITY_FLAG", "time_vague": "AMBIGUITY_FLAG", "intent_unclear": "AMBIGUITY_FLAG",
    "multiple_intents": "AMBIGUITY_FLAG", "contradicts_prior_turn": "AMBIGUITY_FLAG", "out_of_scope_request": "AMBIGUITY_FLAG",
}
ABSTENTIONS = frozenset({"none", "other"})
OVERHEAD_BYTES = 12_000  # system text, instruction, output schema and wrappers; a generous bound, checked against every real cost
PROBE_QUESTION = {"id": "probe", "type": "yes_no", "prompt": "Is this a test message?", "options": ["yes", "no"]}


class BudgetStopped(DeciderError):
    """The budget guard refused the request; nothing was sent."""


class PinDrift(DeciderError):
    """The API reported another model identifier than the one pinned for this run; nothing more is sent."""


@dataclass
class Diagnostics:
    """Per call, in call order; model-only latency is here and nowhere in a gate."""

    calls: list[Json] = field(default_factory=list)
    projection_exceeded: int = 0


def to_harness_question(modules: Any, question: Json, context: Json | None) -> tuple[Any, dict[str, str]]:
    """The harness question for a bake-off question, and the map from the harness's option ids back to the case's options.
    Option ids are neutral (`o1`..) because case options carry dots and digits the harness ids may not; the labels are the case's."""
    if question["type"] == "yes_no":
        return modules.YesNoQuestion(question["id"], question["prompt"]), {}
    labels = {o["id"]: o["label"] for o in ((context or {}).get("options") or [])}
    options, back = [], {}
    for index, name in enumerate(question["options"], 1):
        if name in ABSTENTIONS:
            option_id, label = modules.ABSTAIN, "none or other"
        else:
            option_id, label = f"o{index}", labels.get(name, name)
        options.append(modules.Option(option_id, label))
        back[option_id] = name
    return modules.ChoiceQuestion(question["id"], question["prompt"], tuple(options)), back


class HarnessDecider:
    """`build(reported)` returns a Harness whose provider expects `reported` as the API's model identifier."""

    def __init__(self, *, provider: str, modules: Any, build: Callable[[str | None], Any], guard: BudgetGuard,
                 input_micro_per_mtok: int, output_micro_per_mtok: int, max_output_tokens: int, model_name: str) -> None:
        self.provider = provider
        self._m = modules
        self._build = build
        self._guard = guard
        self._rates = (input_micro_per_mtok, output_micro_per_mtok)
        self._max_out = max_output_tokens
        self._model = model_name
        self._harness: Any = None
        self._loop = asyncio.new_event_loop()
        self._index = 0
        self.reported_model_id: str | None = None
        self.diagnostics = Diagnostics()
        self.drifted = False

    # -- the Decider door -------------------------------------------------------------------------------------------------

    def ready(self) -> None:
        if self._harness is not None:
            return
        reported = self._probe()
        self.reported_model_id = reported
        self._harness = self._build(reported)

    def model_info(self) -> Json:
        return {"provider": "openai", "name": self._model, "version": self.reported_model_id}

    def decide(self, question: Json, text: str, context: Json | None) -> Answer:
        if self._harness is None:
            raise DeciderUnavailable("the decider was not made ready")
        if self.drifted:
            raise PinDrift("the model identifier drifted; nothing more is sent")
        return self._ask(self._harness, question, text, context)

    # -- one request ------------------------------------------------------------------------------------------------------

    def _ask(self, harness: Any, question: Json, text: str, context: Json | None) -> Answer:
        m = self._m
        asked, back = to_harness_question(m, question, context)
        state = {"utterance": text, **({"prior_turn": str(context["prior_turn"])} if context and "prior_turn" in context else {})}
        task = m.DecisionTask[TASK_OF_QUESTION[question["id"]]]
        size = len(json.dumps([question, state], ensure_ascii=False).encode()) + OVERHEAD_BYTES
        projected = projected_micros(size, self._max_out, *self._rates)
        try:
            self._guard.authorize(projected)
        except BudgetExceeded as exc:
            raise BudgetStopped(str(exc)) from exc
        self._index += 1
        request = m.DecideRequest(task, state, (asked,), None, f"corr_bakeoff_{self._index:05d}")
        try:
            result = self._loop.run_until_complete(harness.decide(request))
        except m.HarnessRejection as rejection:
            self._settle(rejection.call, projected)
            if rejection.call is None:
                raise
            if any(a.outcome is m.AttemptOutcome.VERSION_MISMATCH for a in rejection.call.attempts):
                self.drifted = True
                raise PinDrift("the API reported another model identifier than the one pinned") from rejection
            if rejection.reason is m.Reason.NO_PROVIDER_ANSWERED:
                raise DeciderError(rejection.reason.value) from rejection
            raise DeciderRejected(rejection.reason.value) from rejection
        self._settle(result.call, projected)
        answer = result.answers[0]
        cost = result.call.cost_micro if result.call.cost_complete else projected
        if isinstance(answer, m.YesNoAnswer):
            selected = "yes" if answer.yes else "no"
            confidence = answer.p_yes if answer.yes else 1.0 - answer.p_yes
        else:
            selected = back[answer.selected]
            confidence = answer.probabilities.get(answer.selected, 0.0)
        return Answer(selected, confidence, int(cost or 0))

    def _settle(self, call: Any, projected: int) -> None:
        if call is None:
            actual: int | None = 0  # rejected before any request: nothing was sent, nothing was billed
        else:
            actual = call.cost_micro if call.cost_complete else None  # unknown cost: settled at the projection
        self._guard.settle(actual, projected)
        if call is not None:
            self.diagnostics.calls.append({"model_latency_ms": call.latency_ms, "tokens_in": call.tokens_in, "tokens_out": call.tokens_out,
                                           "cost_micro": call.cost_micro, "projected_micro": projected})
            if call.cost_micro is not None and call.cost_micro > projected:
                self.diagnostics.projection_exceeded += 1

    # -- the model identifier -------------------------------------------------------------------------------------------

    def _probe(self) -> str:
        """One tiny request, counted in the budget, to learn the identifier the API returns (decision D5). It is not a corpus call."""
        m = self._m
        harness = self._build(None)
        try:
            return self._identifier(self._ask_probe(harness))
        except m.HarnessRejection as rejection:
            call = rejection.call
            mismatch = call is not None and call.attempts and call.attempts[-1].outcome is m.AttemptOutcome.VERSION_MISMATCH
            if mismatch and call.attempts[-1].model_version_reported:
                return str(call.attempts[-1].model_version_reported)
            raise DeciderUnavailable(f"the probe failed: {rejection.reason.value}") from rejection
        except (DeciderError, BudgetExceeded) as exc:
            raise DeciderUnavailable(f"the probe was not made: {type(exc).__name__}") from exc

    def _ask_probe(self, harness: Any) -> Any:
        m = self._m
        asked, _ = to_harness_question(m, PROBE_QUESTION, None)
        projected = projected_micros(OVERHEAD_BYTES + 200, self._max_out, *self._rates)
        self._guard.authorize(projected)
        self._index += 1
        request = m.DecideRequest(m.DecisionTask.AMBIGUITY_FLAG, {"utterance": "test message"}, (asked,), None, f"corr_bakeoff_probe_{self._index:05d}")
        try:
            result = self._loop.run_until_complete(harness.decide(request))
        except m.HarnessRejection as rejection:
            self._settle(rejection.call, projected)
            raise
        self._settle(result.call, projected)
        return result

    @staticmethod
    def _identifier(result: Any) -> str:
        """On success the harness has already checked the reply against the ID sent, so the identifier is that ID."""
        return str(result.call.attempts[-1].model_version_reported)


def build_real_decider(label: str, config_path: str, environ: dict[str, str], guard: BudgetGuard, transport: Any = None) -> HarnessDecider:
    """A decider on the real harness for one harness configuration file (the one NOEMA_MODEL_HARNESS_CONFIG would name). The model
    ID, the rate card, `reasoning.effort`, `max_output_tokens` and the key's variable name are the file's; none is set here.
    `transport` is for tests; a live run uses the service's own HTTPS transport."""
    import copy
    from importlib import import_module
    from pathlib import Path
    from types import SimpleNamespace

    harness = import_module("noema_model.harness")
    templates = import_module("noema_model.harness.templates")
    decide_types = import_module("noema_model.harness.decide_types")
    errors = import_module("noema_model.harness.errors")
    clock = import_module("noema_model.platform.clock")
    telemetry = import_module("noema_model.platform.telemetry")
    wire = import_module("noema_model.providers.llm.transport")
    modules = SimpleNamespace(
        DecideRequest=harness.DecideRequest, DecisionTask=harness.DecisionTask, HarnessRejection=harness.HarnessRejection,
        AttemptOutcome=harness.AttemptOutcome, Reason=errors.Reason, ChoiceQuestion=harness.ChoiceQuestion, YesNoQuestion=harness.YesNoQuestion,
        YesNoAnswer=harness.YesNoAnswer, Option=harness.Option, ABSTAIN=decide_types.ABSTAIN, template_digest=templates.DECIDE_V2.digest,
        template_version=templates.DECIDE_V2.version,
    )
    raw = json.loads(Path(config_path).read_text(encoding="utf-8"))
    config = harness.load_harness_config(raw)
    (name, provider), = config.providers.items()
    if provider.rate is None or provider.max_output_tokens is None:
        raise DeciderUnavailable("the harness configuration needs a rate card and max_output_tokens for a budgeted run")
    use = transport if transport is not None else wire.UrllibTransport()

    def build(reported: str | None) -> Any:
        body = copy.deepcopy(raw)
        if reported is not None:
            body["providers"][name]["reported_model_version"] = reported
        loaded = harness.load_harness_config(body)
        return harness.Harness(loaded, harness.build_providers(loaded, use, environ), clock.SystemClock(), telemetry.NullTelemetry())

    decider = HarnessDecider(provider=label, modules=modules, build=build, guard=guard, input_micro_per_mtok=provider.rate.input_micro_per_mtok,
                             output_micro_per_mtok=provider.rate.output_micro_per_mtok, max_output_tokens=provider.max_output_tokens,
                             model_name=provider.model_version)
    return decider
