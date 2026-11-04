"""IdpSessionProvider: session tokens for the benchmark runner from the synthetic IdP (REQ-SEC-001; EDG-01 slice 2). It calls the issuer
(`services/core/cmd/synthetic-idp`) because a token lives 15 minutes and a run lasts longer, so a directory of static files cannot serve it.
The stub issuer below is a shell script that records its arguments; tokens are SYNTHETIC strings. A token is a secret: it is never put in an
error."""
import os
import stat
import tempfile
import unittest
from pathlib import Path

from benchmark.runner.env import EnvironmentUnavailable, IdpSessionProvider


def stub(directory: Path, body: str) -> Path:
    path = directory / "synthetic-idp"
    path.write_text("#!/bin/sh\n" + body)
    path.chmod(path.stat().st_mode | stat.S_IXUSR)
    return path


COUNTING = 'n=$(cat "$0.count" 2>/dev/null || echo 0); n=$((n+1)); echo $n > "$0.count"; printf "%s\\n" "$*" > "$0.args"; echo "SYNTHETIC.token.$n"\n'


class Clock:
    def __init__(self) -> None:
        self.t = 1000.0

    def __call__(self) -> float:
        return self.t


class IdpSession(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self._tmp.name)
        self.key = self.dir / "private.pem"
        self.key.write_text("SYNTHETIC key file")
        self.clock = Clock()

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def provider(self, binary: Path, **kw: object) -> IdpSessionProvider:
        return IdpSessionProvider(binary, self.key, kid="idp-k1", audience="noema-core", tenant_id="tnt_a", now=self.clock, **kw)  # type: ignore[arg-type]

    def count(self, binary: Path) -> int:
        return int(Path(str(binary) + ".count").read_text())

    def test_it_asks_the_issuer_for_the_principal_its_roles_the_tenant_and_a_ttl(self) -> None:
        binary = stub(self.dir, COUNTING)
        token = self.provider(binary, ttl_s=600).token_for("cust_1", ["customer", "approver"])
        self.assertEqual(token, "SYNTHETIC.token.1")
        args = Path(str(binary) + ".args").read_text()
        for part in ("issue", f"-key={self.key}", "-kid=idp-k1", "-aud=noema-core", "-tenant=tnt_a", "-principal=cust_1", "-role=customer", "-role=approver", "-ttl=600s"):
            self.assertIn(part, args.split(), part)

    def test_a_token_is_reused_until_shortly_before_it_expires_then_replaced(self) -> None:
        binary = stub(self.dir, COUNTING)
        p = self.provider(binary, ttl_s=600, refresh_margin_s=60)
        first = p.token_for("cust_1", ["customer"])
        self.clock.t += 539  # 61 s left
        self.assertEqual(p.token_for("cust_1", ["customer"]), first)
        self.assertEqual(self.count(binary), 1)
        self.clock.t += 1  # 60 s left: inside the margin
        self.assertNotEqual(p.token_for("cust_1", ["customer"]), first)
        self.assertEqual(self.count(binary), 2)

    def test_a_token_is_not_shared_between_principals_or_role_sets(self) -> None:
        binary = stub(self.dir, COUNTING)
        p = self.provider(binary)
        tokens = {p.token_for("cust_1", ["customer"]), p.token_for("cust_2", ["customer"]), p.token_for("cust_1", ["customer", "approver"])}
        self.assertEqual(len(tokens), 3)
        self.assertEqual(p.token_for("cust_1", ["approver", "customer"]), p.token_for("cust_1", ["customer", "approver"]))  # role order does not matter

    def test_an_issuer_that_fails_is_a_named_gap_and_never_leaks_what_it_printed(self) -> None:
        binary = stub(self.dir, 'echo "SECRET-LOOKING-OUTPUT" ; echo "SECRET-ERR" >&2; exit 1\n')
        with self.assertRaises(EnvironmentUnavailable) as caught:
            self.provider(binary).token_for("cust_1", ["customer"])
        self.assertEqual(caught.exception.component, "session provider")
        self.assertNotIn("SECRET", str(caught.exception))
        empty = stub(self.dir, "exit 0\n")
        with self.assertRaises(EnvironmentUnavailable):
            self.provider(empty).token_for("cust_1", ["customer"])

    def test_a_hung_issuer_times_out(self) -> None:
        binary = stub(self.dir, "sleep 5\n")
        with self.assertRaises(EnvironmentUnavailable) as caught:
            self.provider(binary, timeout_s=0.3).token_for("cust_1", ["customer"])
        self.assertIn("timed out", str(caught.exception))

    def test_a_missing_binary_or_key_is_reported_before_any_call(self) -> None:
        self.assertIsNotNone(self.provider(self.dir / "nope").unavailable_reason)
        binary = stub(self.dir, COUNTING)
        self.assertIsNone(self.provider(binary).unavailable_reason)
        self.key.unlink()
        self.assertIn("key", self.provider(binary).unavailable_reason or "")
        with self.assertRaises(EnvironmentUnavailable):
            self.provider(binary).token_for("cust_1", ["customer"])

    def test_a_principal_or_role_that_looks_like_a_flag_is_refused_not_passed_on(self) -> None:
        binary = stub(self.dir, COUNTING)
        p = self.provider(binary)
        for principal, roles in (("-key", ["customer"]), ("", ["customer"]), ("cust_1", []), ("cust_1", ["-ttl"]), ("cust 1", ["customer"])):
            with self.assertRaises(EnvironmentUnavailable, msg=(principal, roles)):
                p.token_for(principal, roles)
        self.assertFalse(Path(str(binary) + ".count").exists(), "the issuer must not run for a refused request")

    def test_it_does_not_pass_the_environment_on(self) -> None:
        binary = stub(self.dir, 'echo "SYNTHETIC.${NOEMA_PROBE:-clean}"\n')
        os.environ["NOEMA_PROBE"] = "leaked"
        try:
            self.assertEqual(self.provider(binary).token_for("cust_1", ["customer"]), "SYNTHETIC.clean")
        finally:
            del os.environ["NOEMA_PROBE"]


if __name__ == "__main__":
    unittest.main()
