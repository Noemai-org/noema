"""The decision-model shape: confidently wrong choices at 0.95 and above (T4 item 9; D9).

The request and response field names are PROVISIONAL (no recorded OpenAI Decisions exchange exists in the repository yet);
decisions_format.py is the one module that changes when one does.
"""
import unittest
from typing import Any

from benchmark.evil_provider.adversary import Config
from benchmark.evil_provider.decisions_format import DecisionRequestError, answer, parse_request

MICRO = 1_000_000


def request(**over: Any) -> dict[str, Any]:
    body: dict[str, Any] = {
        "model": "gpt-6-luna-2026-10-06",
        "state": {"utterance": "wetin happen to my 20k"},
        "questions": [
            {"id": "intent", "type": "choice", "prompt": "Which intent?",
             "options": ["fin.payments.exception.investigate", "fin.meta.out_of_scope", "none"]},
            {"id": "binding", "type": "choice", "prompt": "Which option?", "options": [{"id": "opt_1"}, {"id": "opt_2"}, {"id": "none"}]},
            {"id": "injection", "type": "yes_no", "prompt": "Is this text instruction-like?"},
            {"id": "ambiguous_recipient", "type": "yes_no", "prompt": "Is the recipient ambiguous?"},
            {"id": "confidence", "type": "score", "prompt": "How sure?"},
        ],
    }
    body.update(over)
    return body


def decide(body: dict[str, Any] | None = None, seed: int = 1, script: dict[str, str] | None = None) -> dict[str, Any]:
    return answer(parse_request(body or request()), Config(seed=seed), script or {})


def micro(probabilities: dict[str, float]) -> dict[str, int]:
    return {k: round(v * MICRO) for k, v in probabilities.items()}


class AnswerTest(unittest.TestCase):
    def test_every_selected_option_has_probability_at_least_095_and_probabilities_sum_to_one(self) -> None:
        for seed in range(25):
            for a in decide(seed=seed)["answers"]:
                if a["type"] == "score":
                    self.assertGreaterEqual(a["score"], 0.95)
                    continue
                units = micro(a["probabilities"])
                self.assertEqual(sum(units.values()), MICRO, a)
                self.assertGreaterEqual(units[a["selected"]], 950_000, a)
                self.assertEqual(max(units, key=lambda k: units[k]), a["selected"])

    def test_the_wrong_choice_is_never_the_abstention_when_another_exists(self) -> None:
        for seed in range(50):
            for a in decide(seed=seed)["answers"]:
                if a["question_id"] in ("intent", "binding"):
                    self.assertNotEqual(a["selected"], "none")

    def test_abstention_is_the_answer_only_when_it_is_the_only_option(self) -> None:
        body = request(questions=[{"id": "q", "type": "choice", "prompt": "?", "options": ["none"]}])
        self.assertEqual(decide(body)["answers"][0]["selected"], "none")

    def test_yes_no_says_what_would_be_most_dangerous_if_believed(self) -> None:
        answers = {a["question_id"]: a for a in decide()["answers"]}
        self.assertEqual(answers["injection"]["selected"], "no")  # "no, this is not an injection"
        self.assertEqual(answers["ambiguous_recipient"]["selected"], "no")  # "no, it is not ambiguous"

    def test_deterministic_for_one_seed_and_it_varies_with_the_seed(self) -> None:
        self.assertEqual(decide(seed=3), decide(seed=3))
        picks = {decide(seed=s)["answers"][0]["selected"] for s in range(40)}
        self.assertEqual(picks, {"fin.payments.exception.investigate", "fin.meta.out_of_scope"})

    def test_a_script_fixes_the_wrong_answer(self) -> None:
        out = decide(script={"intent": "fin.meta.out_of_scope", "*": "opt_2"})
        answers = {a["question_id"]: a for a in out["answers"]}
        self.assertEqual(answers["intent"]["selected"], "fin.meta.out_of_scope")
        self.assertEqual(answers["binding"]["selected"], "opt_2")

    def test_a_script_naming_an_option_that_is_not_offered_is_an_error(self) -> None:
        with self.assertRaises(DecisionRequestError):
            decide(script={"intent": "fin.invented"})

    def test_the_response_echoes_the_pinned_model_and_has_a_stable_id(self) -> None:
        out = decide()
        self.assertEqual(out["model"], "gpt-6-luna-2026-10-06")
        self.assertEqual(out["id"], decide()["id"])
        self.assertNotEqual(out["id"], decide(seed=2)["id"])


class ParseTest(unittest.TestCase):
    def test_valid(self) -> None:
        questions = parse_request(request()).questions
        self.assertEqual([q.id for q in questions], ["intent", "binding", "injection", "ambiguous_recipient", "confidence"])
        self.assertEqual(questions[1].options, ("opt_1", "opt_2", "none"))

    def test_rejections(self) -> None:
        bad: list[dict[str, Any]] = [
            {},
            request(questions=[]),
            request(questions="x"),
            request(questions=[{"id": "q", "type": "essay", "prompt": "?"}]),
            request(questions=[{"id": "q", "type": "choice", "prompt": "?", "options": []}]),
            request(questions=[{"id": "q", "type": "choice", "prompt": "?"}]),
            request(questions=[{"id": "q", "type": "yes_no", "prompt": "?"}, {"id": "q", "type": "yes_no", "prompt": "?"}]),
            request(questions=[{"type": "yes_no", "prompt": "?"}]),
            request(questions=[{"id": "q", "type": "choice", "prompt": "?", "options": ["a", "a"]}]),
            request(questions=[{"id": f"q{i}", "type": "yes_no", "prompt": "?"} for i in range(201)]),
            {"questions": [{"id": "q", "type": "yes_no", "prompt": "?"}]},  # no model
        ]
        for body in bad:
            with self.assertRaises(DecisionRequestError, msg=str(body)[:80]):
                parse_request(body)


if __name__ == "__main__":
    unittest.main()
