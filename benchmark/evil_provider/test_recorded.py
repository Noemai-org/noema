"""The provisional decision-model shape stays provisional until a real exchange is recorded (nothing is faked)."""
import json
import unittest
from pathlib import Path

from benchmark.evil_provider import decisions_format
from benchmark.evil_provider.adversary import Config
from benchmark.evil_provider.decisions_format import answer, parse_request

RECORDED = Path(__file__).parent / "recorded"


def pairs() -> list[tuple[Path, Path]]:
    return [(req, req.with_name(req.name.replace(".request.", ".response."))) for req in sorted(RECORDED.glob("*.request.json"))]


class RecordedTest(unittest.TestCase):
    def test_provisional_flag_matches_whether_a_recorded_exchange_exists(self) -> None:
        self.assertEqual(decisions_format.PROVISIONAL, not pairs(),
                         "record a real exchange and set PROVISIONAL = False, or remove the pair and set it to True")

    def test_every_recorded_pair_is_accepted_and_answered_in_the_recorded_shape(self) -> None:
        for request_path, response_path in pairs():
            self.assertTrue(response_path.exists(), f"{response_path.name} is missing")
            request = json.loads(request_path.read_text())
            recorded = json.loads(response_path.read_text())
            produced = answer(parse_request(request), Config(), {})
            self.assertEqual(set(produced), set(recorded), request_path.name)
            self.assertEqual(set(produced["answers"][0]), set(recorded["answers"][0]), request_path.name)


if __name__ == "__main__":
    unittest.main()
