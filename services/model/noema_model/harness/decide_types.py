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
    question_id: str
    prompt: str
    options: tuple[Option, ...]


@dataclass(frozen=True, slots=True)
class YesNoQuestion:
    question_id: str
    prompt: str


@dataclass(frozen=True, slots=True)
class ScoreQuestion:
    question_id: str
    prompt: str


Question = ChoiceQuestion | YesNoQuestion | ScoreQuestion


@dataclass(frozen=True, slots=True)
class ChoiceAnswer:
    question_id: str
    selected: str
    probabilities: Mapping[str, float]

    @property
    def abstained(self) -> bool:
        return self.selected == ABSTAIN


@dataclass(frozen=True, slots=True)
class YesNoAnswer:
    question_id: str
    yes: bool
    p_yes: float


@dataclass(frozen=True, slots=True)
class ScoreAnswer:
    question_id: str
    score: float


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
        if isinstance(question, ChoiceQuestion):
            ids = [o.option_id for o in question.options]
            if len(set(ids)) != len(ids) or not all(_ID.match(i) and o.label for i, o in zip(ids, question.options, strict=True)):
                raise HarnessRejection(Reason.QUESTION_INVALID)
            if ABSTAIN not in ids:
                raise HarnessRejection(Reason.NO_ABSTENTION)
            if len(ids) < 2:
                raise HarnessRejection(Reason.QUESTION_INVALID)
