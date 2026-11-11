"""Tests of deploy/relay-host/setup.sh --check (ENV-13; Codex review of noema#189).

The DuckDNS token is read from a file and put into a curl configuration line. The script is the secret boundary, so it must refuse a
token file that could change that line (a quote, a second line, a space, an ampersand) before anything is sent, and in --check too, and it
must never print the token. SYNTHETIC (AGENT-25): the tokens below are made up.
"""
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "setup.sh"
BASH = os.environ.get("RELAY_TEST_BASH") or shutil.which("bash")
UUID = "0b9f2a6e-3c1d-4e7a-9a52-7d1e8c4b6f03"


class ImagePin(unittest.TestCase):
    def test_the_caddy_image_is_pinned_by_digest(self) -> None:  # PLT-02 pinning rule; a tag can move, a digest cannot (ENV-13, noema#189)
        line = next(l for l in SCRIPT.read_text().splitlines() if l.startswith("caddy_image="))
        self.assertRegex(line, r'^caddy_image="caddy:2\.8\.4@sha256:[0-9a-f]{64}"$')


class CaddyHardening(unittest.TestCase):
    """SR review of noema#189 (F2, F3, F4): the relay is public, so the generated Caddyfile and the container flags are part of the contract.
    The Caddyfile is printed by --check from the same function that writes it, so these tests read what the host would get."""

    def caddyfile(self) -> str:
        r = SetupCheck("run_check").run_check(b"a" * 20)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        start = r.stdout.index("--- Caddyfile ---\n") + len("--- Caddyfile ---\n")
        return r.stdout[start:r.stdout.index("--- end Caddyfile ---")]

    def test_the_admin_api_is_off(self) -> None:  # F2: the admin endpoint on the host network could rewrite the proxy
        text = self.caddyfile()
        self.assertTrue(text.lstrip().startswith("{\n\tadmin off\n}"), text)

    def test_only_the_webhook_and_health_paths_are_public(self) -> None:  # F3: /v1/deliveries and /v1/stats must not be on the internet
        text = self.caddyfile()
        self.assertEqual(text.count("reverse_proxy"), 2, text)
        self.assertRegex(text, r"handle /webhooks \{\n\s*reverse_proxy 127\.0\.0\.1:8090\n\s*\}")
        self.assertRegex(text, r"handle /healthz \{\n\s*reverse_proxy 127\.0\.0\.1:8090\n\s*\}")
        self.assertRegex(text, r"handle \{\n\s*respond 404\n\s*\}")
        self.assertNotIn("/v1/", text)

    def test_the_caddy_container_is_hardened(self) -> None:  # F4
        run = SCRIPT.read_text()
        run = run[run.index("docker run -d --name"):]
        run = run[:run.index(">/dev/null")]
        for flag in ("--cap-drop ALL", "--cap-add NET_BIND_SERVICE", "--security-opt no-new-privileges:true", "--read-only", "--tmpfs /tmp",
                     "noema-relay-caddy-config:/config", "noema-relay-caddy-data:/data"):
            self.assertIn(flag, run)


class SetupCheck(unittest.TestCase):
    def setUp(self) -> None:
        # A test that cannot run is a failure, never a silent skip (set RELAY_TEST_BASH to a bash on a machine without one on PATH).
        self.assertIsNotNone(BASH, "bash is required to test setup.sh")

    def run_check(self, token_bytes: bytes | None, **env: str) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as tmp:
            token_file = Path(tmp) / "duckdns.token"
            if token_bytes is not None:
                token_file.write_bytes(token_bytes)
            e = {"PATH": os.environ["PATH"], "HOME": tmp, "RELAY_HOSTNAME": "relay-example.duckdns.org", "DUCKDNS_TOKEN_FILE": str(token_file)}
            e.update(env)
            return subprocess.run([BASH, str(SCRIPT), "--check"], env=e, capture_output=True, text=True, timeout=30, check=False)

    def refused(self, token_bytes: bytes) -> None:
        r = self.run_check(token_bytes)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("DUCKDNS_TOKEN_FILE", r.stderr)
        for fragment in (token_bytes.decode("latin-1").strip().split("\n")[0][:12],):
            if len(fragment) >= 8:
                self.assertNotIn(fragment, r.stdout + r.stderr, "the token must never be printed")

    def test_a_plain_token_passes_and_is_never_printed(self) -> None:
        for raw in (UUID.encode(), (UUID + "\n").encode(), (UUID + "\r\n").encode()):
            r = self.run_check(raw)
            self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
            self.assertIn("--check, nothing changed", r.stdout)
            self.assertNotIn(UUID, r.stdout + r.stderr)

    def test_a_double_quote_in_the_token_is_refused(self) -> None:
        self.refused(b'0b9f2a6e-3c1d-4e7a"-9a52-7d1e8c4b6f03\n')

    def test_a_second_line_that_would_add_a_curl_option_is_refused(self) -> None:
        self.refused(b'0b9f2a6e-3c1d-4e7a-9a52-7d1e8c4b6f03\nurl = "http://attacker.invalid/"\n')

    def test_an_ampersand_a_space_or_a_backslash_is_refused(self) -> None:
        for bad in (b"0b9f2a6e3c1d4e7a&ip=198.51.100.7", b"0b9f2a6e3c1d4e7a 9a527d1e8c4b6f03", b"0b9f2a6e3c1d4e7a\\9a527d1e8c4b6f03"):
            with self.subTest(bad=bad):
                self.refused(bad)

    def test_a_token_that_is_too_short_or_too_long_is_refused(self) -> None:
        self.refused(b"abc123")
        self.refused(b"a" * 65)

    def test_an_empty_or_missing_token_file_is_refused(self) -> None:
        self.assertEqual(self.run_check(b"").returncode, 1)
        self.assertEqual(self.run_check(None).returncode, 1)

    def test_the_other_refusals_still_hold(self) -> None:
        self.assertEqual(self.run_check(UUID.encode(), RELAY_HOSTNAME="example.com").returncode, 1)
        self.assertEqual(self.run_check(UUID.encode(), RELAY_HOSTNAME="").returncode, 1)
        self.assertEqual(self.run_check(UUID.encode(), WEBHOOK_RELAY_PUBLISH="0.0.0.0:8090").returncode, 1)
        self.assertEqual(self.run_check(UUID.encode(), WEBHOOK_RELAY_PUBLISH="127.0.0.1:8090").returncode, 0)


if __name__ == "__main__":
    unittest.main()