"""Tests for leakscan.py (SEC-03, REQ-SEC-003): it finds planted leaks, passes redacted output, and never prints a value.

Every planted value is SYNTHETIC (AGENT-25). Secret-shaped strings are assembled at run time so that this file is not itself a
secret finding for the repository's own scanner.
"""
import contextlib
import io
import re
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import leakscan as s  # noqa: E402

ACCOUNT = "0123" + "456789"  # 10 digits: an account number
CARD = "4111" + "1111" + "1111" + "1111"  # 16 digits, Luhn-valid
AWS_KEY_ID = "AKIA" + "IOSFODNN7EXAMPLE"
JWT = "ey" + "JhbGciOiJFUzI1NiJ9" + "." + "eyJzdWIiOiJ4In0aaaa" + "." + "c2lnbmF0dXJlYWFhYQ"
BEARER = "Authorization: Bearer " + "abcdefghij1234567890ABCD"
PROVIDER_KEY = "sk-" + "a1b2c3d4e5f6g7h8i9j0k1l2"
PEM = "-----BEGIN " + "PRIVATE KEY-----"
NAMED = "hmac_key=" + "a1b2c3d4e5f6a7b8c9d0"


def kinds(text: str, **kw) -> list[str]:
    return [f.kind for f in s.scan_text(text, "x.log", **kw)]


class DigitRuns(unittest.TestCase):
    def test_a_planted_account_number_in_a_log_line_is_caught(self):
        self.assertEqual(kinds(f'{{"msg":"transfer","account":"{ACCOUNT}"}}'), ["account_number"])

    def test_a_masked_account_number_passes(self):
        self.assertEqual(kinds('{"account":"******6789","ending":"6789"}'), [])

    def test_a_run_of_four_passes_and_a_run_of_five_is_caught(self):
        self.assertEqual(kinds("code 1234"), [])
        self.assertEqual(kinds("code 12345"), ["digit_run"])

    def test_a_luhn_valid_card_number_is_named_as_one(self):
        self.assertEqual(kinds(f"card {CARD}"), ["card_number"])

    def test_fractional_seconds_are_not_a_digit_run(self):
        self.assertEqual(kinds("at 2026-11-05T09:00:00.123456Z ok"), [])
        self.assertEqual(kinds("at 09:00:00,123456 ok"), [])

    def test_a_run_inside_an_alphanumeric_identifier_is_not_a_digit_run(self):
        self.assertEqual(kinds("evd_01J9ZK12345678ABCDEF and sha256:0123456789abcdef"), [])

    def test_a_prefixed_numeric_identifier_is_caught(self):
        self.assertEqual(kinds(f"acct_{ACCOUNT}"), ["account_number"])

    def test_the_allow_list_skips_the_digit_rule_only(self):
        allow = (re.compile(r'"amount_minor"'),)
        self.assertEqual(kinds('{"amount_minor": 150000}', allow=allow), [])
        self.assertEqual(kinds(f'{{"amount_minor": 150000, "key": "{PROVIDER_KEY}"}}', allow=allow), ["provider_key"])


class Secrets(unittest.TestCase):
    def test_each_secret_shape_is_caught(self):
        for want, text in [("private_key_block", PEM), ("cloud_access_key_id", f"id={AWS_KEY_ID}"), ("jwt", f"t {JWT}"),
                           ("bearer_token", BEARER), ("provider_key", f"k {PROVIDER_KEY}"), ("named_secret_value", NAMED)]:
            with self.subTest(want):
                self.assertIn(want, kinds(text))

    def test_a_secret_file_path_is_not_a_secret(self):
        self.assertEqual(kinds("OPENAI_API_KEY_FILE=/run/secrets/openai_key_file_v1"), [])

    def test_a_redaction_placeholder_is_not_a_secret(self):
        self.assertEqual(kinds('{"token": "[REDACTED]", "password": "********************"}'), [])


class Canaries(unittest.TestCase):
    def test_a_seeded_canary_is_caught_whatever_its_shape(self):
        canary = "canary-Zq7-unremarkable-word"
        self.assertEqual(kinds(f"explanation mentions {canary} here", canaries=(canary,)), ["canary"])

    def test_a_clean_text_with_a_canary_list_passes(self):
        self.assertEqual(kinds("nothing to see", canaries=("canary-Zq7",)), [])


class Reports(unittest.TestCase):
    def test_a_report_never_contains_the_value(self):
        findings = s.scan_text(f"a {ACCOUNT} b {PROVIDER_KEY} c {NAMED}", "x.log", canaries=())
        self.assertGreaterEqual(len(findings), 3)
        for f in findings:
            line = str(f)
            for value in (ACCOUNT, PROVIDER_KEY, "a1b2c3d4e5f6a7b8c9d0"):
                self.assertNotIn(value, line)

    def test_the_line_number_and_path_are_right(self):
        found = s.scan_text(f"ok\nok\nacct {ACCOUNT}\n", "dir/a.log")
        self.assertEqual([(f.path, f.line, f.kind) for f in found], [("dir/a.log", 3, "account_number")])


class Cli(unittest.TestCase):
    def run_main(self, argv):
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = s.main(argv)
        return code, out.getvalue()

    def test_clean_directory_exits_zero(self):
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "a.log").write_text('{"account":"******6789"}\n')
            code, out = self.run_main([tmp])
        self.assertEqual((code, out.strip()), (0, "leakscan OK"))

    def test_a_planted_leak_in_a_nested_file_exits_one_and_names_it(self):
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "sub").mkdir()
            (Path(tmp) / "sub" / "api.json").write_text(f'{{"account": "{ACCOUNT}"}}\n')
            (Path(tmp) / "clean.log").write_text("fine\n")
            code, out = self.run_main([tmp])
        self.assertEqual(code, 1)
        self.assertIn("api.json:1: account_number", out)
        self.assertNotIn(ACCOUNT, out)
        self.assertNotIn("clean.log", out)

    def test_canary_and_allow_files_are_read(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            (d / "t.log").write_text('canary-Zq7 and {"amount_minor": 150000}\n')
            (d / "canaries.txt").write_text("canary-Zq7\n")
            (d / "allow.txt").write_text('"amount_minor"\n')
            code, out = self.run_main([str(d / "t.log"), "--canary", str(d / "canaries.txt"), "--allow", str(d / "allow.txt")])
        self.assertEqual(code, 1)
        self.assertEqual(out.count("leakscan FAIL"), 1)
        self.assertIn("canary", out)

    def test_a_missing_path_is_a_usage_error(self):
        code, _ = self.run_main(["/no/such/path/for/leakscan"])
        self.assertEqual(code, 2)


if __name__ == "__main__":
    unittest.main()
