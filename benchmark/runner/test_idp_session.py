"""IdpSessionProvider: session tokens for the benchmark runner from the synthetic IdP (REQ-SEC-001; EDG-01 slice 2). It calls the issuer
(`services/core/cmd/synthetic-idp`) because a token lives 15 minutes and a run lasts longer, so a directory of static files cannot serve it.
The stub issuer below is a shell script that records its arguments; tokens are SYNTHETIC strings. A token is a secret: it is never put in an
error."""
import os
import stat
import tempfile
import unittest
from pathlib import Path

from benchmark.runner.env import ISSUER_ROLES, EnvironmentUnavailable, IdpSessionProvider


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

    def provider(self, binary: Path, ttl_s: int = 600, refresh_margin_s: int = 60, timeout_s: float = 10.0) -> IdpSessionProvider:
        return IdpSessionProvider(binary, self.key, kid="idp-k1", audience="noema-core", tenant_id="tnt_a", ttl_s=ttl_s,
                                  refresh_margin_s=refresh_margin_s, timeout_s=timeout_s, now=self.clock)

    def count(self, binary: Path) -> int:
        return int(Path(str(binary) + ".count").read_text())

    def test_it_asks_the_issuer_for_the_principal_its_roles_the_tenant_and_a_ttl(self) -> None:
        binary = stub(self.dir, COUNTING)
        token = self.provider(binary, ttl_s=600).token_for("cust_1", ["customer"])
        self.assertEqual(token, "SYNTHETIC.token.1")
        args = Path(str(binary) + ".args").read_text()
        for part in ("issue", f"-key={self.key}", "-kid=idp-k1", "-aud=noema-core", "-tenant=tnt_a", "-principal=cust_1", "-role=customer", "-ttl=600s"):
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
        tokens = {p.token_for("cust_1", ["customer"]), p.token_for("cust_2", ["customer"]), p.token_for("cust_1", ["approver"])}
        self.assertEqual(len(tokens), 3)
        self.assertEqual(p.token_for("cust_1", ["customer", "customer"]), p.token_for("cust_1", ["customer"]))  # one role written twice is the same session

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


SESSION_GO = Path(__file__).resolve().parents[2] / "services" / "core" / "edge" / "session" / "session.go"


class IssuerRoles(unittest.TestCase):
    """Ruling A152 (noema#198, Codex P1): benchmark principals use the A1 roles only. The role list is the issuer's own, read from the session package the issuer and the verifier
    share (`services/core/edge/session/session.go`, `Role` constants, ruling A1 item 1): the runner refuses any other role before it calls the issuer, and every generated case obeys it."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self._tmp.name)
        self.key = self.dir / "private.pem"
        self.key.write_text("SYNTHETIC key file")

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def provider(self, binary: Path) -> IdpSessionProvider:
        return IdpSessionProvider(binary, self.key, kid="idp-k1", audience="noema-core", tenant_id="tnt_a")

    def issuer_roles_from_source(self) -> set[str]:
        import re
        return set(re.findall(r'^\s*Role\w+\s+Role\s*=\s*"([a-z_]+)"\s*$', SESSION_GO.read_text(encoding="utf-8"), re.MULTILINE))

    def test_the_runners_role_list_is_the_issuers_role_list(self) -> None:
        self.assertEqual({"customer", "ops", "approver", "audit"}, self.issuer_roles_from_source())
        self.assertEqual(self.issuer_roles_from_source(), set(ISSUER_ROLES))

    def test_a_role_the_issuer_does_not_accept_is_refused_before_the_issuer_runs(self) -> None:
        binary = stub(self.dir, COUNTING)
        for role in ("viewer", "ops_agent", "operator", "admin", "Customer", "customer ", "", "audit_", "approver2"):
            with self.assertRaises(EnvironmentUnavailable, msg=repr(role)) as caught:
                self.provider(binary).token_for("cust_1", [role])
            self.assertIn("role", str(caught.exception))
            self.assertNotIn("SYNTHETIC", str(caught.exception))
        self.assertFalse(Path(str(binary) + ".count").exists(), "the issuer must never have been called")

    def test_each_of_the_four_roles_is_accepted_alone(self) -> None:
        binary = stub(self.dir, COUNTING)
        for role in sorted(ISSUER_ROLES):
            self.assertTrue(self.provider(binary).token_for(f"usr_{role}", [role]).startswith("SYNTHETIC"), role)

    def test_exactly_one_role_is_required_as_the_issuer_requires(self) -> None:
        binary = stub(self.dir, COUNTING)
        for roles in ([], ["customer", "ops"], ["customer", "approver", "audit"]):
            with self.assertRaises(EnvironmentUnavailable, msg=str(roles)):
                self.provider(binary).token_for("cust_1", roles)
        self.assertFalse(Path(str(binary) + ".count").exists())
        self.assertTrue(self.provider(binary).token_for("cust_1", ["customer", "customer"]))  # one role written twice is one role

    def test_every_principal_of_the_generated_suite_has_exactly_one_issuer_role(self) -> None:
        from benchmark.generator.generate import generate
        suite = generate(20261020)
        bad = {cid: case["principal"]["roles"] for cid, case in suite.cases.items() if len(set(case["principal"]["roles"])) != 1 or not set(case["principal"]["roles"]) <= ISSUER_ROLES}
        self.assertEqual({}, bad)

    def test_every_role_the_pack_allows_is_an_issuer_role(self) -> None:
        import json
        pack = json.loads((Path(__file__).resolve().parents[2] / "packs" / "fintech_v1.json").read_text(encoding="utf-8"))
        found: set[str] = set()

        def walk(node: object) -> None:
            if isinstance(node, dict):
                for key, value in node.items():
                    if key in ("roles_allowed", "roles") and isinstance(value, list):
                        found.update(str(v) for v in value)
                    walk(value)
            elif isinstance(node, list):
                for item in node:
                    walk(item)
        walk(pack)
        self.assertTrue(found, "the pack names roles")
        self.assertLessEqual(found, set(ISSUER_ROLES), sorted(found - set(ISSUER_ROLES)))
