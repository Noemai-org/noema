"""The decide(state, questions) interface (D9): question types, the six eligible tasks and the answers.

A decision model, or an LLM standing in for one, returns information: a selected answer and the probabilities behind it.
The harness validates the shape and no more. It never turns a probability into a decision; whoever consumes an answer
treats it as a proposal that deterministic code checks (SoT §13.3: confidence is information, never authority).
"""

import re
from collections.abc import Mapping
from dataclasses import dataclass
from enum import StrEnum

from noema_model.harness.errors import HarnessRejection, Reason

# The abstention every choice question must offer ("none / other", PERD §19.1). A fixed id, so the harness can check
# it rather than trust a label.
ABSTAIN = "none_or_other"

_ID = re.compile(r"^[a-z][a-z0-9_]{0,63}$")


class DecisionTask(StrEnum):
    """The six understanding tasks a decision model is eligible for (PERD §19.1, D9). No other task may use decide."""

    INTENT_NODE = "intent_node_classification"
    ENUMERATED_SLOT = "enumerated_slot"
    AMBIGUITY_FLAG = "ambiguity_flag"
    LOCALE = "locale_detection"
    ANSWER_BINDING = "answer_binding"
    INSTRUCTION_SCREEN = "instruction_likeness"


@dataclass(frozen=True, slots=True)
class Option:
    option_id: str
    label: str


@dataclass(frozen=True, slots=True)
class ChoiceQuestion:
    """`pick_only` marks the second stage of a gated slot (UND-18, ruling A603): the question is asked only after a yes/no gate has passed, and then it has NO abstention, because the abstention was
    the fragile part. Every other choice question keeps the mandatory abstention (PERD §19.1); a pick must not carry it, so the two kinds cannot be confused."""

    question_id: str
    prompt: str
    options: tuple[Option, ...]
    pick_only: bool = False


@dataclass(frozen=True, slots=True)
class YesNoQuestion:
    question_id: str
    prompt: str


@dataclass(frozen=True, slots=True)
class Level:
    label: str
    description: str


@dataclass(frozen=True, slots=True)
class ScoreQuestion:
    """Rate against ordered levels, lowest first. The levels are developer or pack text, like option labels."""

    question_id: str
    prompt: str
    levels: tuple[Level, ...]


Question = ChoiceQuestion | YesNoQuestion | ScoreQuestion


@dataclass(frozen=True, slots=True)
class ChoiceAnswer:
    """`selected` is the provider's own selection. `confidence` is the decision model's separate confidence value when it
    gives one (an LLM-backed answer has none). Both are information, never authority."""

    question_id: str
    selected: str
    probabilities: Mapping[str, float]
    confidence: float | None = None

    @property
    def abstained(self) -> bool:
        return self.selected == ABSTAIN


@dataclass(frozen=True, slots=True)
class YesNoAnswer:
    """`yes` is the model's own selection and is None when the provider returns only a probability (a decision model).
    Turning that probability into a yes or a no needs a threshold, which is a pack setting decided by benchmark and a
    founder (AGENT-16), so the harness never does it."""

    question_id: str
    yes: bool | None
    p_yes: float


@dataclass(frozen=True, slots=True)
class ScoreAnswer:
    """The probability per level (keys level_0, level_1, ...). `score` is the decision model's probability-weighted level
    index when it gives one; an LLM-backed answer carries only the distribution, because the harness computes no score."""

    question_id: str
    score: float | None
    probabilities: Mapping[str, float]
    confidence: float | None = None


Answer = ChoiceAnswer | YesNoAnswer | ScoreAnswer


def validate_questions(questions: tuple[Question, ...]) -> None:
    """Reject a malformed question set, and any choice question that does not offer the abstention."""
    if not questions:
        raise HarnessRejection(Reason.QUESTION_INVALID)
    seen: set[str] = set()
    for question in questions:
        if not _ID.match(question.question_id) or question.question_id in seen or not question.prompt:
            raise HarnessRejection(Reason.QUESTION_INVALID)
        seen.add(question.question_id)
        if isinstance(question, ScoreQuestion) and (len(question.levels) < 2 or not all(lv.label for lv in question.levels)):
            raise HarnessRejection(Reason.QUESTION_INVALID)
        if isinstance(question, ChoiceQuestion):
            ids = [o.option_id for o in question.options]
            if len(set(ids)) != len(ids) or not all(_ID.match(i) and o.label for i, o in zip(ids, question.options, strict=True)):
                raise HarnessRejection(Reason.QUESTION_INVALID)
            if question.pick_only:
                if ABSTAIN in ids:
                    raise HarnessRejection(Reason.QUESTION_INVALID)
            elif ABSTAIN not in ids:
                raise HarnessRejection(Reason.NO_ABSTENTION)
            if len(ids) < 2:
                raise HarnessRejection(Reason.QUESTION_INVALID)
