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

    def test_the_same_line_with_the_image_build_step_prefix_passes_too(self) -> None:
        # Run 45161180436: the line as the image build prints it is `<timestamp> #<step> <seconds> go: downloading ...`.
        line = "2026-11-23T11:01:20.1234567Z #34 3.128 go: downloading github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761"
        self.assertIn("card_number", kinds(line, digits=False))
        self.assertEqual(kinds(line, digits=False, allow=allow()), set())
        card = luhn_complete("4" + "12345678901234")
        self.assertIn("card_number", kinds(f"2026-11-23T11:01:20.1Z #34 3.128 go: downloading example.org/x v1.0.0 {card}", digits=False, allow=allow()))

    def test_a_card_number_in_a_go_download_line_is_still_found(self) -> None:
        card = luhn_complete("4" + "12345678901234")
        for line in (f"2026-11-23T07:03:50.1Z go: downloading example.org/x v1.0.0 {card}", f"2026-11-23T07:03:50.1Z go: downloading example.org/{card}/x v1.0.0"):
            self.assertIn("card_number", kinds(line, digits=False, allow=allow()), line)

    def test_a_card_number_in_any_other_line_is_still_found(self) -> None:
        card = luhn_complete("4" + "12345678901234")
        self.assertIn("card_number", kinds(f"2026-11-23T07:03:50.1Z planted card {card}", digits=False, allow=allow()))

    def test_a_secret_in_a_go_download_line_is_still_found(self) -> None:
        self.assertIn("named_secret_value", kinds("2026-11-23T07:03:50.1Z go: downloading example.org/x v1.0.0 " + "api_" + "key=" + "synthetic" + "0123456789", digits=False, allow=allow()))

    # SR's two bypasses on noema#908 (A154: no digit group longer than the maximum, except the one 14-digit timestamp), each a Luhn-valid 15-digit number built at run time.
    def card(self) -> str:
        return luhn_complete("4" + "12345678901234")

    def test_a_hyphen_grouped_card_number_in_the_module_path_is_still_found(self) -> None:
        card = self.card()
        grouped = "-".join((card[0:4], card[4:8], card[8:12], card[12:]))
        line = f"2026-11-23T11:01:20.1234567Z go: downloading example.org/{grouped}/x v1.0.0"
        self.assertIn("card_number", kinds(line, digits=False), "the line must be a finding without the allow file, or the test proves nothing")
        self.assertIn("card_number", kinds(line, digits=False, allow=allow()))

    def test_a_card_number_in_the_pseudo_version_prefix_is_still_found(self) -> None:
        line = f"2026-11-23T11:01:20.1234567Z go: downloading example.org/x v1.0.0-{self.card()}.20240606120523-5a60cdf6a761"
        self.assertIn("card_number", kinds(line, digits=False), "the line must be a finding without the allow file, or the test proves nothing")
        self.assertIn("card_number", kinds(line, digits=False, allow=allow()))

    def test_a_card_number_as_a_version_component_the_step_prefix_or_the_log_timestamp_is_never_allowed(self) -> None:
        # The invariant is that the allow file hides nothing: it does not match these lines at all, so the scan of each is exactly what it is without the file (for the version component, a finding).
        card = self.card()
        for name, line in (
            ("a version component", f"2026-11-23T11:01:20.1234567Z go: downloading example.org/x v1.0.{card}"),
            ("the step prefix", f"2026-11-23T11:01:20.1234567Z #{card} 3.128 go: downloading example.org/x v1.0.0"),
            ("the log timestamp", f"{card}Z go: downloading example.org/x v1.0.0"),
            ("the step seconds", f"2026-11-23T11:01:20.1234567Z #34 {card} go: downloading example.org/x v1.0.0"),
        ):
            with self.subTest(name):
                self.assertFalse(any(a.search(line) for a in allow()), line)
                self.assertEqual(kinds(line, digits=False, allow=allow()), kinds(line, digits=False), line)

    def test_more_than_twelve_digits_in_the_module_path_are_refused_but_twelve_are_not(self) -> None:
        ok = "2026-11-23T11:01:20.1234567Z go: downloading example.org/v1234567890/x v1.0.0"
        long = "2026-11-23T11:01:20.1234567Z go: downloading example.org/a1/b2/c3/d4/e5/f6/g7/h8/i9/j1/k2/l3/m4/x v1.0.0"
        self.assertTrue(any(a.search(ok) for a in allow()), "twelve digits or fewer pass the path bound")
        self.assertFalse(any(a.search(long) for a in allow()), "thirteen digits in the path are not allowed")


if __name__ == "__main__":
    unittest.main()
