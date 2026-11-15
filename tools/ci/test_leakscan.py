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
NAMED_VALUE = "a1b2c3d4" + "e5f6a7b8c9d0"
NAMED = "hmac_key=" + NAMED_VALUE


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


    def test_a_formatted_account_or_card_number_is_caught(self):
        """Codex P2 on #222: single spaces or hyphens between digits (the model redactor treats them as part of the number)."""
        self.assertEqual(kinds('{"account":"0123 456 789"}'), ["account_number"])
        self.assertEqual(kinds('{"account":"0123-456-789"}'), ["account_number"])
        self.assertEqual(kinds('{"card":"4111-1111-1111-1111"}'), ["card_number"])
        self.assertEqual(kinds('{"card":"4111 1111 1111 1111"}'), ["card_number"])

    def test_an_iso_date_and_time_with_hyphens_are_still_not_a_digit_run(self):
        self.assertEqual(kinds("on 2026-11-05 at 09:00:00 and 2026-11-05T09:00:00Z"), [])

    def test_a_words_only_secret_value_assigned_to_a_secret_name_is_caught(self):
        """Codex P2 on #222: a password or token need not contain a digit."""
        self.assertEqual(kinds('{"password":"syntheticpasswordvalue"}'), ["named_secret_value"])
        self.assertEqual(kinds("api_key=abcdefghijklmnopqrstuv"), ["named_secret_value"])

    def test_paths_and_placeholders_are_still_not_secrets(self):
        self.assertEqual(kinds('{"password":"/run/secrets/db_password_file"}'), [])
        self.assertEqual(kinds('{"password":"****************"}'), [])
        self.assertEqual(kinds('{"token":"redacted-redacted-redacted"}'), [])


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
            for value in (ACCOUNT, PROVIDER_KEY, NAMED_VALUE):
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
        self.assertEqual(code, 0)
        self.assertEqual(out.strip(), "leakscan OK: scanned 1 files, 1 lines")

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

    def test_a_directory_that_cannot_be_listed_is_a_finding_not_a_clean_scan(self):
        """Codex P1 on #222: Path.rglob swallows OSError while walking, so an unreadable subtree vanished and the scan printed OK."""
        import os
        from unittest import mock

        real = os.scandir
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "hidden").mkdir()
            (Path(tmp) / "hidden" / "a.log").write_text(f"acct {ACCOUNT}\n")
            (Path(tmp) / "ok.log").write_text("fine\n")
            hidden = str(Path(tmp) / "hidden")

            def fake(path=".", *a, **k):
                if str(path) == hidden:
                    raise PermissionError(13, "denied", str(path))
                return real(path, *a, **k)

            with mock.patch("os.scandir", side_effect=fake):
                code, out = self.run_main([tmp])
        self.assertEqual(code, 1)
        self.assertIn("unreadable_directory", out)
        self.assertNotIn("leakscan OK", out)

    def test_a_real_unreadable_directory_is_a_finding(self):
        """The same P1 without a mock, where the operating system can deny a listing (a non-root POSIX user, as in CI)."""
        import os

        if os.name != "posix" or os.geteuid() == 0:
            self.skipTest("needs a POSIX user that is not root; the mocked test above covers the logic everywhere")
        with tempfile.TemporaryDirectory() as tmp:
            hidden = Path(tmp) / "hidden"
            hidden.mkdir()
            (hidden / "a.log").write_text(f"acct {ACCOUNT}\n")
            hidden.chmod(0)
            try:
                code, out = self.run_main([tmp])
            finally:
                hidden.chmod(0o700)
        self.assertEqual(code, 1)
        self.assertIn("unreadable_directory", out)

    def test_a_missing_path_is_a_usage_error(self):
        code, _ = self.run_main(["/no/such/path/for/leakscan"])
        self.assertEqual(code, 2)


class Vacuous(unittest.TestCase):
    """C1 blocker 1 on #222: a scan that looked at nothing must not say OK (REQ-SEC-003; the same rule as the unreadable-directory fix)."""

    run_main = Cli.run_main

    def scan(self, make, extra=()):
        with tempfile.TemporaryDirectory() as tmp:
            make(Path(tmp))
            return self.run_main([tmp, *extra])

    def test_an_empty_directory_is_a_usage_error_not_ok(self):
        code, out = self.scan(lambda d: None)
        self.assertEqual(code, 2)
        self.assertNotIn("leakscan OK", out)

    def test_an_empty_file_is_a_usage_error_not_ok(self):
        code, out = self.scan(lambda d: (d / "a.log").write_text(""))
        self.assertEqual(code, 2)
        self.assertNotIn("leakscan OK", out)

    def test_a_symlink_is_a_finding(self):
        import os

        def make(d):
            (d / "real").mkdir()
            (d / "real" / "a.log").write_text(f"acct {ACCOUNT}\n")
            os.symlink(d / "real", d / "link")
            (d / "b.log").write_text("fine\n")

        code, out = self.scan(make)
        self.assertEqual(code, 1)
        self.assertIn("symlink", out)

    def test_a_gzip_file_is_a_finding(self):
        import gzip

        def make(d):
            with gzip.open(d / "a.log.gz", "wb") as f:
                f.write(f"acct {ACCOUNT}\n".encode())
            (d / "b.log").write_text("fine\n")

        code, out = self.scan(make)
        self.assertEqual(code, 1)
        self.assertIn("binary_or_undecodable", out)

    def test_a_utf16_log_is_a_finding_not_a_clean_scan(self):
        def make(d):
            (d / "a.log").write_bytes(f"acct {ACCOUNT}\n".encode("utf-16"))
            (d / "b.log").write_text("fine\n")

        code, out = self.scan(make)
        self.assertEqual(code, 1)
        self.assertIn("binary_or_undecodable", out)

    def test_a_canary_file_without_a_value_is_a_usage_error(self):
        def make(d):
            (d / "a.log").write_text("fine\n")
            (d / "canaries.txt").write_text("\n   \n")

        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            make(d)
            code, out = self.run_main([str(d / "a.log"), "--canary", str(d / "canaries.txt")])
        self.assertEqual(code, 2)
        self.assertNotIn("leakscan OK", out)

    def test_an_invalid_allow_regex_or_missing_file_is_exit_two_not_a_traceback(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            (d / "a.log").write_text("fine\n")
            (d / "allow.txt").write_text("([unclosed\n")
            self.assertEqual(self.run_main([str(d / "a.log"), "--allow", str(d / "allow.txt")])[0], 2)
            self.assertEqual(self.run_main([str(d / "a.log"), "--allow", str(d / "missing.txt")])[0], 2)
            self.assertEqual(self.run_main([str(d / "a.log"), "--canary", str(d / "missing.txt")])[0], 2)

    def test_an_allow_pattern_that_matches_everything_is_refused(self):
        with tempfile.TemporaryDirectory() as tmp:
            d = Path(tmp)
            (d / "a.log").write_text(f"acct {ACCOUNT}\n")
            for rx in (".*", "^", "x*"):
                (d / "allow.txt").write_text(rx + "\n")
                code, _ = self.run_main([str(d / "a.log"), "--allow", str(d / "allow.txt")])
                self.assertEqual(code, 2, "pattern %r matches an empty string and must be refused" % rx)


class Fingerprint(unittest.TestCase):
    """C1 blocker 2: an unsalted short hash of a digit run can be reversed by enumeration, so the report was not safe to log."""

    def test_the_fingerprint_is_not_a_plain_hash_of_the_value(self):
        import hashlib

        found = s.scan_text(f"acct {ACCOUNT}", "x.log")[0]
        for algo in ("sha256", "sha1", "md5", "sha512"):
            plain = hashlib.new(algo, ACCOUNT.encode()).hexdigest()
            self.assertNotIn(found.fingerprint, plain, algo)

    def test_the_fingerprint_differs_between_runs(self):
        import subprocess

        code = ("import sys; sys.path.insert(0, %r); import leakscan as s; print(s.scan_text('acct %s', 'x')[0].fingerprint)"
                % (str(Path(__file__).resolve().parent), ACCOUNT))
        a = subprocess.run([sys.executable, "-I", "-c", code], capture_output=True, text=True, check=True).stdout.strip()
        b = subprocess.run([sys.executable, "-I", "-c", code], capture_output=True, text=True, check=True).stdout.strip()
        self.assertNotEqual(a, b)

    def test_the_same_value_has_one_fingerprint_within_a_run(self):
        a, b = s.scan_text(f"acct {ACCOUNT}\nagain {ACCOUNT}", "x")
        self.assertEqual(a.fingerprint, b.fingerprint)


class CodexP2Round2(unittest.TestCase):
    """Codex P2 findings on 4a36112."""

    def test_a_date_shaped_prefix_does_not_hide_the_rest_of_an_account_number(self):
        self.assertEqual(kinds('{"account":"0123-45-67-89"}'), ["account_number"])
        self.assertEqual(kinds("acct 0123-45-67 89"), ["account_number"])

    def test_a_complete_date_token_is_still_exempt(self):
        self.assertEqual(kinds("on 2026-11-05, again 2026-11-06T10:00:00Z"), [])

    def test_a_long_fraction_is_not_a_timestamp_fraction(self):
        """C1 should-fix 7: the fraction mask must not hide ten digits."""
        self.assertEqual(kinds("x 01:02:03.0123456789"), ["account_number"])
        self.assertEqual(kinds("at 2026-11-05T09:00:00.123456789Z"), [])

    def test_a_password_with_punctuation_is_caught(self):
        self.assertEqual(kinds('{"password":"!synthetic.password.value!"}'), ["named_secret_value"])
        self.assertEqual(kinds('{"password":"synthetic.password.value"}'), ["named_secret_value"])

    def test_realistic_named_secret_forms_are_caught(self):
        """C1 should-fix 3."""
        for text in ["password=Hunter2Hunter", 'password="two words here"', "pw_token: a@b!c#d$e%f^", r'{\"token\":\"abcdefghij1234\"}',
                     "AWS_SECRET_ACCESS_KEY=" + "wJalrXUtnFEMI" + "/K7MDENGbPxRfiCY", "client_secret = 'abc def ghi jkl'"]:
            with self.subTest(text):
                self.assertIn("named_secret_value", kinds(text))

    def test_more_secret_shapes_are_caught(self):
        for want, text in [("github_token", "x " + "gh" + "p_" + "A" * 36), ("github_token", "github_" + "pat_" + "A" * 30),
                           ("slack_token", "xo" + "xb-" + "1" * 10 + "-abcdefghij"), ("provider_key", "sk_" + "live_" + "a" * 24),
                           ("basic_auth", "Authorization: Basic " + "dXNlcjpwYXNzd29yZA=="), ("private_key_block", "-----BEGIN PGP " + "PRIVATE KEY BLOCK-----")]:
            with self.subTest(want):
                self.assertIn(want, kinds(text))

    def test_a_long_path_and_a_file_url_are_exempt_and_a_look_alike_is_not(self):
        """C1 finding 8: the path exemption must be live code with a test that fails without it."""
        self.assertEqual(kinds("password=/run/secrets/a_long_secret_file_name"), [])
        self.assertEqual(kinds("token=file:/run/secrets/a_long_secret_file_name"), [])
        self.assertIn("named_secret_value", kinds("token=run/secrets/not_absolute_but_long_value"))

    def test_a_luhn_invalid_card_length_run_is_not_named_a_card(self):
        """C1 finding 8: forcing the Luhn check to True must break a test."""
        self.assertEqual(kinds("n 4111111111111112"), ["digit_run"])
        self.assertEqual(kinds("n 4111111111111111"), ["card_number"])


if __name__ == "__main__":
    unittest.main()
