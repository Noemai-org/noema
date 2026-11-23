"""The database image's downloads (deploy/spike/pgsodium/Dockerfile; ruling A530): the libsodium download retries a transient failure, and the SHA-256 check stays (a retry never relaxes a pin).
Reads the Dockerfile; builds nothing."""
import re
import unittest
from pathlib import Path

DOCKERFILE = Path(__file__).resolve().parents[2] / "deploy" / "spike" / "pgsodium" / "Dockerfile"


def libsodium_step() -> str:
    text = DOCKERFILE.read_text(encoding="utf-8").replace("\\\n", " ")
    step = re.search(r"(?m)^RUN curl [^\n]*libsodium\.tar\.gz[^\n]*$", text)
    assert step, "the libsodium download step was not found"
    return step.group(0)


class LibsodiumDownload(unittest.TestCase):
    def test_the_download_retries_transient_failures(self) -> None:
        step = libsodium_step()
        self.assertRegex(step, r"--retry [1-9]\d*\b")
        self.assertIn("--retry-all-errors", step)
        self.assertRegex(step, r"--retry-delay [1-9]\d*\b")

    def test_the_download_still_fails_on_http_errors_and_is_checked_against_the_pin(self) -> None:
        step = libsodium_step()
        self.assertRegex(step, r"curl -f")  # -f: an HTTP error is a failure, never a saved error page
        self.assertIn('sha256sum -c -', step)
        self.assertIn('"${LIBSODIUM_SHA256}  libsodium.tar.gz"', step)
        self.assertLess(step.index("sha256sum -c -"), step.index("tar -xzf"), "the archive is unpacked before it is checked")

    def test_the_sha256_pin_is_still_a_64_hex_build_argument(self) -> None:
        self.assertRegex(DOCKERFILE.read_text(encoding="utf-8"), r"(?m)^ARG LIBSODIUM_SHA256=[0-9a-f]{64}$")


if __name__ == "__main__":
    unittest.main()
