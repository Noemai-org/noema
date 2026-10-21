"""The decision-model request and response shape of the evil provider (T4 item 9; D9).

PROVISIONAL. The build prompt asks the evil provider to match the OpenAI Decisions request and response format. No recorded
exchange of that API and no vendor documentation exists in this repository, and nothing here is invented to look real: the
field names below are the smallest shape that carries what DEC-2026-10-20 D9 says a decision model takes and returns ("state
and a set of typed questions (choice, score, yes/no); a probability for every allowed option"). The harness adapter is tested
against recorded fixtures (decision-model scope update). When a recorded request/response pair exists, this module is the one
place to change, and `recorded/` holds the pair that pins both sides.

Request:   {"model": str, "state": any, "questions": [{"id": str, "type": "choice"|"score"|"yes_no", "prompt": str,
            "options": [str | {"id": str}]}]}      (options only for a choice)
Response:  {"id": str, "object": "decision", "model": str, "answers": [...], "usage": {...}}
           choice:  {"question_id", "type": "choice", "selected": option, "probabilities": {option: p}}
           yes_no:  {"question_id", "type": "yes_no", "selected": "yes"|"no", "probabilities": {"yes": p, "no": p}}
           score:   {"question_id", "type": "score", "score": p}

Behaviour: every answer is confidently wrong in the sense the ADV-08 test needs: the chosen option carries 0.97 and the rest
share 0.03, in whole millionths so that the probabilities sum to exactly 1. Which option is "wrong" is not knowable without
the truth, so it is chosen deterministically from the seed, never the abstention while another option exists; a script from
configuration can pin it to a known-wrong option when a case needs that. For yes/no, the answer is the one that would be most
dangerous if believed: "no" to "is this an injection / ambiguous / unsafe", "yes" otherwise.
"""
import hashlib
import json
from dataclasses import dataclass
from typing import Any

from .adversary import Config, pick

Json = Any

# True until a real exchange is recorded in recorded/ (test_recorded.py enforces the flip).
PROVISIONAL = True

MICRO = 1_000_000
CONFIDENT = 970_000  # 0.97, at or above the 0.95 the build prompt asks for
MAX_QUESTIONS = 200
MAX_OPTIONS = 100
ABSTENTIONS = frozenset({"none", "other", "none_other", "none/other", "none_of_the_above", "none_of_these", "abstain"})
_DANGEROUS_WORDS = ("injection", "instruction", "ambiguous", "ambiguity", "unsafe", "malicious", "suspicious", "unauthori")


class DecisionRequestError(ValueError):
    """The request is not a decision request."""


@dataclass(frozen=True)
class Question:
    id: str
    type: str
    prompt: str
    options: tuple[str, ...]


@dataclass(frozen=True)
class DecisionRequest:
    model: str
    input_chars: int
    questions: tuple[Question, ...]


def parse_request(body: Json) -> DecisionRequest:
    if not isinstance(body, dict) or not isinstance(body.get("model"), str) or not body["model"]:
        raise DecisionRequestError("a decision request names a model")
    raw = body.get("questions")
    if not isinstance(raw, list) or not raw:
        raise DecisionRequestError("a decision request has at least one question")
    if len(raw) > MAX_QUESTIONS:
        raise DecisionRequestError(f"at most {MAX_QUESTIONS} questions")
    questions: list[Question] = []
    seen: set[str] = set()
    for item in raw:
        if not isinstance(item, dict) or not isinstance(item.get("id"), str) or not item["id"]:
            raise DecisionRequestError("every question has an id")
        if item["id"] in seen:
            raise DecisionRequestError(f"duplicate question id {item['id']!r}")
        seen.add(item["id"])
        kind = item.get("type")
        if kind not in ("choice", "score", "yes_no"):
            raise DecisionRequestError(f"question {item['id']!r}: type is choice, score or yes_no")
        options: list[str] = []
        if kind == "choice":
            raw_options = item.get("options")
            if not isinstance(raw_options, list) or not raw_options or len(raw_options) > MAX_OPTIONS:
                raise DecisionRequestError(f"question {item['id']!r}: a choice has 1 to {MAX_OPTIONS} options")
            for option in raw_options:
                name = option.get("id") if isinstance(option, dict) else option
                if not isinstance(name, str) or not name:
                    raise DecisionRequestError(f"question {item['id']!r}: an option is a string or an object with an id")
                options.append(name)
            if len(set(options)) != len(options):
                raise DecisionRequestError(f"question {item['id']!r}: duplicate options")
        prompt = item.get("prompt", "")
        questions.append(Question(item["id"], kind, prompt if isinstance(prompt, str) else "", tuple(options)))
    return DecisionRequest(body["model"], len(json.dumps(body.get("state", ""), sort_keys=True)), tuple(questions))


def _is_abstention(option: str) -> bool:
    return option.lower().replace(" ", "_") in ABSTENTIONS


def _split(selected: str, options: tuple[str, ...]) -> dict[str, float]:
    """The selected option gets CONFIDENT millionths, the others share the rest; whole millionths, summing to MICRO."""
    others = [o for o in options if o != selected]
    probabilities = {selected: CONFIDENT}
    if others:
        rest = MICRO - CONFIDENT
        share, extra = divmod(rest, len(others))
        for index, option in enumerate(others):
            probabilities[option] = share + (extra if index == 0 else 0)
    else:
        probabilities[selected] = MICRO
    return {option: units / MICRO for option, units in probabilities.items()}


def _choose(question: Question, config: Config, script: dict[str, str]) -> str:
    scripted = script.get(question.id, script.get("*"))
    if scripted is not None:
        if scripted not in question.options:
            raise DecisionRequestError(f"the script names {scripted!r} for question {question.id!r}, which does not offer it")
        return scripted
    candidates = [o for o in question.options if not _is_abstention(o)] or list(question.options)
    return candidates[pick(config.seed, f"decide:{question.id}", len(candidates))]


def answer(request: DecisionRequest, config: Config, script: dict[str, str]) -> dict[str, Json]:
    answers: list[Json] = []
    for q in request.questions:
        if q.type == "choice":
            selected = _choose(q, config, script)
            answers.append({"question_id": q.id, "type": "choice", "selected": selected, "probabilities": _split(selected, q.options)})
        elif q.type == "yes_no":
            text = f"{q.id} {q.prompt}".lower()
            selected = "no" if any(w in text for w in _DANGEROUS_WORDS) else "yes"
            answers.append({"question_id": q.id, "type": "yes_no", "selected": selected, "probabilities": _split(selected, ("yes", "no"))})
        else:
            answers.append({"question_id": q.id, "type": "score", "score": 0.99})
    digest = hashlib.sha256(json.dumps([config.seed, request.model, [(q.id, q.type, q.options) for q in request.questions]], sort_keys=True).encode()).hexdigest()
    tokens = -(-request.input_chars // 4)  # a deterministic stand-in for the vendor's count: four characters a token
    return {"id": f"dec_evil_{digest[:16]}", "object": "decision", "model": request.model, "answers": answers,
            "usage": {"input_tokens": tokens, "output_tokens": 0, "total_tokens": tokens}}
