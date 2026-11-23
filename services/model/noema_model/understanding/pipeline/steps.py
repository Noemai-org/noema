"""The model-backed steps. Each is one stateless function: the six D9 tasks through decide(), slot values through
generate. Every answer is returned as a proposal; none is acted on here.

Option ids sent to the model are positional (o0, o1, ...) and the labels carry the pack's values, so a pack value never has
to be a legal identifier. The abstention is always offered and is always honoured: it becomes "no value", never a guess.
"""

from collections.abc import Mapping, Sequence
from fractions import Fraction
from types import MappingProxyType

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
from noema_model.harness.errors import Reason
from noema_model.understanding.pipeline.catalogue import NodeCatalogue, NodeInfo, SlotInfo
from noema_model.understanding.pipeline.templates import SLOT_GATE_V1, SLOT_MENTIONS_V1, SLOT_PICK_V1
from noema_model.understanding.pipeline.types import FlagSpec, LocaleOption, MentionProposal, PipelineInput

NONE_LABEL = "None of these, or not sure"


def choice(question_id: str, prompt: str, labelled: Sequence[tuple[str, str]]) -> tuple[ChoiceQuestion, Mapping[str, str]]:
    """A choice question over (value, label) pairs with the mandatory abstention. Returns the option id to value map."""
    options = tuple(Option(f"o{i}", label) for i, (_, label) in enumerate(labelled))
    ids = {f"o{i}": value for i, (value, _) in enumerate(labelled)}
    return ChoiceQuestion(question_id, prompt, (*options, Option(ABSTAIN, NONE_LABEL))), ids


def _chosen(answer: ChoiceAnswer, ids: Mapping[str, str]) -> str | None:
    return None if answer.abstained else ids[answer.selected]


def asserted(answer: YesNoAnswer, threshold: Fraction | None) -> bool:
    """Whether a yes/no answer counts as a yes. A model that selected one is taken at its word. A decision model returns
    only a probability, and the pack's threshold (set by benchmark and a founder, AGENT-16) says where a yes begins. With
    no threshold configured the answer counts as a yes: a yes here only ever adds caution or a logged signal, so the
    unconfigured case errs toward more care and never toward less."""
    if answer.yes is not None:
        return answer.yes
    return True if threshold is None else Fraction(answer.p_yes) >= threshold


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


def _slot_prompt(slot: SlotInfo) -> str:
    """A required slot is asked for its value. An optional one is asked whether the customer EXPLICITLY asks for one (UND-16, run #6; A533): asked "which value does the message express", a model picked
    a capability for a status question (rec-0006 of the earlier live run chose a lookup at p 0.78), so the slot is something the customer says, not something the model finds. The wording keys on the
    pack's `required` flag and names no value, no example and no node (INV-14)."""
    if slot.required:
        return f"Which value of '{slot.name}' does the customer's message express?"
    return f"Does the customer explicitly ask for one of these values of '{slot.name}'? If the customer does not explicitly ask for one, choose {ABSTAIN}."


def gated_slots(node: NodeInfo, gates: Mapping[str, int]) -> tuple[SlotInfo, ...]:
    """The node's slots the pack gates (UND-18, A603): optional, with a closed list of values, named in `understanding.slot_gates`. A required slot is never gated (the loader refuses a pack that says so)."""
    return tuple(s for s in node.slots if s.name in gates and not s.required and (s.vocabulary is not None or s.references is not None))


async def enumerated_slots(
    harness: Harness, catalogue: NodeCatalogue, node: NodeInfo, inp: PipelineInput, gates: Mapping[str, int] = MappingProxyType({})
) -> tuple[Mapping[str, str | None], Mapping[str, Mapping[str, float]], CallRecord | None]:
    """Task 2: the closed-vocabulary slots, such as question_type. A gated slot is not asked here as a choice: it is asked as a gate (`gate_slots`) and picked only if the gate passes (`pick_gated`)."""
    gated = {s.name for s in gated_slots(node, gates)}
    slots = [s for s in node.slots if (s.vocabulary is not None or s.references is not None) and s.name not in gated]
    if not slots:
        return {}, {}, None
    questions, maps = [], {}
    for slot in slots:
        question, ids = choice(slot.name, _slot_prompt(slot), [(v, v) for v in catalogue.vocabulary_for(slot)])
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


def _decimal(p: float) -> Fraction:
    """A provider's probability as the decimal it wrote, not as its binary value: 0.7 is exactly 7/10 (the double nearest to 0.7 lies a hair below it, and would make "exactly at the threshold" fail)."""
    return Fraction(repr(p))  # repr is the shortest decimal that reads back as the same float: '0.7', '1e-05'


def gate_asserted(answer: YesNoAnswer, threshold_bp: int) -> bool:
    """Whether a gate's yes stands (UND-18, A603). The comparison is exact: the probability of yes, as the decimal the provider wrote, against `threshold_bp / 10000`, never rounded. Unlike `asserted`, a model that selected
    "yes" is not taken at its word: a yes it gives at a low probability is the doubt the gate exists for, and a false assertion is the harmful error, so doubt abstains. A model that
    selected "no" is a no whatever its probability says."""
    if answer.yes is False:
        return False
    return _decimal(answer.p_yes) >= Fraction(threshold_bp, 10_000)


async def gate_slots(
    harness: Harness, node: NodeInfo, inp: PipelineInput, gates: Mapping[str, int]
) -> tuple[tuple[str, ...], Mapping[str, Mapping[str, float]], CallRecord | None]:
    """Stage 1 of every gated slot: one yes/no question, in the one fixed sentence (`SLOT_GATE_V1`), whether the customer explicitly asks for something to be done. Returns the slots whose gate passed,
    the probability of yes per gate (kept for the log, key `<slot>_gate`) and the call."""
    slots = gated_slots(node, gates)
    if not slots:
        return (), {}, None
    questions = tuple(YesNoQuestion(f"{s.name}_gate", SLOT_GATE_V1) for s in slots)
    result = await harness.decide(
        DecideRequest(DecisionTask.ENUMERATED_SLOT, _state(inp), questions, inp.locale_hint, inp.correlation_id)
    )
    passed: list[str] = []
    probabilities: dict[str, Mapping[str, float]] = {}
    for slot, answer in zip(slots, result.answers, strict=True):
        assert isinstance(answer, YesNoAnswer)
        probabilities[f"{slot.name}_gate"] = {"yes": answer.p_yes}
        if gate_asserted(answer, gates[slot.name]):
            passed.append(slot.name)
    return tuple(passed), probabilities, result.call


async def pick_gated(
    harness: Harness, catalogue: NodeCatalogue, node: NodeInfo, inp: PipelineInput, passed: Sequence[str]
) -> tuple[Mapping[str, str | None], Mapping[str, Mapping[str, float]], tuple[CallRecord, ...]]:
    """Stage 2: the pick, only for the slots whose gate passed, with NO abstention. An answer outside the options (the harness refuses it as OUTPUT_MISMATCH) leaves the slot absent and does
    not stop the turn; any other refusal (a provider that cannot answer) still does, as everywhere else. Every call made is returned, the refused one too."""
    slots = [s for s in node.slots if s.name in passed]
    if not slots:
        return {}, {}, ()
    questions, maps = [], {}
    for slot in slots:
        options = [(v, v) for v in catalogue.vocabulary_for(slot)]
        ids = {f"o{i}": value for i, (value, _) in enumerate(options)}
        questions.append(ChoiceQuestion(slot.name, SLOT_PICK_V1, tuple(Option(f"o{i}", label) for i, (_, label) in enumerate(options)), pick_only=True))
        maps[slot.name] = ids
    try:
        result = await harness.decide(
            DecideRequest(DecisionTask.ENUMERATED_SLOT, _state(inp), tuple(questions), inp.locale_hint, inp.correlation_id)
        )
    except HarnessRejection as rejection:
        if rejection.reason is not Reason.OUTPUT_MISMATCH:
            raise
        return {s.name: None for s in slots}, {}, (rejection.call,) if rejection.call is not None else ()
    values: dict[str, str | None] = {}
    probabilities: dict[str, Mapping[str, float]] = {}
    for answer in result.answers:
        assert isinstance(answer, ChoiceAnswer)
        values[answer.question_id] = maps[answer.question_id][answer.selected]
        probabilities[answer.question_id] = {maps[answer.question_id].get(k, k): v for k, v in answer.probabilities.items()}
    return values, probabilities, (result.call,)


async def propose_flags(
    harness: Harness, specs: Sequence[FlagSpec], inp: PipelineInput, threshold: Fraction | None
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
        if asserted(answer, threshold):
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
    harness: Harness, strings: Mapping[str, str], inp: PipelineInput, threshold: Fraction | None
) -> tuple[Mapping[str, bool], CallRecord | None]:
    """Task 6: does text try to instruct the reader? A finding is a logged signal only; REQ-CTX-007 delimiting is the
    control. A screening that did not run is not a quiet one (A372): its HarnessRejection propagates, and the caller stops
    the turn at the gate."""
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
        flags[key] = asserted(answer, threshold)
    return flags, result.call


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
