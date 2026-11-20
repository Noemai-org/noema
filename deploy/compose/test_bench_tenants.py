"""One tenant list for the bench stack (Codex P1 on noema#790; A356).

`make bench-up` runs secrets_local.sh BEFORE Compose starts. secrets_local.sh makes one emitter-login secret per tenant of NOEMA_TENANTS, and postgres's init.sh refuses to start when a tenant it is
given has none. In the bench overlay postgres and trust-dsn-init take NOEMA_TENANTS, falling back to the stand-in tenant (BENCH_TENANT, default ten_alpha). If the secret step does not see the same
fallback, a fresh checkout with NOEMA_TENANTS unset gets a tenant in postgres and no secret for it, and the stack does not come up. These tests read the real Makefile and the real overlay."""
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
MAKEFILE = (ROOT / "Makefile").read_text(encoding="utf-8")
OVERLAY = (ROOT / "deploy" / "compose" / "bench-stack.yml").read_text(encoding="utf-8")


def recipe(name: str) -> str:
    m = re.search(r"(?ms)^" + re.escape(name) + r":.*?(?=^\S)", MAKEFILE)
    assert m, name
    return m.group(0)


class BenchTenants(unittest.TestCase):
    def test_there_is_one_effective_tenant_list_and_it_falls_back_to_the_bench_tenant(self) -> None:
        self.assertRegex(MAKEFILE, r"(?m)^BENCH_TENANTS = \$\(or \$\(NOEMA_TENANTS\),\$\(BENCH_TENANT\)\)$")

    def test_the_secret_step_of_bench_up_sees_the_same_list_postgres_gets(self) -> None:
        up = recipe("bench-up")
        self.assertRegex(up, r'NOEMA_TENANTS="\$\(BENCH_TENANTS\)" deploy/compose/secrets_local\.sh')
        self.assertNotRegex(up, r"(?m)^\tdeploy/compose/secrets_local\.sh")  # no call without the list

    def test_compose_gets_the_same_list(self) -> None:
        env = re.search(r"(?m)^BENCH_ENV = (.*)$", MAKEFILE)
        assert env
        self.assertIn('NOEMA_TENANTS="$(BENCH_TENANTS)"', env.group(1))

    def test_postgres_and_the_dsn_step_take_the_same_expression_in_the_overlay(self) -> None:
        expr = '"${NOEMA_TENANTS:-${STANDIN_TENANT_ID:-ten_alpha}}"'
        self.assertEqual(OVERLAY.count("NOEMA_TENANTS: " + expr), 1)
        self.assertEqual(OVERLAY.count("TRUST_DSN_TENANTS: " + expr), 1)


if __name__ == "__main__":
    unittest.main()
