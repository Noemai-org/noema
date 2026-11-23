"""The job-log scan's own allow file (tools/ci/g1_joblog.allow; noema#899, run 45128374794).

The first job-log scan that could read the log failed on a Go module version in a `go: downloading` line: a pseudo-version holds a 14-digit timestamp, which the Luhn card rule read as a card number
(15 digits with the separators). The allow file lets exactly that line shape through and nothing else: a card number in the same line, or in any other line, is still found. SYNTHETIC values."""
import re
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import leakscan  # noqa: E402

ALLOW = Path(__file__).resolve().parent / "g1_joblog.allow"


def allow() -> list[re.Pattern[str]]:
    return [re.compile(line) for line in ALLOW.read_text(encoding="utf-8").splitlines() if line.strip() and not line.startswith("#")]


def luhn_complete(body: str) -> str:
    total = 0
    for i, ch in enumerate(reversed(body)):
        d = int(ch) * 2 if i % 2 == 0 else int(ch)
        total += d - 9 if d > 9 else d
    return body + str((10 - total % 10) % 10)


def kinds(line: str, **kw: object) -> set[str]:
    return {f.kind for f in leakscan.scan_text(line, "joblog", **kw)}


class JoblogAllow(unittest.TestCase):
    PSEUDO = "2026-11-23T07:03:50.1234567Z go: downloading github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761"

    def test_without_the_allow_file_the_pseudo_version_line_is_a_false_card_finding(self) -> None:
        # The failure as it happened: with the digit-run rule off, the card rule still reads the line.
        self.assertIn("card_number", kinds(self.PSEUDO, digits=False))

    def test_the_go_download_line_passes_with_the_allow_file(self) -> None:
        self.assertEqual(kinds(self.PSEUDO, digits=False, allow=allow()), set())

    def test_a_card_number_in_a_go_download_line_is_still_found(self) -> None:
        card = luhn_complete("4" + "12345678901234")
        for line in (f"2026-11-23T07:03:50.1Z go: downloading example.org/x v1.0.0 {card}", f"2026-11-23T07:03:50.1Z go: downloading example.org/{card}/x v1.0.0"):
            self.assertIn("card_number", kinds(line, digits=False, allow=allow()), line)

    def test_a_card_number_in_any_other_line_is_still_found(self) -> None:
        card = luhn_complete("4" + "12345678901234")
        self.assertIn("card_number", kinds(f"2026-11-23T07:03:50.1Z planted card {card}", digits=False, allow=allow()))

    def test_a_secret_in_a_go_download_line_is_still_found(self) -> None:
        self.assertIn("named_secret_value", kinds("2026-11-23T07:03:50.1Z go: downloading example.org/x v1.0.0 api_key=0123456789abcdef", digits=False, allow=allow()))


if __name__ == "__main__":
    unittest.main()
