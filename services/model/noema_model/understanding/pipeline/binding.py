"""Task 5: map the customer's answer to a clarification option, or to none (REQ-AMB-005).

This returns a proposal only. The deterministic label-overlap confirmation that must follow, and the decision to bind, are
Trust's (T6.4). The option labels are built by the core from enterprise data, so they travel as delimited data fields
and the question's own option labels are neutral ("Option 1").
"""

from dataclasses import dataclass

from noema_model.harness import (
    ABSTAIN,
    CallRecord,
    ChoiceAnswer,
    ChoiceQuestion,
    DecideRequest,
    DecisionTask,
    Harness,
    Option,
)


@dataclass(frozen=True, slots=True)
class BindingProposal:
    option_id: str | None
    probabilities: dict[str, float]
    call: CallRecord


async def propose_binding(
    harness: Harness,
    *,
    answer_text: str,
    options: tuple[tuple[str, str], ...],
    locale: str | None,
    correlation_id: str,
) -> BindingProposal:
    """`options` are (option_id, label) pairs from the core's clarification. Returns the option the model picked, or None."""
    if not options:
        raise ValueError("a binding needs at least one option")
    ids = {f"o{i}": option_id for i, (option_id, _) in enumerate(options)}
    state = {"answer": answer_text, **{f"label_o{i}": label for i, (_, label) in enumerate(options)}}
    question = ChoiceQuestion(
        "binding",
        "Which option does the customer's answer pick? The text of option N is in the field label_oN.",
        (*(Option(f"o{i}", f"Option {i + 1}") for i in range(len(options))), Option(ABSTAIN, "None of these, or not sure")),
    )
    result = await harness.decide(DecideRequest(DecisionTask.ANSWER_BINDING, state, (question,), locale, correlation_id))
    (answer,) = result.answers
    assert isinstance(answer, ChoiceAnswer)
    return BindingProposal(
        option_id=None if answer.abstained else ids[answer.selected],
        probabilities={ids.get(k, k): v for k, v in answer.probabilities.items()},
        call=result.call,
    )
