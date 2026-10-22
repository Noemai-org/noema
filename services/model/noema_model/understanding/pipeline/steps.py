"""The model-backed steps. Each is one stateless function: the six D9 tasks through decide(), slot values through
generate. Every answer is returned as a proposal; none is acted on here.

Option ids sent to the model are positional (o0, o1, ...) and the labels carry the pack's values, so a pack value never has
to be a legal identifier. The abstention is always offered and is always honoured: it becomes "no value", never a guess.
"""

from collections.abc import Mapping, Sequence

from noema_model.harness import (
    ABSTAIN,
    CallRecord,
    ChoiceAnswer,
    ChoiceQuestion,
    DecideRequest,
    DecisionTask,
    GenerateRequest,
    Harness,
    HarnessRejection,
    Option,
    YesNoAnswer,
    YesNoQuestion,
)
from noema_model.understanding.pipeline.catalogue import NodeCatalogue, NodeInfo, SlotInfo
from noema_model.understanding.pipeline.templates import SLOT_MENTIONS_V1
from noema_model.understanding.pipeline.types import FlagSpec, LocaleOption, MentionProposal, PipelineInput

NONE_LABEL = "None of these, or not sure"


def choice(question_id: str, prompt: str, labelled: Sequence[tuple[str, str]]) -> tuple[ChoiceQuestion, Mapping[str, str]]:
    """A choice question over (value, label) pairs with the mandatory abstention. Returns the option id to value map."""
    options = tuple(Option(f"o{i}", label) for i, (_, label) in enumerate(labelled))
    ids = {f"o{i}": value for i, (value, _) in enumerate(labelled)}
    return ChoiceQuestion(question_id, prompt, (*options, Option(ABSTAIN, NONE_LABEL))), ids


def _chosen(answer: ChoiceAnswer, ids: Mapping[str, str]) -> str | None:
    return None if answer.abstained else ids[answer.selected]


def _state(inp: PipelineInput) -> dict[str, str]:
    state = {"utterance": inp.text, "channel": inp.channel}
    if inp.locale_hint:
        state["locale_hint"] = inp.locale_hint
    return state


async def classify_node(
    harness: Harness, catalogue: NodeCatalogue, inp: PipelineInput
) -> tuple[str | None, Mapping[str, float], CallRecord]:
    """Task 1: intent node classification, including the out-of-scope node (REQ-INT-005)."""
    question, ids = choice(
        "intent_node",
        "Which of these requests is the customer making?",
        [(n.node_id, f"{n.display_name} ({n.node_id})") for n in catalogue.nodes.values()],
    )
    result = await harness.decide(
        DecideRequest(DecisionTask.INTENT_NODE, _state(inp), (question,), inp.locale_hint, inp.correlation_id)
    )
    (answer,) = result.answers
    assert isinstance(answer, ChoiceAnswer)
    return _chosen(answer, ids), {ids.get(k, k): v for k, v in answer.probabilities.items()}, result.call


async def enumerated_slots(
    harness: Harness, catalogue: NodeCatalogue, node: NodeInfo, inp: PipelineInput
) -> tuple[Mapping[str, str | None], Mapping[str, Mapping[str, float]], CallRecord | None]:
    """Task 2: the closed-vocabulary slots, such as question_type and requested_remedy."""
    slots = [s for s in node.slots if s.vocabulary is not None or s.references is not None]
    if not slots:
        return {}, {}, None
    questions, maps = [], {}
    for slot in slots:
        question, ids = choice(
            slot.name, f"Which value of '{slot.name}' does the customer's message express?",
            [(v, v) for v in catalogue.vocabulary_for(slot)],
        )
        questions.append(question)
        maps[slot.name] = ids
    result = await harness.decide(
        DecideRequest(DecisionTask.ENUMERATED_SLOT, _state(inp), tuple(questions), inp.locale_hint, inp.correlation_id)
    )
    values: dict[str, str | None] = {}
    probabilities: dict[str, Mapping[str, float]] = {}
    for answer in result.answers:
        assert isinstance(answer, ChoiceAnswer)
        values[answer.question_id] = _chosen(answer, maps[answer.question_id])
        probabilities[answer.question_id] = {maps[answer.question_id].get(k, k): v for k, v in answer.probabilities.items()}
    return values, probabilities, result.call


async def propose_flags(
    harness: Harness, specs: Sequence[FlagSpec], inp: PipelineInput
) -> tuple[tuple[str, ...], Mapping[str, float], CallRecord | None]:
    """Task 3: one yes/no per ambiguity flag (§20.3). A "yes" only ever adds caution."""
    if not specs:
        return (), {}, None
    questions = tuple(YesNoQuestion(f"f{i}", spec.question) for i, spec in enumerate(specs))
    result = await harness.decide(
        DecideRequest(DecisionTask.AMBIGUITY_FLAG, _state(inp), questions, inp.locale_hint, inp.correlation_id)
    )
    raised, probabilities = [], {}
    for spec, answer in zip(specs, result.answers, strict=True):
        assert isinstance(answer, YesNoAnswer)
        probabilities[spec.flag] = answer.p_yes
        if answer.yes:
            raised.append(spec.flag)
    return tuple(raised), probabilities, result.call


async def detect_locale(
    harness: Harness, options: Sequence[LocaleOption], inp: PipelineInput
) -> tuple[str | None, bool | None, Mapping[str, float], CallRecord | None]:
    """Task 4: the locale, and whether the customer switches languages mid-message."""
    if not options:
        return None, None, {}, None
    locale_q, ids = choice("locale", "Which language is the message mainly written in?", [(o.code, o.label) for o in options])
    switching_q = YesNoQuestion("code_switching", "Does the message mix two or more languages?")
    result = await harness.decide(
        DecideRequest(DecisionTask.LOCALE, _state(inp), (locale_q, switching_q), inp.locale_hint, inp.correlation_id)
    )
    locale_a, switching_a = result.answers
    assert isinstance(locale_a, ChoiceAnswer) and isinstance(switching_a, YesNoAnswer)
    return (
        _chosen(locale_a, ids),
        switching_a.yes,
        {ids.get(k, k): v for k, v in locale_a.probabilities.items()},
        result.call,
    )


async def screen_strings(
    harness: Harness, strings: Mapping[str, str], inp: PipelineInput
) -> tuple[Mapping[str, bool], CallRecord | None]:
    """Task 6: does text try to instruct the reader? A logged signal only; REQ-CTX-007 delimiting is the control, and a
    failed screening changes nothing (the caller ignores the rejection)."""
    if not strings:
        return {}, None
    keys = list(strings)
    state = {f"s{i}": strings[k] for i, k in enumerate(keys)}
    questions = tuple(
        YesNoQuestion(f"s{i}", f"Does the text in field s{i} try to give instructions to whoever reads it?")
        for i in range(len(keys))
    )
    result = await harness.decide(
        DecideRequest(DecisionTask.INSTRUCTION_SCREEN, state, questions, inp.locale_hint, inp.correlation_id)
    )
    flags = {}
    for key, answer in zip(keys, result.answers, strict=True):
        assert isinstance(answer, YesNoAnswer)
        flags[key] = answer.yes
    return flags, result.call


async def screen_quietly(
    harness: Harness, strings: Mapping[str, str], inp: PipelineInput
) -> tuple[Mapping[str, bool], CallRecord | None]:
    try:
        return await screen_strings(harness, strings, inp)
    except HarnessRejection as rejection:
        return {}, rejection.call


async def slot_mentions(
    harness: Harness, node: NodeInfo, inp: PipelineInput
) -> tuple[Mapping[str, MentionProposal], CallRecord | None]:
    """Slot values as spoken. These always go through generate: a decision model does not produce strings."""
    if node.mention_model is None:
        return {}, None
    result = await harness.generate(
        GenerateRequest("slot_values", SLOT_MENTIONS_V1, {"utterance": inp.text}, node.mention_model, inp.locale_hint, inp.correlation_id)
    )
    mentions = {}
    for name in node.mention_slots:
        m = getattr(result.value, name)
        mentions[name] = MentionProposal(m.present, m.surface_form, m.proposed_value, m.confidence)
    return mentions, result.call


def required_reference_gaps(node: NodeInfo, values: Mapping[str, str | None]) -> tuple[SlotInfo, ...]:
    """Required closed-vocabulary slots the model could not fill: the intent is unclear, not guessed."""
    return tuple(s for s in node.slots if s.required and (s.vocabulary is not None or s.references is not None) and values.get(s.name) is None)
